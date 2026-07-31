package services

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSanitizeConfigValueRemovesCredentialsAndPreservesPlaceholders(t *testing.T) {
	input := map[string]any{
		"apiKey":   "secret-key",
		"url":      "https://example.com/mcp?apiKey=secret-query&mode=fast",
		"template": "https://example.com/mcp?apiKey={apiKey}",
		"env":      map[string]any{"GITHUB_TOKEN": "secret-token", "MODE": "safe"},
		"args":     []any{"--api-key", "secret-arg", "--mode=fast"},
	}
	sanitized := sanitizeConfigValue("root", input).(map[string]any)
	if _, exists := sanitized["apiKey"]; exists {
		t.Fatal("apiKey 不应出现在脱敏配置中")
	}
	if strings.Contains(sanitized["url"].(string), "secret-query") || !strings.Contains(sanitized["url"].(string), "mode=fast") {
		t.Fatalf("URL 脱敏错误: %s", sanitized["url"])
	}
	if !strings.Contains(sanitized["template"].(string), "%7BapiKey%7D") && !strings.Contains(sanitized["template"].(string), "{apiKey}") {
		t.Fatalf("占位符不应被删除: %s", sanitized["template"])
	}
	if _, exists := sanitized["env"].(map[string]any)["GITHUB_TOKEN"]; exists {
		t.Fatal("敏感环境变量不应导出")
	}
	args := sanitized["args"].([]any)
	if len(args) != 1 || args[0] != "--mode=fast" {
		t.Fatalf("命令参数脱敏错误: %#v", args)
	}
}

func TestEncryptedBackupRoundTrip(t *testing.T) {
	source := t.TempDir()
	if err := os.MkdirAll(filepath.Join(source, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "nested", "secret.txt"), []byte("credential payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	var encrypted bytes.Buffer
	if err := encryptSnapshotDirectory(source, "correct horse battery staple", &encrypted); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encrypted.Bytes(), []byte("credential payload")) {
		t.Fatal("加密备份不应包含明文内容")
	}
	target := t.TempDir()
	count, err := decryptBackupToDirectory(bytes.NewReader(encrypted.Bytes()), "correct horse battery staple", target)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("解密文件数错误: %d", count)
	}
	data, err := os.ReadFile(filepath.Join(target, "nested", "secret.txt"))
	if err != nil || string(data) != "credential payload" {
		t.Fatalf("解密内容错误: %q err=%v", data, err)
	}
	if _, err := decryptBackupToDirectory(bytes.NewReader(encrypted.Bytes()), "wrong password", t.TempDir()); err == nil {
		t.Fatal("错误密码不应解密成功")
	}

	truncated := append([]byte(nil), encrypted.Bytes()[:encrypted.Len()-1]...)
	if _, err := decryptBackupToDirectory(bytes.NewReader(truncated), "correct horse battery staple", t.TempDir()); err == nil {
		t.Fatal("截断的加密备份不应恢复成功")
	}
	tampered := append(append([]byte(nil), encrypted.Bytes()...), 0x01)
	if _, err := decryptBackupToDirectory(bytes.NewReader(tampered), "correct horse battery staple", t.TempDir()); err == nil {
		t.Fatal("带尾随数据的加密备份不应恢复成功")
	}
}

func TestConfigMigrationPreservesDestinationCredentials(t *testing.T) {
	setupRenameTestEnv(t)
	service := NewProviderService()
	exported := Provider{
		Name: "Migrated", APIURL: "https://example.com/v1?apiKey=source-query", APIKey: "source-key",
		Enabled: true, Level: 3, UpstreamProtocol: "anthropic", Headers: map[string]string{"X-Trace": "from-export"},
		RequestIdentity: &ProviderRequestIdentity{MetadataMode: ProviderMetadataModeFixed, MetadataUserID: "source-identity"},
	}
	if err := service.SaveProviders("claude", []Provider{exported}); err != nil {
		t.Fatal(err)
	}
	transfer := NewImportService(service, NewMCPService(), NewAppSettingsService(nil))
	packagePath := filepath.Join(t.TempDir(), "config.csrconfig")
	if _, err := transfer.ExportSanitizedConfig(packagePath); err != nil {
		t.Fatal(err)
	}
	packageData, err := os.ReadFile(packagePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"source-key", "source-query", "source-identity"} {
		if bytes.Contains(packageData, []byte(secret)) {
			t.Fatalf("配置迁移包泄漏敏感值 %q", secret)
		}
	}

	current := exported
	current.APIURL = "https://example.com/v1?apiKey=destination-query"
	current.APIKey = "destination-key"
	current.Level = 9
	current.Headers = map[string]string{"X-Local": "keep"}
	current.RequestIdentity = &ProviderRequestIdentity{MetadataMode: ProviderMetadataModeFixed, MetadataUserID: "destination-identity"}
	if err := service.SaveProviders("claude", []Provider{current}); err != nil {
		t.Fatal(err)
	}
	if _, err := transfer.ImportSanitizedConfig(packagePath); err != nil {
		t.Fatal(err)
	}
	loaded, err := service.LoadProviders("claude")
	if err != nil || len(loaded) != 1 {
		t.Fatalf("读取导入结果失败: providers=%#v err=%v", loaded, err)
	}
	got := loaded[0]
	if got.APIKey != "destination-key" || !strings.Contains(got.APIURL, "destination-query") || got.RequestIdentity == nil || got.RequestIdentity.MetadataUserID != "destination-identity" {
		t.Fatalf("目标凭据未保留: %#v", got)
	}
	if got.Level != 3 || got.Headers["X-Trace"] != "from-export" || got.Headers["X-Local"] != "keep" {
		t.Fatalf("配置字段合并错误: %#v", got)
	}
}
