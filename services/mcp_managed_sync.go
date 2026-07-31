package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

const mcpManagedStateVersion = 1

type mcpManagedState struct {
	Version int               `json:"version"`
	Entries map[string]string `json:"entries"`
}

func mcpManagedStatePath(platform string) (string, error) {
	dir, err := ensureAppConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "mcp-managed-"+platform+".json"), nil
}

func loadMCPManagedState(platform string) (mcpManagedState, string, error) {
	state := mcpManagedState{Version: mcpManagedStateVersion, Entries: map[string]string{}}
	path, err := mcpManagedStatePath(platform)
	if err != nil {
		return state, "", err
	}
	if err := ReadJSONFile(path, &state); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return state, path, nil
		}
		return state, path, fmt.Errorf("读取 MCP 托管状态失败: %w", err)
	}
	if state.Version != mcpManagedStateVersion {
		return state, path, fmt.Errorf("不支持的 MCP 托管状态版本: %d", state.Version)
	}
	if state.Entries == nil {
		state.Entries = map[string]string{}
	}
	return state, path, nil
}

func saveMCPManagedState(path string, state mcpManagedState) error {
	if len(state.Entries) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	state.Version = mcpManagedStateVersion
	return AtomicWriteJSON(path, state)
}

func rawMCPEntryMap(value any) (map[string]json.RawMessage, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	entries := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func verifyManagedMCPEntries(current map[string]json.RawMessage, state mcpManagedState) error {
	for name, expectedHash := range state.Entries {
		raw, exists := current[name]
		if !exists || canonicalJSONHash(raw) != expectedHash {
			return fmt.Errorf("MCP Server %q 已被外部修改，拒绝覆盖", name)
		}
	}
	return nil
}

func syncManagedJSONMCPServers(path, key, platform string, desired any, perm os.FileMode) error {
	desiredEntries, err := rawMCPEntryMap(desired)
	if err != nil {
		return err
	}
	data, existed, err := readOptionalFile(path)
	if err != nil {
		return err
	}
	current := map[string]json.RawMessage{}
	if existed && len(strings.TrimSpace(string(data))) > 0 {
		if !json.Valid(data) {
			return fmt.Errorf("%s 不是有效 JSON", path)
		}
		var root map[string]json.RawMessage
		if err := json.Unmarshal(data, &root); err != nil {
			return err
		}
		if raw, ok := root[key]; ok {
			if err := json.Unmarshal(raw, &current); err != nil {
				return fmt.Errorf("%s 必须是 JSON 对象", key)
			}
		}
	}
	state, statePath, err := loadMCPManagedState(platform)
	if err != nil {
		return err
	}
	if err := verifyManagedMCPEntries(current, state); err != nil {
		return err
	}
	for name := range state.Entries {
		if _, stillManaged := desiredEntries[name]; !stillManaged {
			delete(current, name)
		}
	}
	nextState := mcpManagedState{Version: mcpManagedStateVersion, Entries: map[string]string{}}
	for name, raw := range desiredEntries {
		current[name] = raw
		nextState.Entries[name] = canonicalJSONHash(raw)
	}
	if err := writeJSONMCPServersPreservingLayout(path, key, current, perm); err != nil {
		return err
	}
	if err := saveMCPManagedState(statePath, nextState); err != nil {
		if rollbackErr := restoreOptionalFile(path, data, existed); rollbackErr != nil {
			return fmt.Errorf("保存 MCP 托管状态失败: %w; 配置回滚失败: %v", err, rollbackErr)
		}
		return fmt.Errorf("保存 MCP 托管状态失败，配置已回滚: %w", err)
	}
	return nil
}

func codexMCPEntries(content []byte) (map[string]map[string]any, error) {
	entries := map[string]map[string]any{}
	if len(strings.TrimSpace(string(content))) == 0 {
		return entries, nil
	}
	var root map[string]any
	if err := toml.Unmarshal(content, &root); err != nil {
		return nil, fmt.Errorf("Codex config.toml 解析失败: %w", err)
	}
	rawServers, exists := root["mcp_servers"]
	if !exists {
		return entries, nil
	}
	servers, ok := rawServers.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("Codex mcp_servers 必须是 TOML table")
	}
	for name, raw := range servers {
		entry, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("Codex MCP Server %q 必须是 TOML table", name)
		}
		entries[name] = entry
	}
	return entries, nil
}

func codexMCPEntryHash(entry map[string]any) string {
	raw, _ := json.Marshal(entry)
	return canonicalJSONHash(raw)
}

func syncManagedCodexMCPServers(path string, desired map[string]map[string]any) error {
	content, existed, err := readOptionalFile(path)
	if err != nil {
		return err
	}
	current, err := codexMCPEntries(content)
	if err != nil {
		return err
	}
	state, statePath, err := loadMCPManagedState(platCodex)
	if err != nil {
		return err
	}
	for name, expectedHash := range state.Entries {
		entry, exists := current[name]
		if !exists || codexMCPEntryHash(entry) != expectedHash {
			return fmt.Errorf("MCP Server %q 已被外部修改，拒绝覆盖", name)
		}
	}
	for name := range state.Entries {
		if _, stillManaged := desired[name]; !stillManaged {
			delete(current, name)
		}
	}
	nextState := mcpManagedState{Version: mcpManagedStateVersion, Entries: map[string]string{}}
	for name, entry := range desired {
		current[name] = entry
		nextState.Entries[name] = codexMCPEntryHash(entry)
	}
	updated := replaceCodexMCPServersSection(string(content), buildCodexMCPServersBlock(current))
	if err := atomicWriteFile(path, []byte(updated), 0o644); err != nil {
		return err
	}
	if err := saveMCPManagedState(statePath, nextState); err != nil {
		if rollbackErr := restoreOptionalFile(path, content, existed); rollbackErr != nil {
			return fmt.Errorf("保存 Codex MCP 托管状态失败: %w; 配置回滚失败: %v", err, rollbackErr)
		}
		return fmt.Errorf("保存 Codex MCP 托管状态失败，配置已回滚: %w", err)
	}
	return nil
}
