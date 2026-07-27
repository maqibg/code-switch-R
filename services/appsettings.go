package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	appSettingsDir      = ".code-switch-R"
	appSettingsFile     = "app.json"
	oldSettingsDir      = ".codex-swtich"               // 旧的错误拼写
	migrationMarkerFile = ".migrated-from-codex-swtich" // 迁移标记文件
)

type AppSettings struct {
	ShowHeatmap               bool    `json:"show_heatmap"`
	ShowHomeTitle             bool    `json:"show_home_title"`
	BudgetTotal               float64 `json:"budget_total"`
	BudgetUsedAdjustment      float64 `json:"budget_used_adjustment"`
	BudgetCycleEnabled        bool    `json:"budget_cycle_enabled"`
	BudgetCycleMode           string  `json:"budget_cycle_mode"`
	BudgetRefreshTime         string  `json:"budget_refresh_time"`
	BudgetRefreshDay          int     `json:"budget_refresh_day"`
	BudgetShowCountdown       bool    `json:"budget_show_countdown"`
	BudgetShowForecast        bool    `json:"budget_show_forecast"`
	BudgetForecastMethod      string  `json:"budget_forecast_method"`
	BudgetTotalCodex          float64 `json:"budget_total_codex"`
	BudgetUsedAdjustmentCodex float64 `json:"budget_used_adjustment_codex"`
	BudgetCycleEnabledCodex   bool    `json:"budget_cycle_enabled_codex"`
	BudgetCycleModeCodex      string  `json:"budget_cycle_mode_codex"`
	BudgetRefreshTimeCodex    string  `json:"budget_refresh_time_codex"`
	BudgetRefreshDayCodex     int     `json:"budget_refresh_day_codex"`
	BudgetShowCountdownCodex  bool    `json:"budget_show_countdown_codex"`
	BudgetShowForecastCodex   bool    `json:"budget_show_forecast_codex"`
	BudgetForecastMethodCodex string  `json:"budget_forecast_method_codex"`
	AutoStart                 bool    `json:"auto_start"`
	AutoUpdate                bool    `json:"auto_update"`
	EnableSwitchNotify        bool    `json:"enable_switch_notify"` // 供应商切换通知开关
	EnableRoundRobin          bool    `json:"enable_round_robin"`   // 同 Level 轮询负载均衡开关（默认关闭）
	GlobalProxyEnabled        bool    `json:"global_proxy_enabled"`
	GlobalProxyProtocol       string  `json:"global_proxy_protocol"`
	GlobalProxyHost           string  `json:"global_proxy_host"`
	GlobalProxyPort           int     `json:"global_proxy_port"`
	LogRetentionDays          int     `json:"log_retention_days"`
	// LogRetentionInitialized 标记保留策略是否已经对用户明确过。
	//
	// LogRetentionDays 的 0 表示"永不清理"，但旧版本 app.json 里根本没有这个字段，
	// 反序列化后同样是 0——两者无法区分。直接把默认值改成 90 天会让老用户在
	// 升级后某天突然丢失历史数据。因此用这个标记区分：
	//   false = 从未明确过（老配置或全新安装），需要走一次默认值决策并提示用户
	//   true  = 用户或迁移逻辑已确认过，此后 0 就是字面意义上的"永不清理"
	LogRetentionInitialized bool `json:"log_retention_initialized"`
	// LogRetentionNotice 一次性提示文案，非空表示需要向用户说明保留策略的变化。
	// 前端读取并展示后调用 AcknowledgeLogRetentionNotice 清除。
	LogRetentionNotice string `json:"log_retention_notice,omitempty"`
}

type AppSettingsService struct {
	path             string
	mu               sync.Mutex
	autoStartService *AutoStartService
	cache            AppSettings
	cacheModTime     time.Time
	cacheSize        int64
	cacheExists      bool
	cacheValid       bool
}

func NewAppSettingsService(autoStartService *AutoStartService) *AppSettingsService {
	newDir, err := getAppConfigDir()
	if err != nil {
		newDir = mustGetAppConfigDir()
	}

	return &AppSettingsService{
		// app.json 统一存放在程序目录下的 .code-switch-R。
		path:             filepath.Join(newDir, appSettingsFile),
		autoStartService: autoStartService,
	}
}

