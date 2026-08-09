package services

import (
	"reflect"
	"testing"
)

func TestBuildModelDiscoveryURLs(t *testing.T) {
	urls, err := buildModelDiscoveryURLs(Provider{APIURL: "https://example.com/api/v1"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"https://example.com/api/v1/models"}
	if !reflect.DeepEqual(urls, want) {
		t.Fatalf("模型 URL 期望 %#v，实际 %#v", want, urls)
	}
	urls, err = buildModelDiscoveryURLs(Provider{APIURL: "https://example.com", ModelsEndpoint: "/catalog/models"})
	if err != nil || len(urls) != 1 || urls[0] != "https://example.com/catalog/models" {
		t.Fatalf("显式模型端点错误: %#v, %v", urls, err)
	}
}

func TestParseDiscoveredModelsFormatsAndDeduplication(t *testing.T) {
	models, err := parseDiscoveredModels([]byte(`{"data":[{"id":"gpt-b"},{"id":"gpt-a","name":"A"},{"id":"gpt-a"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0].ID != "gpt-a" || models[1].ID != "gpt-b" {
		t.Fatalf("OpenAI 模型解析错误: %#v", models)
	}
	models, err = parseDiscoveredModels([]byte(`{"models":[{"name":"models/gemini-pro","displayName":"Gemini Pro"}]}`))
	if err != nil || len(models) != 1 || models[0].ID != "gemini-pro" || models[0].Name != "Gemini Pro" {
		t.Fatalf("Gemini 模型解析错误: %#v, %v", models, err)
	}
}

func TestRedactSecret(t *testing.T) {
	if got := redactSecret("request failed for sk-secret", "sk-secret"); got != "request failed for ***" {
		t.Fatalf("密钥脱敏失败: %s", got)
	}
}

func TestBuildModelDiscoveryURLsNativeGoogle(t *testing.T) {
	urls, err := buildModelDiscoveryURLsNativeGoogle(Provider{APIURL: "https://generativelanguage.googleapis.com/"})
	if err != nil || len(urls) != 1 || urls[0] != "https://generativelanguage.googleapis.com/v1beta/models" {
		t.Fatalf("Google 默认版本路径错误: %#v, %v", urls, err)
	}
	urls, err = buildModelDiscoveryURLsNativeGoogle(Provider{APIURL: "https://generativelanguage.googleapis.com/v1beta"})
	if err != nil || len(urls) != 1 || urls[0] != "https://generativelanguage.googleapis.com/v1beta/models" {
		t.Fatalf("Google v1beta 路径错误: %#v, %v", urls, err)
	}
	urls, err = buildModelDiscoveryURLsNativeGoogle(Provider{APIURL: "https://generativelanguage.googleapis.com/v1beta/models"})
	if err != nil || len(urls) != 1 || urls[0] != "https://generativelanguage.googleapis.com/v1beta/models" {
		t.Fatalf("Google 已带 models 路径错误: %#v, %v", urls, err)
	}
	urls, err = buildModelDiscoveryURLsNativeGoogle(Provider{APIURL: "https://example.com/gateway", ModelsEndpoint: "/v1beta/models"})
	if err != nil || len(urls) != 1 || urls[0] != "https://example.com/v1beta/models" {
		t.Fatalf("Google 显式端点错误: %#v, %v", urls, err)
	}
}
