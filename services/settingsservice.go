package services

import (
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"

	"github.com/daodao97/xgo/xdb"
)

const settingsHotPathCacheTTL = 2 * time.Second

// SettingsService 管理全局配置
type SettingsService struct {
	cacheMu                   sync.Mutex
	blacklistEnabledValue     bool
	blacklistEnabledUntil     time.Time
	blacklistLevelConfig      *BlacklistLevelConfig
	blacklistLevelConfigUntil time.Time
}

// BlacklistSettings 黑名单配置（基础配置，向后兼容）
type BlacklistSettings struct {
	FailureThreshold int `json:"failureThreshold"` // 失败次数阈值
	DurationMinutes  int `json:"durationMinutes"`  // 拉黑时长（分钟）
}

// BlacklistLevelConfig 等级拉黑配置（v0.4.0 新增）
type BlacklistLevelConfig struct {
	// 功能开关
	EnableLevelBlacklist bool `json:"enableLevelBlacklist"` // 是否启用等级拉黑

	// 基础配置
	FailureThreshold    int `json:"failureThreshold"`    // 失败阈值（连续失败次数）
	DedupeWindowSeconds int `json:"dedupeWindowSeconds"` // 去重窗口（秒）
	RetryWaitSeconds    int `json:"retryWaitSeconds"`    // 同 Provider 重试等待时间（秒），必须 > DedupeWindowSeconds

	// 降级配置
	NormalDegradeIntervalHours float64 `json:"normalDegradeIntervalHours"` // 正常降级间隔（小时）
	ForgivenessHours           float64 `json:"forgivenessHours"`           // 宽恕触发时间（小时）
	JumpPenaltyWindowHours     float64 `json:"jumpPenaltyWindowHours"`     // 跳级惩罚窗口（小时）

	// 等级时长配置（分钟）
	L1DurationMinutes int `json:"l1DurationMinutes"` // L1 拉黑时长
	L2DurationMinutes int `json:"l2DurationMinutes"` // L2 拉黑时长
	L3DurationMinutes int `json:"l3DurationMinutes"` // L3 拉黑时长
	L4DurationMinutes int `json:"l4DurationMinutes"` // L4 拉黑时长
	L5DurationMinutes int `json:"l5DurationMinutes"` // L5 拉黑时长

	// 开关关闭时的行为
	FallbackMode            string `json:"fallbackMode"`            // fixed=固定拉黑, none=不拉黑
	FallbackDurationMinutes int    `json:"fallbackDurationMinutes"` // 固定拉黑时长（分钟）
}

// DefaultBlacklistLevelConfig 返回默认的等级拉黑配置
func DefaultBlacklistLevelConfig() *BlacklistLevelConfig {
	return &BlacklistLevelConfig{
		EnableLevelBlacklist:       false, // 默认关闭，向后兼容
		FailureThreshold:           3,
		DedupeWindowSeconds:        2,
		RetryWaitSeconds:           3, // 必须 > DedupeWindowSeconds，否则重试不会计入失败次数
		NormalDegradeIntervalHours: 1.0,
		ForgivenessHours:           3.0,
		JumpPenaltyWindowHours:     2.5,
		L1DurationMinutes:          5,
		L2DurationMinutes:          15,
		L3DurationMinutes:          60,
		L4DurationMinutes:          360,  // 6小时
		L5DurationMinutes:          1440, // 24小时
		FallbackMode:               "fixed",
		FallbackDurationMinutes:    30,
	}
}

func NewSettingsService() *SettingsService {
	// 幂等兜底：正常启动已由 InitDatabase 跑过迁移，
	// 这里覆盖不经 InitDatabase 直接构造服务的场景（主要是测试）。
	if err := RunMigrations(); err != nil {
		// 记录错误但不阻止服务创建
		logError("SettingsService 应用数据库迁移失败", "error", err)
	}
	return &SettingsService{}
}

// GetBlacklistSettings 获取黑名单配置（阈值与固定拉黑时长）。
//
// 这两个值收敛到 blacklist_level_config 行（迁移 v7）。原先它们另有两个
// 独立键 blacklist_failure_threshold / blacklist_duration_minutes，
// 与配置里的 failureThreshold / fallbackDurationMinutes 是同一概念存两处，
// 调用方要"两处都读、不一致就打补丁"。
func (ss *SettingsService) GetBlacklistSettings() (threshold int, duration int, err error) {
	config, err := ss.GetBlacklistLevelConfig()
	if err != nil {
		return 0, 0, err
	}
	return config.FailureThreshold, config.FallbackDurationMinutes, nil
}

// IsBlacklistEnabled 检查拉黑功能是否启用
func (ss *SettingsService) IsBlacklistEnabled() bool {
	ss.cacheMu.Lock()
	defer ss.cacheMu.Unlock()
	if time.Now().Before(ss.blacklistEnabledUntil) {
		return ss.blacklistEnabledValue
	}
	db, err := xdb.DB("default")
	if err != nil {
		log.Printf("⚠️  获取数据库连接失败: %v，默认关闭拉黑", err)
		return false
	}

	var enabledStr string
	err = db.QueryRow(`
		SELECT value FROM app_settings WHERE key = 'enable_blacklist'
	`).Scan(&enabledStr)

	if err != nil {
		log.Printf("⚠️  获取拉黑开关失败: %v，默认关闭", err)
		return false
	}

	ss.blacklistEnabledValue = enabledStr == "true"
	ss.blacklistEnabledUntil = time.Now().Add(settingsHotPathCacheTTL)
	return ss.blacklistEnabledValue
}

