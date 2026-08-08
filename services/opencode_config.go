package services

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"muzzammil.xyz/jsonc"
)

func resolveOpenCodeConfigPath(explicit string) (path, source, format string, err error) {
	if value := strings.TrimSpace(explicit); value != "" {
		return filepath.Clean(value), "custom", openCodeFormatForPath(value), nil
	}
	if value := strings.TrimSpace(os.Getenv("OPENCODE_CONFIG")); value != "" {
		return filepath.Clean(value), "env", openCodeFormatForPath(value), nil
	}
	if dir := strings.TrimSpace(os.Getenv("OPENCODE_CONFIG_DIR")); dir != "" {
		jsoncPath := filepath.Join(dir, "opencode.jsonc")
		if openCodeFileExists(jsoncPath) {
			return jsoncPath, "env_dir", "jsonc", nil
		}
		return filepath.Join(dir, "opencode.json"), "env_dir", "json", nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", "", fmt.Errorf("解析 OpenCode 用户目录失败: %w", err)
	}
	configDir := filepath.Join(home, ".config", "opencode")
	jsoncPath := filepath.Join(configDir, "opencode.jsonc")
	if openCodeFileExists(jsoncPath) {
		return jsoncPath, "default", "jsonc", nil
	}
	return filepath.Join(configDir, "opencode.json"), "default", "json", nil
}

func openCodeFormatForPath(path string) string {
	if strings.EqualFold(filepath.Ext(path), ".jsonc") {
		return "jsonc"
	}
	return "json"
}

func openCodeFileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func openCodeStatePath() (string, error) {
	dir, err := ensureAppConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "opencode-state.json"), nil
}

func loadOpenCodeState() (openCodeStateFile, error) {
	path, err := openCodeStatePath()
	if err != nil {
		return openCodeStateFile{}, err
	}
	data, readErr := os.ReadFile(path)
	if os.IsNotExist(readErr) {
		return newOpenCodeStateFile(), nil
	}
	if readErr != nil {
		return openCodeStateFile{}, fmt.Errorf("读取 OpenCode 状态失败: %w", readErr)
	}
	var state openCodeStateFile
	if err := json.Unmarshal(data, &state); err != nil {
		return openCodeStateFile{}, fmt.Errorf("解析 OpenCode 状态失败: %w", err)
	}
	if state.Version == 0 {
		state.Version = openCodeConfigStateVersion
	}
	if state.Targets == nil {
		state.Targets = make(map[string]openCodeTargetState)
	}
	if state.Managed == nil {
		state.Managed = make(map[string]openCodeManagedState)
	}
	if state.MCP == nil {
		state.MCP = make(map[string]openCodeManagedMCPState)
	}
	if state.WSL == nil {
		state.WSL = make(map[string]openCodeWSLState)
	}
	return state, nil
}

func saveOpenCodeState(state openCodeStateFile) error {
	path, err := openCodeStatePath()
	if err != nil {
		return err
	}
	state.Version = openCodeConfigStateVersion
	return AtomicWriteJSON(path, state)
}

func readOpenCodeDocument(path string) ([]byte, openCodeConfigDocument, string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, openCodeConfigDocument{
			Raw:       make(map[string]json.RawMessage),
			Providers: make(map[string]json.RawMessage),
		}, sha256Hex(nil), nil
	}
	if err != nil {
		return nil, openCodeConfigDocument{}, "", fmt.Errorf("读取 OpenCode 配置 %s 失败: %w", path, err)
	}
	return readOpenCodeDocumentFromBytes(data)
}

func readOpenCodeDocumentFromBytes(data []byte) ([]byte, openCodeConfigDocument, string, error) {
	var raw map[string]json.RawMessage
	if err := jsonc.Unmarshal(data, &raw); err != nil {
		return data, openCodeConfigDocument{}, sha256Hex(data), fmt.Errorf("解析 OpenCode 配置失败: %w", err)
	}
	if raw == nil {
		raw = make(map[string]json.RawMessage)
	}
	doc := openCodeConfigDocument{Raw: raw, Providers: make(map[string]json.RawMessage)}
	if providerRaw, exists := raw["provider"]; exists {
		if err := json.Unmarshal(providerRaw, &doc.Providers); err != nil {
			return data, openCodeConfigDocument{}, sha256Hex(data), fmt.Errorf("解析 OpenCode provider map 失败: %w", err)
		}
		if doc.Providers == nil {
			doc.Providers = make(map[string]json.RawMessage)
		}
	}
	doc.DefaultModel = rawString(raw["model"])
	doc.SmallModel = rawString(raw["small_model"])
	return data, doc, sha256Hex(data), nil
}

