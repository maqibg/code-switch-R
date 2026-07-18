package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPiProviderTemplateDefaults(t *testing.T) {
	templates, err := loadPiProviderTemplates(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(templates) != 3 {
		t.Fatalf("默认模板数量 = %d，期望 3", len(templates))
	}
	byID := map[string]PiProviderTemplate{}
	for _, template := range templates {
		byID[template.ID] = template
	}
	if byID["anthropic"].API != "anthropic-messages" || len(byID["anthropic"].KnownModels) != 5 {
		t.Fatalf("Anthropic 默认模板不完整: %#v", byID["anthropic"])
	}
	if byID["openai-codex"].API != "openai-responses" || len(byID["openai-codex"].KnownModels) != 5 {
		t.Fatalf("Codex 默认模板不完整: %#v", byID["openai-codex"])
	}
	if byID["openai-chat"].API != "openai-completions" {
		t.Fatalf("OpenAI Chat 默认模板不完整: %#v", byID["openai-chat"])
	}
}

func TestPiProviderTemplateCRUDRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "templates.json")
	template := PiProviderTemplate{
		ID: "local-chat", Name: "Local Chat", Description: "Local OpenAI-compatible server",
		API: "openai-completions", UpstreamProtocol: string(UpstreamProtocolOpenAIChat),
		DefaultEndpoint: "/v1/chat/completions", DefaultAuth: "none",
		KnownModels: map[string]PiModelEntry{
			"local-model": {ID: "local-model", Name: "Local Model", Input: []string{"text"}, ContextWindow: intPointer(8192), MaxTokens: intPointer(2048)},
		},
	}
	if err := createPiProviderTemplate(path, template); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadPiProviderTemplates(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 4 {
		t.Fatalf("创建后模板数量 = %d，期望 4", len(loaded))
	}
	template.Name = "Local Chat Updated"
	if err := updatePiProviderTemplate(path, template); err != nil {
		t.Fatal(err)
	}
	loaded, err = loadPiProviderTemplates(path)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range loaded {
		if item.ID == template.ID {
			found = item.Name == template.Name && item.KnownModels["local-model"].ID == "local-model"
		}
	}
	if !found {
		t.Fatalf("更新后的模板未正确回读: %#v", loaded)
	}
	if err := deletePiProviderTemplate(path, loaded, nil, template.ID); err != nil {
		t.Fatal(err)
	}
	loaded, err = loadPiProviderTemplates(path)
	if err != nil || len(loaded) != 3 {
		t.Fatalf("删除后模板错误: templates=%#v err=%v", loaded, err)
	}
}

func TestPiProviderTemplateRejectsDuplicateID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "templates.json")
	template := defaultPiProviderTemplates()[0]
	if err := createPiProviderTemplate(path, template); err == nil || !strings.Contains(err.Error(), "已存在") {
		t.Fatalf("重复模板 ID 应被拒绝，实际错误: %v", err)
	}
}

func TestPiProviderTemplateValidation(t *testing.T) {
	valid := defaultPiProviderTemplates()[0]
	tests := []struct {
		name   string
		mutate func(*PiProviderTemplate)
	}{
		{name: "empty id", mutate: func(value *PiProviderTemplate) { value.ID = "" }},
		{name: "invalid id", mutate: func(value *PiProviderTemplate) { value.ID = "Bad/ID" }},
		{name: "empty name", mutate: func(value *PiProviderTemplate) { value.Name = "" }},
		{name: "invalid api", mutate: func(value *PiProviderTemplate) { value.API = "unknown" }},
		{name: "mismatched protocol", mutate: func(value *PiProviderTemplate) { value.UpstreamProtocol = string(UpstreamProtocolOpenAIResponses) }},
		{name: "invalid auth", mutate: func(value *PiProviderTemplate) { value.DefaultAuth = "custom" }},
		{name: "invalid endpoint", mutate: func(value *PiProviderTemplate) { value.DefaultEndpoint = "v1/messages" }},
		{name: "mismatched model key", mutate: func(value *PiProviderTemplate) { value.KnownModels = map[string]PiModelEntry{"wrong": {ID: "actual"}} }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := clonePiProviderTemplate(valid)
			test.mutate(&candidate)
			if _, err := normalizePiProviderTemplate(candidate); err == nil {
				t.Fatalf("非法模板应被拒绝: %#v", candidate)
			}
		})
	}
}

func TestDeletePiProviderTemplateRejectsReferencedTemplate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "templates.json")
	templates := defaultPiProviderTemplates()
	err := deletePiProviderTemplate(path, templates, []Provider{
		{Name: "Beta", PiTemplate: "anthropic"},
		{Name: "Alpha", PiTemplate: "anthropic"},
	}, "anthropic")
	if err == nil || !strings.Contains(err.Error(), "2 个供应商") || !strings.Contains(err.Error(), "Alpha、Beta") {
		t.Fatalf("被引用模板应拒绝删除并列出引用者，实际错误: %v", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("拒绝删除时不应写文件: %v", statErr)
	}
}

func TestDeletePiProviderTemplateProtectsLegacyInferredReference(t *testing.T) {
	path := filepath.Join(t.TempDir(), "templates.json")
	templates := defaultPiProviderTemplates()
	err := deletePiProviderTemplate(path, templates, []Provider{
		{Name: "Legacy", UpstreamProtocol: string(UpstreamProtocolOpenAIResponses), APIEndpoint: "/v1/responses"},
	}, "openai-codex")
	if err == nil || !strings.Contains(err.Error(), "Legacy") {
		t.Fatalf("旧供应商的推断模板引用也应阻止删除，实际错误: %v", err)
	}
}

func TestPiProviderTemplateInvalidUpdatePreservesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "templates.json")
	templates := defaultPiProviderTemplates()
	if err := AtomicWriteJSON(path, templates); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	invalid := clonePiProviderTemplate(templates[0])
	invalid.API = "invalid"
	if err := updatePiProviderTemplate(path, invalid); err == nil {
		t.Fatal("非法更新应失败")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("非法更新不应破坏原模板文件")
	}
}

func clonePiProviderTemplate(source PiProviderTemplate) PiProviderTemplate {
	cloned := source
	cloned.KnownModels = make(map[string]PiModelEntry, len(source.KnownModels))
	for id, model := range source.KnownModels {
		cloned.KnownModels[id] = clonePiModelEntries([]PiModelEntry{model})[0]
	}
	return cloned
}
