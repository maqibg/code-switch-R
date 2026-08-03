package services

import (
	"fmt"
	"strings"
)

// migrateGeminiExplicitCredentials 只在迁移时读取旧名称/合作方字段，之后运行时
// 使用 config_json 中的显式 CredentialType 和 EndpointKind。
func migrateGeminiExplicitCredentials(tx sqlExecutor) error {
	rows, err := tx.Query(`SELECT id, name, api_url, api_key, enabled, level, config_json FROM provider WHERE platform = 'gemini' AND source_id = ''`)
	if err != nil {
		return fmt.Errorf("读取旧 Gemini provider 失败: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		provider, err := scanProviderRow(rows)
		if err != nil {
			return fmt.Errorf("解析旧 Gemini provider 失败: %w", err)
		}
		if provider.gemini == nil || strings.TrimSpace(provider.gemini.CredentialType) != "" {
			continue
		}
		legacyProvider := provider.toGeminiProvider()
		legacyAuth := detectGeminiAuthType(&legacyProvider)
		if legacyAuth == GeminiAuthOAuth {
			provider.gemini.CredentialType = string(GeminiCredentialCLIOAuth)
			provider.gemini.EndpointKind = string(GeminiEndpointOfficial)
		} else if strings.EqualFold(provider.gemini.Category, "third_party") {
			provider.gemini.CredentialType = string(GeminiCredentialGateway)
			provider.gemini.EndpointKind = string(GeminiEndpointGateway)
		} else {
			provider.gemini.CredentialType = string(GeminiCredentialAPIKey)
			provider.gemini.EndpointKind = string(GeminiEndpointOfficial)
		}
		configJSON, err := marshalProviderConfig(provider)
		if err != nil {
			return fmt.Errorf("序列化 Gemini Credential 失败: %w", err)
		}
		if _, err := tx.Exec(`UPDATE provider SET config_json = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND platform = 'gemini' AND source_id = ''`, configJSON, provider.ID); err != nil {
			return fmt.Errorf("保存 Gemini Credential 失败: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("遍历旧 Gemini provider 失败: %w", err)
	}
	return nil
}
