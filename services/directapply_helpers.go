package services

import (
	"context"
	"path/filepath"
	"strings"
)

// providerFilePathNoCreate 返回 provider 配置文件路径（不创建目录）
// 用于只读操作场景，避免副作用。
//
// kind → 文件名的映射与 providerFilePath 共用 providerFileNameFor，
// 不再各自维护一份 switch——之前这里漏了 pi 和 custom，遇到它们返回空路径，
// 调用方当成"无配置"静默跳过，导致直连应用对这些平台失效且不报错。
func providerFilePathNoCreate(kind string) (string, error) {
	dir, err := getAppConfigDir()
	if err != nil {
		return "", err
	}
	filename, customToolID, err := providerFileNameFor(kind)
	if err != nil {
		// 未知平台：保持原有的"返回空路径"契约，由调用方判空处理
		return "", nil
	}
	if customToolID != "" {
		return filepath.Join(dir, "providers", filename), nil
	}
	return filepath.Join(dir, filename), nil
}

// loadProviderSnapshot 只读加载 provider 列表，用于直连应用的 provider 查找。
//
// 数据源是 provider 表（A1）。此前它直读 JSON 文件，写入切到数据库后
// 那样会读到陈旧数据，所以必须一起切换。
func loadProviderSnapshot(kind string) ([]Provider, error) {
	scope, err := scopeForKind(kind)
	if err != nil {
		// 未知平台：保持原有的"返回空"契约，由调用方判空处理
		return nil, nil
	}
	return loadProvidersFromDB(context.Background(), scope)
}

// findProviderByID 在 provider 列表中按 ID 查找
// 返回找到的 Provider 和是否找到
func findProviderByID(providers []Provider, id int64) (Provider, bool) {
	for _, p := range providers {
		if p.ID == id {
			return p, true
		}
	}
	return Provider{}, false
}

// normalizeURLTrimSlash 标准化 URL：去除所有尾部斜杠和空白
func normalizeURLTrimSlash(url string) string {
	return strings.TrimRight(strings.TrimSpace(url), "/")
}

// urlsEqualFold 不区分大小写比较两个 URL（已标准化）
func urlsEqualFold(a, b string) bool {
	return strings.EqualFold(normalizeURLTrimSlash(a), normalizeURLTrimSlash(b))
}
