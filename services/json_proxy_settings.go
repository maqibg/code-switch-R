package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// 本文件抽出 JSON 型 CLI 配置的代理启停通用实现。
//
// 背景：claudesettings.go 与 reasonixsettings.go 曾是逐行同构的手工拷贝，
// 连错误消息都逐字相同。两者只有五处真实差异（配置结构嵌套与否、字段键名、
// baseURL 是否带路径后缀、状态文件的 EnvExisted 语义、写后是否清理空对象），
// 其余全部一致。拷贝导致同一个 bug 要修多份——B5 那个"解析失败用空配置
// 覆盖用户文件"的问题就在 5 个地方各存在一份。
//
// 各平台服务的外壳与方法签名保持不变：它们注册进 Wails 绑定供前端调用，
// 改动方法集会连带改前端。这里只承接内部实现。

// jsonProxyFieldAccess 描述"在配置对象里读写代理字段"的方式。
// 用于抹平 Claude 的 env 嵌套结构与 Reasonix 的顶层扁平结构。
type jsonProxyFieldAccess struct {
	// baseURLKey / authTokenKey 代理地址与凭据的字段名
	baseURLKey   string
	authTokenKey string
	// container 从配置根对象取出承载这两个字段的容器。
	// create 为 true 时容器不存在则新建；为 false 时不存在返回 nil。
	container func(payload map[string]any, create bool) map[string]any
	// afterWrite 写回前的收尾处理（Claude 用它在 env 变空且原本不存在时删掉 env）
	afterWrite func(payload map[string]any, container map[string]any, state *ProxyState)
	// containerExisted 计算写入状态文件的 EnvExisted 值
	containerExisted func(payload map[string]any) bool
}

// jsonProxyPlatform 一个 JSON 型平台的完整描述
type jsonProxyPlatform struct {
	// platform 代理状态文件使用的平台标识
	platform string
	// configDir 相对用户 home 的配置目录名，如 ".claude"
	configDir string
	// configFile 配置文件名，如 "settings.json"
	configFile string
	// backupFile 基线备份文件名
	backupFile string
	// authToken 启用代理时写入当前随机 Relay Token。
	authToken func() string
	// urlSuffix 代理地址的路径后缀，如 "/reasonix"；Claude 为空
	urlSuffix string
	// logPrefix 日志前缀，如 "[ClaudeSettingsService]"
	logPrefix string
	access    jsonProxyFieldAccess
}

// paths 返回配置文件与基线备份路径
func (p jsonProxyPlatform) paths() (configPath string, backupPath string, err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	dir := filepath.Join(home, p.configDir)
	return filepath.Join(dir, p.configFile), filepath.Join(dir, p.backupFile), nil
}

// baseURL 由 relay 监听地址推导代理地址
func (p jsonProxyPlatform) baseURL(relayAddr string) string {
	addr := strings.TrimSpace(relayAddr)
	if addr == "" {
		addr = ":18100"
	}
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		return strings.TrimSuffix(addr, "/") + p.urlSuffix
	}
	host := addr
	if strings.HasPrefix(host, ":") {
		host = "127.0.0.1" + host
	}
	if !strings.Contains(host, "://") {
		host = "http://" + host
	}
	return strings.TrimSuffix(host, "/") + p.urlSuffix
}

func (p jsonProxyPlatform) relayAuthToken() string {
	if p.authToken == nil {
		return RelayToken()
	}
	return p.authToken()
}

// urlMatchesProxy 比较配置里的地址是否就是本机代理地址（忽略尾部斜杠与大小写）
func urlMatchesProxy(configured, proxyURL string) bool {
	return strings.EqualFold(
		strings.TrimSuffix(strings.TrimSpace(configured), "/"),
		strings.TrimSuffix(strings.TrimSpace(proxyURL), "/"),
	)
}