// migrateSettings 完整的配置迁移
// 迁移顺序：写新文件 → 校验 → 标记 → 删旧
func migrateSettings(oldPath, newPath, oldDir, markerPath string) error {
	// 1. 确保新目录存在
	if err := os.MkdirAll(filepath.Dir(newPath), 0755); err != nil {
		return fmt.Errorf("创建新目录失败: %w", err)
	}

	// 2. 检查新文件是否已存在
	if _, err := os.Stat(newPath); err == nil {
		// 新文件已存在，不覆盖，但仍创建迁移标记
		fmt.Printf("[AppSettings] 新配置文件已存在，跳过迁移\n")
	} else {
		// 3. 读取旧配置
		data, err := os.ReadFile(oldPath)
		if err != nil {
			return fmt.Errorf("读取旧配置失败: %w", err)
		}

		// 4. 写入新配置
		if err := os.WriteFile(newPath, data, 0644); err != nil {
			return fmt.Errorf("写入新配置失败: %w", err)
		}

		// 5. 校验新文件
		verifyData, err := os.ReadFile(newPath)
		if err != nil {
			// 写入成功但读取失败，回滚
			os.Remove(newPath)
			return fmt.Errorf("校验新配置失败（已回滚）: %w", err)
		}

		// 校验内容一致性
		if !bytes.Equal(data, verifyData) {
			os.Remove(newPath)
			return fmt.Errorf("配置内容校验失败（已回滚）: 写入内容与读取内容不一致")
		}

		// 如果是 JSON 文件，额外校验 JSON 格式有效性
		var jsonTest interface{}
		if err := json.Unmarshal(verifyData, &jsonTest); err != nil {
			os.Remove(newPath)
			return fmt.Errorf("JSON 格式校验失败（已回滚）: %w", err)
		}

		fmt.Printf("[AppSettings] ✅ 已迁移并校验配置: %s → %s\n", oldPath, newPath)
	}

	// 6. 创建迁移标记文件
	markerContent := fmt.Sprintf("迁移时间: %s\n旧路径: %s\n", time.Now().Format(time.RFC3339), oldDir)
	if err := os.WriteFile(markerPath, []byte(markerContent), 0644); err != nil {
		return fmt.Errorf("创建迁移标记失败: %w", err)
	}

	// 7. 只有在新文件校验通过后才删除旧目录
	if err := os.RemoveAll(oldDir); err != nil {
		// 删除失败不是致命错误，只记录警告
		fmt.Printf("[AppSettings] ⚠️  删除旧目录失败: %v（可手动删除 %s）\n", err, oldDir)
	} else {
		fmt.Printf("[AppSettings] ✅ 已删除旧目录: %s\n", oldDir)
	}

	return nil
}

func (as *AppSettingsService) defaultSettings() AppSettings {
	return AppSettings{
		ShowHeatmap:               true,
		ShowHomeTitle:             true,
		BudgetTotal:               0,
		BudgetUsedAdjustment:      0,
		BudgetCycleEnabled:        false,
		BudgetCycleMode:           "daily",
		BudgetRefreshTime:         "00:00",
		BudgetRefreshDay:          1,
		BudgetShowCountdown:       false,
		BudgetShowForecast:        false,
		BudgetForecastMethod:      "cycle",
		BudgetTotalCodex:          0,
		BudgetUsedAdjustmentCodex: 0,
		BudgetCycleEnabledCodex:   false,
		BudgetCycleModeCodex:      "daily",
		BudgetRefreshTimeCodex:    "00:00",
		BudgetRefreshDayCodex:     1,
		BudgetShowCountdownCodex:  false,
		BudgetShowForecastCodex:   false,
		BudgetForecastMethodCodex: "cycle",
		AutoStart:                 false,
		AutoUpdate:                true,  // 默认开启自动更新
		EnableSwitchNotify:        true,  // 默认开启切换通知
		EnableRoundRobin:          false, // 默认关闭轮询（使用顺序降级）
		GlobalProxyEnabled:        false,
		GlobalProxyProtocol:       defaultGlobalProxyProtocol,
		GlobalProxyHost:           defaultGlobalProxyHost,
		GlobalProxyPort:           defaultGlobalProxyPort,
		LogRetentionDays:          defaultLogRetentionDays,
		LogRetentionInitialized:   true, // 全新安装：默认值即为已确认，无需提示
	}
}