// UpdateBlacklistEnabled 更新拉黑总开关。
//
// 这个开关只在 enable_blacklist 键上，不属于等级拉黑配置：
// 它决定是否记失败、是否拉黑；等级拉黑开关（EnableLevelBlacklist）
// 决定的是拉黑用等级模式还是固定模式。ShouldUseFixedMode 分别读两者。
func (ss *SettingsService) UpdateBlacklistEnabled(enabled bool) error {
	if err := dbExec(
		`INSERT INTO app_settings (key, value) VALUES ('enable_blacklist', ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		boolToSettingValue(enabled),
	); err != nil {
		return fmt.Errorf("更新拉黑开关失败: %w", err)
	}
	ss.cacheMu.Lock()
	ss.blacklistEnabledValue = enabled
	ss.blacklistEnabledUntil = time.Now().Add(settingsHotPathCacheTTL)
	ss.cacheMu.Unlock()

	logInfo("拉黑功能开关已更新", "enabled", enabled)
	return nil
}

// UpdateBlacklistSettings 同时更新失败阈值和固定拉黑时长。
//
// 两个值都写进 blacklist_level_config 行的 failureThreshold 与
// fallbackDurationMinutes（迁移 v7 收敛，原先另有两个独立键）。
//
// 这里的取值范围比 validateBlacklistLevelConfig 更窄，是有意保留的：
// 这是通用设置页那两个控件的约束（阈值 1-9、时长四选一），
// 而整份配置保存走的是等级拉黑那套更宽的范围。
//
// 早先这里是"Saga 模式 + 手工补偿回滚"——两条 UPDATE 分别提交，第二条失败时
// 再写一次把第一条改回去。那是因为所有写入都必须过队列，而队列没法开事务。
func (ss *SettingsService) UpdateBlacklistSettings(threshold int, duration int) error {
	// 验证参数
	if threshold < 1 || threshold > 9 {
		return fmt.Errorf("失败阈值必须在 1-9 之间")
	}

	if duration != 5 && duration != 15 && duration != 30 && duration != 60 {
		return fmt.Errorf("拉黑时长只支持 5/15/30/60 分钟")
	}

	if err := ss.updateBlacklistLevelConfigFields(func(config *BlacklistLevelConfig) {
		config.FailureThreshold = threshold
		config.FallbackDurationMinutes = duration
	}); err != nil {
		return fmt.Errorf("更新拉黑配置失败: %w", err)
	}
	return nil
}

// GetBlacklistSettingsStruct 获取黑名单配置（结构体形式，用于前端）
func (ss *SettingsService) GetBlacklistSettingsStruct() (*BlacklistSettings, error) {
	threshold, duration, err := ss.GetBlacklistSettings()
	if err != nil {
		return nil, err
	}

	return &BlacklistSettings{
		FailureThreshold: threshold,
		DurationMinutes:  duration,
	}, nil
}

// GetLevelBlacklistEnabled 获取等级拉黑开关状态。
//
// 前端通过这对方法读写开关（frontend/src/services/settings.ts）。
// 它原本用独立的 blacklist_level_enabled 键，与 JSON 配置里的
// enableLevelBlacklist 是同一概念存两处。现在统一读配置行，
// 方法签名不变，前端无需改动。
func (ss *SettingsService) GetLevelBlacklistEnabled() (bool, error) {
	config, err := ss.GetBlacklistLevelConfig()
	if err != nil {
		return false, err
	}
	return config.EnableLevelBlacklist, nil
}

// SetLevelBlacklistEnabled 设置等级拉黑开关状态。
//
// 写配置行里的 enableLevelBlacklist，与 GetLevelBlacklistEnabled 同源。
// 原先写独立键 blacklist_level_enabled，读取时再用它覆盖 JSON 文件里的
// 同名字段——反方向就丢：SaveBlacklistLevelConfig 只写 JSON 文件，
// 存进去的开关值会被下次读取时的旧独立键覆盖掉。
func (ss *SettingsService) SetLevelBlacklistEnabled(enabled bool) error {
	if err := ss.updateBlacklistLevelConfigFields(func(config *BlacklistLevelConfig) {
		config.EnableLevelBlacklist = enabled
	}); err != nil {
		return fmt.Errorf("设置等级拉黑开关失败: %w", err)
	}
	return nil
}

func (ss *SettingsService) invalidateBlacklistLevelConfigCache() {
	ss.cacheMu.Lock()
	ss.blacklistLevelConfig = nil
	ss.blacklistLevelConfigUntil = time.Time{}
	ss.cacheMu.Unlock()
}

// GetIntSetting 获取整数类型的配置值（通用方法）
// 如果找不到或解析失败，返回 0
func (ss *SettingsService) GetIntSetting(key string) int {
	db, err := xdb.DB("default")
	if err != nil {
		return 0
	}

	var valueStr string
	err = db.QueryRow(`SELECT value FROM app_settings WHERE key = ?`, key).Scan(&valueStr)
	if err != nil {
		return 0
	}

	value, err := strconv.Atoi(valueStr)
	if err != nil {
		return 0
	}

	return value
}

// SetIntSetting 设置整数类型的配置值（通用方法）
func (ss *SettingsService) SetIntSetting(key string, value int) error {
	err := dbExec(`
		INSERT INTO app_settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, key, strconv.Itoa(value))

	if err != nil {
		return fmt.Errorf("设置 %s 失败: %w", key, err)
	}

	return nil
}