// readJSONConfig 读取配置文件。
// 文件不存在返回 (nil, false, nil)；解析失败返回错误——绝不能"用空配置继续"，
// 那会把用户其余配置整体覆盖掉，属于不可恢复的数据丢失。
func (p jsonProxyPlatform) readJSONConfig(configPath string) (payload map[string]any, exists bool, err error) {
	content, readErr := os.ReadFile(configPath)
	if readErr != nil {
		if errors.Is(readErr, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, readErr
	}
	payload = make(map[string]any)
	if len(content) > 0 {
		if unmarshalErr := json.Unmarshal(content, &payload); unmarshalErr != nil {
			return nil, true, fmt.Errorf("%s 解析失败，请检查文件格式: %w", p.configFile, unmarshalErr)
		}
	}
	return payload, true, nil
}

// proxyStatus 判断代理是否已启用：配置里的地址等于本机代理地址即为启用
func (p jsonProxyPlatform) proxyStatus(relayAddr string) (enabled bool, baseURL string, err error) {
	baseURL = p.baseURL(relayAddr)
	configPath, _, err := p.paths()
	if err != nil {
		return false, baseURL, err
	}
	payload, exists, err := p.readJSONConfig(configPath)
	if err != nil {
		// 状态查询不应因文件损坏而失败，返回"未启用"即可
		return false, baseURL, nil
	}
	if !exists {
		return false, baseURL, nil
	}
	container := p.access.container(payload, false)
	if container == nil {
		return false, baseURL, nil
	}
	return urlMatchesProxy(anyToString(container[p.access.baseURLKey]), baseURL), baseURL, nil
}

// enableProxy 启用代理：只改代理相关字段，保留用户其余配置
func (p jsonProxyPlatform) enableProxy(relayAddr string) error {
	configPath, backupPath, err := p.paths()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return err
	}

	// 幂等化：状态文件存在说明已启用过，不能覆盖基线
	stateExists, err := ProxyStateExists(p.platform)
	if err != nil {
		return err
	}

	content, readErr := os.ReadFile(configPath)
	fileExisted := readErr == nil
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return fmt.Errorf("无法读取 %s: %w", p.configFile, readErr)
	}

	if fileExisted {
		// 基线备份只在首次启用时写
		if !stateExists {
			if err := os.WriteFile(backupPath, content, 0o600); err != nil {
				return err
			}
		}
		// 每次启用额外留一份时间戳快照：基线不含用户在代理期间的编辑
		if err := writeTimestampedBackup(configPath, content); err != nil {
			logWarn(fmt.Sprintf("%s 快照备份失败: %v", p.configFile, err))
		}
	}

	payload := make(map[string]any)
	if fileExisted && len(content) > 0 {
		if err := json.Unmarshal(content, &payload); err != nil {
			return fmt.Errorf("%s 解析失败，已中止启用以避免覆盖现有配置，请修正文件格式后重试: %w", p.configFile, err)
		}
	}

	proxyURL := p.baseURL(relayAddr)

	// 首次启用：记录启用前的基线，供停用时手术式恢复
	if !stateExists {
		state := &ProxyState{
			TargetPath:        configPath,
			FileExisted:       fileExisted,
			EnvExisted:        p.access.containerExisted(payload),
			InjectedBaseURL:   proxyURL,
			InjectedAuthToken: p.relayAuthToken(),
		}
		if container := p.access.container(payload, false); container != nil {
			if v, ok := container[p.access.baseURLKey]; ok {
				s := anyToString(v)
				state.OriginalBaseURL = &s
			}
			if v, ok := container[p.access.authTokenKey]; ok {
				s := anyToString(v)
				state.OriginalAuthToken = &s
			}
		}
		if err := SaveProxyState(p.platform, state); err != nil {
			return err
		}
	}

	container := p.access.container(payload, true)
	container[p.access.baseURLKey] = proxyURL
	container[p.access.authTokenKey] = p.relayAuthToken()
	if p.access.afterWrite != nil {
		p.access.afterWrite(payload, container, nil)
	}

	return AtomicWriteJSON(configPath, payload)
}

// disableProxy 停用代理：按基线手术式恢复，保留用户在代理期间的所有编辑
func (p jsonProxyPlatform) disableProxy(relayAddr string) error {
	configPath, _, err := p.paths()
	if err != nil {
		return err
	}

	payload, exists, err := p.readJSONConfig(configPath)
	if err != nil {
		return err
	}
	if !exists {
		return DeleteProxyState(p.platform)
	}

	state, stateErr := LoadProxyState(p.platform)
	if stateErr != nil {
		// 兜底：状态文件缺失或损坏时，只删仍等于代理值的字段，
		// 避免误删用户自己配置的直连地址
		container := p.access.container(payload, false)
		if container == nil {
			return DeleteProxyState(p.platform)
		}
		changed := false
		if urlMatchesProxy(anyToString(container[p.access.baseURLKey]), p.baseURL(relayAddr)) {
			delete(container, p.access.baseURLKey)
			changed = true
		}
		if relayManagedTokenMatches(anyToString(container[p.access.authTokenKey])) {
			delete(container, p.access.authTokenKey)
			changed = true
		}
		if changed {
			if p.access.afterWrite != nil {
				p.access.afterWrite(payload, container, nil)
			}
			if err := AtomicWriteJSON(configPath, payload); err != nil {
				return err
			}
		}
		return DeleteProxyState(p.platform)
	}

	container := p.access.container(payload, false)
	if container == nil || !urlMatchesProxy(anyToString(container[p.access.baseURLKey]), state.InjectedBaseURL) {
		return fmt.Errorf("%s 的代理地址已被外部修改，拒绝覆盖", p.configFile)
	}
	if anyToString(container[p.access.authTokenKey]) != state.InjectedAuthToken {
		return fmt.Errorf("%s 的代理凭据已被外部修改，拒绝覆盖", p.configFile)
	}
	if state.OriginalBaseURL != nil {
		container[p.access.baseURLKey] = *state.OriginalBaseURL
	} else {
		delete(container, p.access.baseURLKey)
	}
	if state.OriginalAuthToken != nil {
		container[p.access.authTokenKey] = *state.OriginalAuthToken
	} else {
		delete(container, p.access.authTokenKey)
	}
	if p.access.afterWrite != nil {
		p.access.afterWrite(payload, container, state)
	}

	if err := AtomicWriteJSON(configPath, payload); err != nil {
		return err
	}
	return DeleteProxyState(p.platform)
}