func rawString(raw json.RawMessage) string {
	var value string
	if len(raw) > 0 && json.Unmarshal(raw, &value) == nil {
		return value
	}
	return ""
}

func marshalOpenCodeDocument(doc openCodeConfigDocument) ([]byte, error) {
	if doc.Raw == nil {
		doc.Raw = make(map[string]json.RawMessage)
	}
	_, hasProvider := doc.Raw["provider"]
	if len(doc.Providers) > 0 || hasProvider {
		providers, err := json.Marshal(doc.Providers)
		if err != nil {
			return nil, fmt.Errorf("序列化 OpenCode provider map 失败: %w", err)
		}
		doc.Raw["provider"] = providers
	}
	data, err := json.MarshalIndent(doc.Raw, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("序列化 OpenCode 配置失败: %w", err)
	}
	return append(data, '\n'), nil
}

func writeOpenCodeDocument(path string, original []byte, data []byte) (warning string, err error) {
	current, readErr := os.ReadFile(path)
	currentExists := readErr == nil
	if readErr != nil && !os.IsNotExist(readErr) {
		return "", fmt.Errorf("校验 OpenCode 配置是否被外部修改失败: %w", readErr)
	}
	originalExists := original != nil
	if currentExists != originalExists || (currentExists && !bytes.Equal(current, original)) {
		return "", fmt.Errorf("OpenCode 配置已被外部修改，拒绝覆盖: %s", path)
	}
	if len(original) > 0 && hasJSONCComment(original) {
		backup, backupErr := CreateBackup(path)
		if backupErr != nil {
			return "", fmt.Errorf("JSONC 配置包含注释，创建备份失败: %w", backupErr)
		}
		warning = "当前解析器会规范化 JSONC 格式，已创建备份"
		if backup != "" {
			warning += ": " + backup
		}
	}
	if err := AtomicWriteBytes(path, data); err != nil {
		return warning, fmt.Errorf("写入 OpenCode 配置失败: %w", err)
	}
	return warning, nil
}

func hasJSONCComment(data []byte) bool {
	inString := false
	escaped := false
	for index := 0; index < len(data); index++ {
		char := data[index]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if char == '\\' {
				escaped = true
			} else if char == '"' {
				inString = false
			}
			continue
		}
		if char == '"' {
			inString = true
			continue
		}
		if char == '/' && index+1 < len(data) && (data[index+1] == '/' || data[index+1] == '*') {
			return true
		}
	}
	return false
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func cloneOpenCodeState(state openCodeStateFile) openCodeStateFile {
	cloned := newOpenCodeStateFile()
	cloned.Version = state.Version
	for key, value := range state.Targets {
		cloned.Targets[key] = value
	}
	for key, value := range state.Managed {
		cloned.Managed[key] = value
	}
	for key, value := range state.MCP {
		value.OriginalServer = cloneRaw(value.OriginalServer)
		value.InjectedServer = cloneRaw(value.InjectedServer)
		cloned.MCP[key] = value
	}
	for key, value := range state.WSL {
		cloned.WSL[key] = value
	}
	return cloned
}

func providerRawMap(raw json.RawMessage) (map[string]json.RawMessage, error) {
	if len(raw) == 0 {
		return make(map[string]json.RawMessage), nil
	}
	var value map[string]json.RawMessage
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	if value == nil {
		value = make(map[string]json.RawMessage)
	}
	return value, nil
}