// GetAppSettings returns the persisted app settings or defaults if the file does not exist.
func (as *AppSettingsService) GetAppSettings() (AppSettings, error) {
	as.mu.Lock()
	defer as.mu.Unlock()
	return as.loadLocked()
}

// SaveAppSettings persists the provided settings to disk.
func (as *AppSettingsService) SaveAppSettings(settings AppSettings) (AppSettings, error) {
	as.mu.Lock()
	defer as.mu.Unlock()
	settings = normalizeAppSettings(settings)

	// 同步开机自启动状态
	if as.autoStartService != nil {
		if settings.AutoStart {
			if err := as.autoStartService.Enable(); err != nil {
				return settings, err
			}
		} else {
			if err := as.autoStartService.Disable(); err != nil {
				return settings, err
			}
		}
	}

	if err := as.saveLocked(settings); err != nil {
		return settings, err
	}
	return settings, nil
}

func (as *AppSettingsService) loadLocked() (AppSettings, error) {
	settings := as.defaultSettings()
	info, statErr := os.Stat(as.path)
	if statErr != nil && !os.IsNotExist(statErr) {
		return settings, statErr
	}
	exists := statErr == nil
	if as.cacheValid && as.cacheExists == exists {
		if !exists || (info.Size() == as.cacheSize && info.ModTime().Equal(as.cacheModTime)) {
			return as.cache, nil
		}
	}
	if !exists {
		if as.autoStartService != nil {
			if enabled, err := as.autoStartService.IsEnabled(); err == nil {
				settings.AutoStart = enabled
			}
		}
		as.storeCacheLocked(settings, nil)
		return settings, nil
	}

	data, err := os.ReadFile(as.path)
	if err != nil {
		return settings, err
	}
	if len(data) == 0 {
		as.storeCacheLocked(settings, info)
		return settings, nil
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		return settings, err
	}
	settings = normalizeAppSettings(settings)

	// 老配置迁移：文件里没有 log_retention_initialized 说明是升级上来的用户。
	// 此时保留他们当前的行为（LogRetentionDays 原样，通常是 0 = 永不清理），
	// 只写入一条提示，让用户自己决定是否开启保留策略——不能替他们删历史数据。
	if migrated, changed := migrateLogRetentionSettings(settings, data); changed {
		settings = migrated
		if err := as.saveLocked(settings); err != nil {
			// 迁移落盘失败不阻塞读取，下次启动会再试一次
			logWarn(fmt.Sprintf("写入日志保留策略迁移标记失败: %v", err))
			as.storeCacheLocked(settings, info)
			return settings, nil
		}
		// saveLocked 内部已更新缓存
		return settings, nil
	}

	as.storeCacheLocked(settings, info)
	return settings, nil
}

func (as *AppSettingsService) saveLocked(settings AppSettings) error {
	dir := filepath.Dir(as.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	// app.json 是应用主设置文件，必须原子写入：
	// 裸 os.WriteFile 在写入途中崩溃或断电会留下截断的 JSON，下次启动直接读不出设置。
	if err := atomicWriteFile(as.path, data, 0o644); err != nil {
		return err
	}
	info, err := os.Stat(as.path)
	if err != nil {
		as.cacheValid = false
		return nil
	}
	as.storeCacheLocked(settings, info)
	return nil
}

func (as *AppSettingsService) storeCacheLocked(settings AppSettings, info os.FileInfo) {
	as.cache = settings
	as.cacheExists = info != nil
	as.cacheValid = true
	if info == nil {
		as.cacheModTime = time.Time{}
		as.cacheSize = 0
		return
	}
	as.cacheModTime = info.ModTime()
	as.cacheSize = info.Size()
}

func (as *AppSettingsService) GetGlobalProxyConfig() (ProxyConfig, error) {
	settings, err := as.GetAppSettings()
	if err != nil {
		return ProxyConfig{}, err
	}
	return settings.GlobalProxyConfig(), nil
}

func (as *AppSettingsService) GetProviderProxyConfig(enabled bool) (ProxyConfig, error) {
	config, err := as.GetGlobalProxyConfig()
	if err != nil {
		return ProxyConfig{}, err
	}
	config.Enabled = enabled
	return config, nil
}

func (as *AppSettingsService) TestGlobalProxy(protocol, host string, port int) ProxyTestResult {
	return TestProxyConfig(ProxyConfig{
		Enabled:  true,
		Protocol: protocol,
		Host:     host,
		Port:     port,
	})
}
