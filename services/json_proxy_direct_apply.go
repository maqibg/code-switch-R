package services

import (
	"fmt"
	"os"
)

// 直连应用（不经本地代理，把供应商地址直接写进 CLI 配置）的通用实现。
// 与 json_proxy_settings.go 同一背景：各平台原本各存一份逐行同构的拷贝。

// applySingleProvider 把指定供应商直接写入 CLI 配置。
// 代理启用时拒绝执行——两者会互相覆盖，同时生效没有意义。
func (p jsonProxyPlatform) applySingleProvider(relayAddr string, providerID int64) error {
	enabled, _, err := p.proxyStatus(relayAddr)
	if err != nil {
		return fmt.Errorf("检查代理状态失败: %w", err)
	}
	if enabled {
		return fmt.Errorf("本地代理已启用，请先关闭代理再进行直接应用")
	}

	providers, err := loadProviderSnapshot(p.platform)
	if err != nil {
		return fmt.Errorf("加载供应商配置失败: %w", err)
	}
	provider, found := findProviderByID(providers, providerID)
	if !found {
		return fmt.Errorf("未找到 ID 为 %d 的供应商", providerID)
	}
	if provider.APIURL == "" {
		return fmt.Errorf("供应商 '%s' 未配置 API 地址", provider.Name)
	}
	if provider.APIKey == "" {
		return fmt.Errorf("供应商 '%s' 未配置 API 密钥", provider.Name)
	}

	configPath, _, err := p.paths()
	if err != nil {
		return fmt.Errorf("获取配置路径失败: %w", err)
	}

	if _, err := CreateBackup(configPath); err != nil {
		fmt.Printf("%s 备份失败（非阻塞）: %v\n", p.logPrefix, err)
	}

	payload, _, err := p.readJSONConfig(configPath)
	if err != nil {
		return err
	}
	if payload == nil {
		payload = make(map[string]any)
	}

	container := p.access.container(payload, true)
	container[p.access.baseURLKey] = normalizeURLTrimSlash(provider.APIURL)
	container[p.access.authTokenKey] = provider.APIKey
	if p.access.afterWrite != nil {
		p.access.afterWrite(payload, container, nil)
	}

	return AtomicWriteJSON(configPath, payload)
}

// getDirectAppliedProviderID 反查当前配置对应哪个供应商。
// 代理启用时返回 nil（此时配置里是代理地址，不代表任何供应商）。
func (p jsonProxyPlatform) getDirectAppliedProviderID(relayAddr string) (*int64, error) {
	enabled, _, err := p.proxyStatus(relayAddr)
	if err != nil {
		return nil, fmt.Errorf("检查代理状态失败: %w", err)
	}
	if enabled {
		return nil, nil
	}

	configPath, _, err := p.paths()
	if err != nil {
		return nil, fmt.Errorf("获取配置路径失败: %w", err)
	}

	payload, exists, err := p.readJSONConfig(configPath)
	if err != nil {
		// 配置损坏时无法判断，按"未直连应用"处理而不是报错
		return nil, nil
	}
	if !exists || payload == nil {
		if _, statErr := os.Stat(configPath); statErr != nil {
			return nil, nil
		}
		return nil, nil
	}

	container := p.access.container(payload, false)
	if container == nil {
		return nil, nil
	}
	currentURL := anyToString(container[p.access.baseURLKey])
	currentKey := anyToString(container[p.access.authTokenKey])
	if currentURL == "" {
		return nil, nil
	}

	providers, err := loadProviderSnapshot(p.platform)
	if err != nil {
		return nil, fmt.Errorf("加载供应商配置失败: %w", err)
	}
	for _, candidate := range providers {
		if urlsEqualFold(candidate.APIURL, currentURL) && candidate.APIKey == currentKey {
			id := candidate.ID
			return &id, nil
		}
	}
	return nil, nil
}