func formatOpenCodeConfigJSON(raw json.RawMessage) (string, error) {
	if len(raw) > 0 && !strings.HasPrefix(strings.TrimSpace(string(raw)), "{") {
		return "", fmt.Errorf("OpenCode Provider 配置必须是 JSON 对象")
	}
	value, err := providerRawMap(raw)
	if err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func mergeOpenCodeJSONExtension(target map[string]json.RawMessage, rawValue, fieldName string) error {
	if strings.TrimSpace(rawValue) == "" {
		return nil
	}
	var extension map[string]json.RawMessage
	if err := json.Unmarshal([]byte(rawValue), &extension); err != nil || extension == nil {
		return fmt.Errorf("OpenCode %s 必须是 JSON 对象", fieldName)
	}
	for key, value := range extension {
		target[key] = cloneRaw(value)
	}
	return nil
}

func optionsRawMap(provider map[string]json.RawMessage) (map[string]json.RawMessage, error) {
	if raw := provider["options"]; len(raw) > 0 {
		return providerRawMap(raw)
	}
	return make(map[string]json.RawMessage), nil
}

func setRawString(target map[string]json.RawMessage, key, value string, omitEmpty bool) {
	if omitEmpty && strings.TrimSpace(value) == "" {
		delete(target, key)
		return
	}
	data, _ := json.Marshal(value)
	target[key] = data
}

func setRawBool(target map[string]json.RawMessage, key string, value bool) {
	data, _ := json.Marshal(value)
	target[key] = data
}

func sortedRawKeys(values map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func openCodeTargetInfo(path, source, format, hash string, exists bool, warning string, providerCount int, raw map[string]json.RawMessage) OpenCodeConfigInfo {
	return OpenCodeConfigInfo{
		Path: path, Source: source, Format: format, Hash: hash, Exists: exists,
		Warning: warning, ProviderCount: providerCount, TopLevelKeys: sortedRawKeys(raw),
	}
}

func openCodeTimeNow() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func openCodeProviderModelMap(rawProvider map[string]json.RawMessage) (map[string]json.RawMessage, error) {
	raw := rawProvider["models"]
	if len(raw) == 0 {
		return make(map[string]json.RawMessage), nil
	}
	var models map[string]json.RawMessage
	if err := json.Unmarshal(raw, &models); err != nil {
		return nil, fmt.Errorf("解析 OpenCode models map 失败: %w", err)
	}
	if models == nil {
		models = make(map[string]json.RawMessage)
	}
	return models, nil
}

func buildModelRaw(input OpenCodeModelInput, existing json.RawMessage) (json.RawMessage, error) {
	model, err := providerRawMap(existing)
	if err != nil {
		return nil, fmt.Errorf("解析模型 %s 失败: %w", input.ID, err)
	}
	if err := mergeOpenCodeJSONExtension(model, input.ExtraJSON, fmt.Sprintf("模型 %s 扩展字段", input.ID)); err != nil {
		return nil, err
	}
	setRawString(model, "name", input.Name, true)
	limit := make(map[string]json.RawMessage)
	if raw := model["limit"]; len(raw) > 0 {
		limit, _ = providerRawMap(raw)
	}
	if input.ContextLimit > 0 {
		data, _ := json.Marshal(input.ContextLimit)
		limit["context"] = data
	}
	if input.InputLimit > 0 {
		data, _ := json.Marshal(input.InputLimit)
		limit["input"] = data
	}
	if input.OutputLimit > 0 {
		data, _ := json.Marshal(input.OutputLimit)
		limit["output"] = data
	}
	if len(limit) > 0 {
		data, _ := json.Marshal(limit)
		model["limit"] = data
	}
	setRawBool(model, "reasoning", input.Reasoning)
	setRawBool(model, "tool_call", input.ToolCall)
	setRawBool(model, "attachment", input.Attachment)
	if input.Modalities != nil {
		if len(input.Modalities) == 0 {
			delete(model, "modalities")
		} else {
			data, _ := json.Marshal(input.Modalities)
			model["modalities"] = data
		}
	}
	if input.Variants != nil {
		if len(input.Variants) == 0 {
			delete(model, "variants")
		} else {
			data, _ := json.Marshal(input.Variants)
			model["variants"] = data
		}
	}
	return json.Marshal(model)
}
