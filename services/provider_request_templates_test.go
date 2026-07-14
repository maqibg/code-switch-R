package services

import (
	"path/filepath"
	"testing"
)

func TestRequestHeaderTemplateRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "templates.json")
	saved, err := saveUserRequestTemplate(path, ProviderRequestTemplate{
		Name: "Custom", Headers: map[string]string{"user-agent": "custom/1"}, MetadataUserID: `{"device_id":"local"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := loadUserRequestTemplates(path)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Headers["User-Agent"] != "custom/1" || len(loaded) != 1 || loaded[0].ID != saved.ID || loaded[0].Headers["User-Agent"] != "custom/1" {
		t.Fatalf("模板回读错误: %#v", loaded)
	}
	if err := deleteUserRequestTemplate(path, saved.ID); err != nil {
		t.Fatal(err)
	}
	loaded, err = loadUserRequestTemplates(path)
	if err != nil || len(loaded) != 0 {
		t.Fatalf("模板删除错误: templates=%#v err=%v", loaded, err)
	}
}

func TestRequestHeaderTemplateRejectsCaseInsensitiveDuplicates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "templates.json")
	_, err := saveUserRequestTemplate(path, ProviderRequestTemplate{
		Name: "Duplicate", Headers: map[string]string{"User-Agent": "first", "user-agent": "second"},
	})
	if err == nil {
		t.Fatal("仅大小写不同的重复 Header 不应保存")
	}
}

func TestBuiltInRequestHeaderTemplatesAreComplete(t *testing.T) {
	if len(builtInProviderRequestTemplates) != 2 {
		t.Fatalf("内置模板数量错误: %d", len(builtInProviderRequestTemplates))
	}
	claude := builtInProviderRequestTemplates[0]
	if claude.Headers["X-Stainless-Lang"] == "" || claude.Headers["Anthropic-Beta"] == "" {
		t.Fatalf("Claude Code 模板不完整: %#v", claude.Headers)
	}
	codex := builtInProviderRequestTemplates[1]
	if codex.Headers["Originator"] == "" || codex.Headers["X-Codex-Beta-Features"] == "" {
		t.Fatalf("Codex 模板不完整: %#v", codex.Headers)
	}
}
