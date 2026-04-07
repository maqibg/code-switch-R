package services

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/pelletier/go-toml/v2"
	"github.com/tidwall/sjson"
)

const (
	mcpLegacyStoreFile = "mcp.json"
	claudeMcpFile      = ".claude.json"
	codexDirName       = ".codex"
	codexConfigFile    = "config.toml"
	geminiDirName      = ".gemini"
	geminiConfigFile   = "settings.json"
	platClaudeCode     = "claude-code"
	platCodex          = "codex"
	platGemini         = "gemini"
)

var builtInServers = map[string]rawMCPServer{
	"reftools": {
		Type:    "http",
		URL:     "https://api.ref.tools/mcp?apiKey={apiKey}",
		Website: "https://ref.tools",
		Tips:    "Visit ref.tools to claim your API key.",
	},
	"chrome-devtools": {
		Type:    "stdio",
		Command: "npx",
		Args:    []string{"-y", "chrome-devtools-mcp@latest"},
		Tips:    "Needs Node.js. Run once to install dependencies.",
	},
}

var placeholderPattern = regexp.MustCompile(`\{([a-zA-Z0-9_]+)\}`)
var tomlBareKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

type MCPService struct {
	mu sync.Mutex
}

func NewMCPService() *MCPService {
	return &MCPService{}
}

func boolPtr(v bool) *bool {
	return &v
}

func platformStoreFile(platform string) (string, error) {
	normalized, ok := normalizePlatform(platform)
	if !ok {
		return "", fmt.Errorf("未知 MCP 平台: %s", platform)
	}
	return fmt.Sprintf("mcp-%s.json", normalized), nil
}

type MCPServer struct {
	Name                string            `json:"name"`
	Type                string            `json:"type"`
	Command             string            `json:"command,omitempty"`
	Args                []string          `json:"args,omitempty"`
	Env                 map[string]string `json:"env,omitempty"`
	URL                 string            `json:"url,omitempty"`
	Website             string            `json:"website,omitempty"`
	Tips                string            `json:"tips,omitempty"`
	Enabled             bool              `json:"enabled"`
	EnablePlatform      []string          `json:"enable_platform"`
	EnabledInClaude     bool              `json:"enabled_in_claude"`
	EnabledInCodex      bool              `json:"enabled_in_codex"`
	EnabledInGemini     bool              `json:"enabled_in_gemini"`
	MissingPlaceholders []string          `json:"missing_placeholders"`
}

type rawMCPServer struct {
	Type           string            `json:"type"`
	Command        string            `json:"command,omitempty"`
	Args           []string          `json:"args,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	URL            string            `json:"url,omitempty"`
	Website        string            `json:"website,omitempty"`
	Tips           string            `json:"tips,omitempty"`
	Enabled        *bool             `json:"enabled,omitempty"`
	EnablePlatform []string          `json:"enable_platform"`
}

type mcpStorePayload struct {
	Servers         map[string]rawMCPServer `json:"servers"`
	DeletedBuiltins []string                `json:"deletedBuiltins,omitempty"`
}

type mcpPlatformStore struct {
	Servers         map[string]rawMCPServer `json:"servers"`
	DeletedBuiltins []string                `json:"deletedBuiltins,omitempty"`
}

type claudeMcpFilePayload struct {
	Servers map[string]json.RawMessage `json:"mcpServers"`
}

type geminiMcpFilePayload struct {
	Servers map[string]json.RawMessage `json:"mcpServers"`
}

type codexMcpFilePayload struct {
	Servers map[string]map[string]any `toml:"mcp_servers"`
}

type claudeDesktopServer struct {
	Type    string            `json:"type,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	URL     string            `json:"url,omitempty"`
}

func (ms *MCPService) ListServers() ([]MCPServer, error) {
	return ms.ListServersForPlatform(platClaudeCode)
}

func (ms *MCPService) ListServersForPlatform(platform string) ([]MCPServer, error) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	normalizedPlatform, ok := normalizePlatform(platform)
	if !ok {
		return nil, fmt.Errorf("未知 MCP 平台: %s", platform)
	}

	config, err := ms.loadConfig(normalizedPlatform)
	if err != nil {
		return nil, err
	}

	claudeEnabled := loadClaudeEnabledServers()
	codexEnabled := loadCodexEnabledServers()
	geminiEnabled := loadGeminiEnabledServers()

	names := make([]string, 0, len(config))
	for name := range config {
		names = append(names, name)
	}
	sort.Strings(names)

	servers := make([]MCPServer, 0, len(names))
	for _, name := range names {
		entry := config[name]
		typ := normalizeServerType(entry.Type)
		server := MCPServer{
			Name:            name,
			Type:            typ,
			Command:         strings.TrimSpace(entry.Command),
			Args:            cloneArgs(entry.Args),
			Env:             cloneEnv(entry.Env),
			URL:             strings.TrimSpace(entry.URL),
			Website:         strings.TrimSpace(entry.Website),
			Tips:            strings.TrimSpace(entry.Tips),
			Enabled:         entry.Enabled == nil || *entry.Enabled,
			EnablePlatform:  []string{normalizedPlatform},
			EnabledInClaude: containsNormalized(claudeEnabled, name),
			EnabledInCodex:  containsNormalized(codexEnabled, name),
			EnabledInGemini: containsNormalized(geminiEnabled, name),
		}
		server.MissingPlaceholders = detectPlaceholders(server.URL, server.Args)
		servers = append(servers, server)
	}

	return servers, nil
}

func (ms *MCPService) SaveServers(servers []MCPServer) error {
	return ms.SaveServersForPlatform(platClaudeCode, servers)
}

func (ms *MCPService) SaveServersForPlatform(platform string, servers []MCPServer) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	normalizedPlatform, ok := normalizePlatform(platform)
	if !ok {
		return fmt.Errorf("未知 MCP 平台: %s", platform)
	}

	normalized := make([]MCPServer, len(servers))
	raw := make(map[string]rawMCPServer, len(servers))
	for i := range servers {
		server := servers[i]
		name := strings.TrimSpace(server.Name)
		if name == "" {
			return fmt.Errorf("server name 不能为空")
		}
		typ := normalizeServerType(server.Type)
		platforms := []string{normalizedPlatform}
		args := cleanArgs(server.Args)
		env := cleanEnv(server.Env)
		command := strings.TrimSpace(server.Command)
		url := strings.TrimSpace(server.URL)
		if typ == "stdio" && command == "" {
			return fmt.Errorf("%s 需要提供 command", name)
		}
		if typ == "http" && url == "" {
			return fmt.Errorf("%s 需要提供 url", name)
		}
		normalized[i] = MCPServer{
			Name:            name,
			Type:            typ,
			Command:         command,
			Args:            args,
			Env:             env,
			URL:             url,
			Website:         strings.TrimSpace(server.Website),
			Tips:            strings.TrimSpace(server.Tips),
			Enabled:         server.Enabled,
			EnablePlatform:  platforms,
			EnabledInClaude: server.EnabledInClaude,
			EnabledInCodex:  server.EnabledInCodex,
			EnabledInGemini: server.EnabledInGemini,
		}
		raw[name] = rawMCPServer{
			Type:           typ,
			Command:        command,
			Args:           args,
			Env:            env,
			URL:            url,
			Website:        normalized[i].Website,
			Tips:           normalized[i].Tips,
			Enabled:        boolPtr(server.Enabled),
			EnablePlatform: platforms,
		}
		placeholders := detectPlaceholders(url, args)
		normalized[i].MissingPlaceholders = placeholders
		if len(placeholders) > 0 {
			normalized[i].Enabled = false
			normalized[i].EnablePlatform = []string{}
			rawEntry := raw[name]
			rawEntry.Enabled = boolPtr(false)
			rawEntry.EnablePlatform = []string{}
			raw[name] = rawEntry
		}
	}

	// Calculate deletedBuiltins: built-in servers that are missing from raw
	var deletedBuiltins []string
	for builtInName := range builtInServers {
		if _, exists := raw[builtInName]; !exists {
			deletedBuiltins = append(deletedBuiltins, builtInName)
		}
	}
	sort.Strings(deletedBuiltins)

	if err := ms.saveStore(normalizedPlatform, raw, deletedBuiltins); err != nil {
		return err
	}
	switch normalizedPlatform {
	case platClaudeCode:
		if err := ms.syncClaudeServers(normalized); err != nil {
			return err
		}
	case platCodex:
		if err := ms.syncCodexServers(normalized); err != nil {
			return err
		}
	case platGemini:
		if err := ms.syncGeminiServers(normalized); err != nil {
			return err
		}
	}
	return nil
}

func (ms *MCPService) configPath(platform string) (string, error) {
	dir, err := ensureAppConfigDir()
	if err != nil {
		return "", err
	}
	file, err := platformStoreFile(platform)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, file), nil
}

func (ms *MCPService) legacyConfigPath() (string, error) {
	dir, err := ensureAppConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, mcpLegacyStoreFile), nil
}

func (ms *MCPService) loadLegacySharedConfig() (map[string]rawMCPServer, []string, error) {
	path, err := ms.legacyConfigPath()
	if err != nil {
		return nil, nil, err
	}

	servers := map[string]rawMCPServer{}
	var deletedBuiltins []string
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return servers, deletedBuiltins, nil
		}
		return nil, nil, err
	}
	if len(data) == 0 {
		return servers, deletedBuiltins, nil
	}

	var storePayload mcpStorePayload
	if err := json.Unmarshal(data, &storePayload); err == nil && storePayload.Servers != nil {
		servers = storePayload.Servers
		deletedBuiltins = storePayload.DeletedBuiltins
	} else {
		if err := json.Unmarshal(data, &servers); err != nil {
			return nil, nil, err
		}
	}

	for name, entry := range servers {
		servers[name] = normalizeRawEntry(entry)
	}
	return servers, deletedBuiltins, nil
}

func selectPlatformServers(servers map[string]rawMCPServer, platform string) map[string]rawMCPServer {
	selected := make(map[string]rawMCPServer)
	for name, entry := range servers {
		if platformContains(normalizePlatforms(entry.EnablePlatform), platform) {
			normalized := normalizeRawEntry(entry)
			normalized.EnablePlatform = []string{platform}
			selected[name] = normalized
		}
	}
	return selected
}

func (ms *MCPService) loadConfig(platform string) (map[string]rawMCPServer, error) {
	path, err := ms.configPath(platform)
	if err != nil {
		return nil, err
	}

	servers := map[string]rawMCPServer{}
	var deletedBuiltins []string

	if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
		// Try parsing new format first (with servers and deletedBuiltins)
		var storePayload mcpStorePayload
		if err := json.Unmarshal(data, &storePayload); err == nil && storePayload.Servers != nil {
			servers = storePayload.Servers
			deletedBuiltins = storePayload.DeletedBuiltins
		} else {
			// Fall back to legacy flat format for backward compatibility
			if err := json.Unmarshal(data, &servers); err != nil {
				return nil, err
			}
		}
	} else if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		legacyServers, legacyDeletedBuiltins, legacyErr := ms.loadLegacySharedConfig()
		if legacyErr != nil {
			return nil, legacyErr
		}
		servers = selectPlatformServers(legacyServers, platform)
		deletedBuiltins = legacyDeletedBuiltins
	}

	for name, entry := range servers {
		servers[name] = normalizeRawEntry(entry)
	}

	changed := false
	if platform == platClaudeCode {
		if imported, err := ms.importFromClaude(servers); err == nil {
			if ms.mergeImportedServers(servers, imported) {
				changed = true
			}
		} else {
			return nil, err
		}
	}

	if ensureBuiltInServers(servers, deletedBuiltins) {
		changed = true
	}

	if changed {
		if err := ms.saveStore(platform, servers, deletedBuiltins); err != nil {
			return servers, err
		}
	}

	return servers, nil
}

func (ms *MCPService) importFromClaude(existing map[string]rawMCPServer) (map[string]rawMCPServer, error) {
	path, err := claudeConfigPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]rawMCPServer{}, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return map[string]rawMCPServer{}, nil
	}
	var payload struct {
		Servers map[string]claudeDesktopServer `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	result := make(map[string]rawMCPServer, len(payload.Servers))
	for name, entry := range payload.Servers {
		trimmedName := strings.TrimSpace(name)
		if trimmedName == "" {
			continue
		}
		if _, exists := existing[trimmedName]; exists {
			continue
		}
		typeHint := entry.Type
		if strings.TrimSpace(typeHint) == "" {
			if strings.TrimSpace(entry.URL) != "" {
				typeHint = "http"
			}
		}
		if strings.TrimSpace(typeHint) == "" {
			typeHint = "stdio"
		}
		typ := normalizeServerType(typeHint)
		if typ == "http" && entry.URL == "" {
			continue
		}
		if typ == "stdio" && entry.Command == "" {
			continue
		}
		result[trimmedName] = rawMCPServer{
			Type:           typ,
			Command:        strings.TrimSpace(entry.Command),
			Args:           cleanArgs(entry.Args),
			Env:            cleanEnv(entry.Env),
			URL:            strings.TrimSpace(entry.URL),
			Enabled:        boolPtr(true),
			EnablePlatform: []string{platClaudeCode},
		}
	}
	return result, nil
}

func (ms *MCPService) saveStore(platform string, servers map[string]rawMCPServer, deletedBuiltins []string) error {
	path, err := ms.configPath(platform)
	if err != nil {
		return err
	}
	storePayload := mcpStorePayload{
		Servers:         servers,
		DeletedBuiltins: deletedBuiltins,
	}
	data, err := json.MarshalIndent(storePayload, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func normalizeServerType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "http":
		return "http"
	default:
		return "stdio"
	}
}

func normalizePlatforms(values []string) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0, len(values))
	for _, raw := range values {
		if platform, ok := normalizePlatform(raw); ok {
			if _, exists := seen[platform]; exists {
				continue
			}
			seen[platform] = struct{}{}
			result = append(result, platform)
		}
	}
	return result
}

func normalizePlatform(value string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "claude", "claude_code", "claude-code":
		return "claude-code", true
	case "codex":
		return "codex", true
	case "gemini", "gemini-cli", "gemini_cli":
		return "gemini", true
	default:
		return "", false
	}
}

func unionPlatforms(primary, secondary []string) []string {
	combined := append([]string{}, primary...)
	combined = append(combined, secondary...)
	return normalizePlatforms(combined)
}

func normalizeRawEntry(entry rawMCPServer) rawMCPServer {
	entry.Type = normalizeServerType(entry.Type)
	entry.Command = strings.TrimSpace(entry.Command)
	entry.URL = strings.TrimSpace(entry.URL)
	entry.Website = strings.TrimSpace(entry.Website)
	entry.Tips = strings.TrimSpace(entry.Tips)
	entry.Args = cleanArgs(entry.Args)
	entry.Env = cleanEnv(entry.Env)
	if entry.Enabled == nil {
		entry.Enabled = boolPtr(true)
	}
	entry.EnablePlatform = normalizePlatforms(entry.EnablePlatform)
	return entry
}

func cloneArgs(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	dup := make([]string, len(values))
	copy(dup, values)
	return dup
}

func cloneEnv(values map[string]string) map[string]string {
	if len(values) == 0 {
		return map[string]string{}
	}
	dup := make(map[string]string, len(values))
	for k, v := range values {
		dup[k] = v
	}
	return dup
}

func cleanArgs(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	result := make([]string, 0, len(values))
	for _, v := range values {
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			continue
		}
		result = append(result, trimmed)
	}
	return result
}

func cleanEnv(values map[string]string) map[string]string {
	if len(values) == 0 {
		return map[string]string{}
	}
	result := make(map[string]string, len(values))
	for key, value := range values {
		trimmedKey := strings.TrimSpace(key)
		if trimmedKey == "" {
			continue
		}
		result[trimmedKey] = strings.TrimSpace(value)
	}
	return result
}

func containsNormalized(pool map[string]struct{}, value string) bool {
	if len(pool) == 0 {
		return false
	}
	_, ok := pool[strings.ToLower(strings.TrimSpace(value))]
	return ok
}

func loadClaudeEnabledServers() map[string]struct{} {
	result := map[string]struct{}{}
	home, err := os.UserHomeDir()
	if err != nil {
		return result
	}
	path := filepath.Join(home, claudeMcpFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return result
	}
	var payload claudeMcpFilePayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return result
	}
	for name := range payload.Servers {
		result[strings.ToLower(strings.TrimSpace(name))] = struct{}{}
	}
	return result
}

func loadCodexEnabledServers() map[string]struct{} {
	result := map[string]struct{}{}
	home, err := os.UserHomeDir()
	if err != nil {
		return result
	}
	path := filepath.Join(home, codexDirName, codexConfigFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return result
	}
	var payload codexMcpFilePayload
	if err := toml.Unmarshal(data, &payload); err != nil {
		return result
	}
	for name := range payload.Servers {
		result[strings.ToLower(strings.TrimSpace(name))] = struct{}{}
	}
	return result
}

func loadGeminiEnabledServers() map[string]struct{} {
	result := map[string]struct{}{}
	home, err := os.UserHomeDir()
	if err != nil {
		return result
	}
	path := filepath.Join(home, geminiDirName, geminiConfigFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return result
	}
	var payload geminiMcpFilePayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return result
	}
	for name := range payload.Servers {
		result[strings.ToLower(strings.TrimSpace(name))] = struct{}{}
	}
	return result
}

func (ms *MCPService) mergeImportedServers(target, imported map[string]rawMCPServer) bool {
	changed := false
	for name, entry := range imported {
		entry = normalizeRawEntry(entry)
		if existing, ok := target[name]; ok {
			entry.EnablePlatform = unionPlatforms(existing.EnablePlatform, entry.EnablePlatform)
			if entry.Website == "" {
				entry.Website = existing.Website
			}
			if entry.Tips == "" {
				entry.Tips = existing.Tips
			}
		}
		if existing, ok := target[name]; !ok || !reflect.DeepEqual(existing, entry) {
			target[name] = entry
			changed = true
		}
	}
	return changed
}

func ensureBuiltInServers(target map[string]rawMCPServer, deletedBuiltins []string) bool {
	deletedSet := make(map[string]struct{}, len(deletedBuiltins))
	for _, name := range deletedBuiltins {
		deletedSet[strings.ToLower(strings.TrimSpace(name))] = struct{}{}
	}

	changed := false
	for name, builtIn := range builtInServers {
		// Skip deleted built-in servers (tombstone check)
		if _, deleted := deletedSet[strings.ToLower(name)]; deleted {
			continue
		}

		builtIn = normalizeRawEntry(builtIn)
		if existing, ok := target[name]; ok {
			merged := existing
			merged.EnablePlatform = unionPlatforms(existing.EnablePlatform, builtIn.EnablePlatform)
			if merged.Command == "" {
				merged.Command = builtIn.Command
			}
			if len(merged.Args) == 0 {
				merged.Args = builtIn.Args
			}
			if len(merged.Env) == 0 {
				merged.Env = builtIn.Env
			}
			if merged.URL == "" {
				merged.URL = builtIn.URL
			}
			if merged.Website == "" {
				merged.Website = builtIn.Website
			}
			if merged.Tips == "" {
				merged.Tips = builtIn.Tips
			}
			merged = normalizeRawEntry(merged)
			if !reflect.DeepEqual(existing, merged) {
				target[name] = merged
				changed = true
			}
			continue
		}
		target[name] = builtIn
		changed = true
	}
	return changed
}

func (ms *MCPService) syncClaudeServers(servers []MCPServer) error {
	path, err := claudeConfigPath()
	if err != nil {
		return err
	}
	desired := make(map[string]claudeDesktopServer)
	for _, server := range servers {
		if !server.Enabled || !platformContains(server.EnablePlatform, platClaudeCode) {
			continue
		}
		desired[server.Name] = buildClaudeDesktopEntry(server)
	}
	return writeJSONMCPServersPreservingLayout(path, "mcpServers", desired, 0o600)
}

func (ms *MCPService) syncCodexServers(servers []MCPServer) error {
	path, err := codexConfigPath()
	if err != nil {
		return err
	}
	desired := make(map[string]map[string]any)
	for _, server := range servers {
		if !server.Enabled || !platformContains(server.EnablePlatform, platCodex) {
			continue
		}
		desired[server.Name] = buildCodexEntry(server)
	}
	content, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	block := buildCodexMCPServersBlock(desired)
	updated := replaceCodexMCPServersSection(string(content), block)
	return os.WriteFile(path, []byte(updated), 0o644)
}

func (ms *MCPService) syncGeminiServers(servers []MCPServer) error {
	path, err := geminiConfigPath()
	if err != nil {
		return err
	}
	desired := make(map[string]map[string]any)
	for _, server := range servers {
		if !server.Enabled || !platformContains(server.EnablePlatform, platGemini) {
			continue
		}
		desired[server.Name] = buildGeminiEntry(server)
	}
	return writeJSONMCPServersPreservingLayout(path, "mcpServers", desired, 0o644)
}

func platformContains(platforms []string, target string) bool {
	for _, value := range platforms {
		if value == target {
			return true
		}
	}
	return false
}

func buildClaudeDesktopEntry(server MCPServer) claudeDesktopServer {
	entry := claudeDesktopServer{Type: server.Type}
	if server.Type == "http" {
		entry.URL = server.URL
	} else {
		entry.Command = server.Command
		if len(server.Args) > 0 {
			entry.Args = server.Args
		}
		if len(server.Env) > 0 {
			entry.Env = server.Env
		}
	}
	return entry
}

func buildCodexEntry(server MCPServer) map[string]any {
	entry := make(map[string]any)
	entry["type"] = server.Type
	if server.Type == "http" {
		entry["url"] = server.URL
	} else {
		entry["command"] = server.Command
		if len(server.Args) > 0 {
			entry["args"] = server.Args
		}
		if len(server.Env) > 0 {
			entry["env"] = server.Env
		}
	}
	return entry
}

// buildGeminiEntry creates Gemini CLI MCP server config.
// Gemini uses "httpUrl" (not "url") for HTTP type, and omits the "type" field.
func buildGeminiEntry(server MCPServer) map[string]any {
	entry := make(map[string]any)
	if server.Type == "http" {
		entry["httpUrl"] = server.URL
	} else {
		entry["command"] = server.Command
		if len(server.Args) > 0 {
			entry["args"] = server.Args
		}
		if len(server.Env) > 0 {
			entry["env"] = server.Env
		}
	}
	return entry
}

func writeJSONMCPServersPreservingLayout(path string, key string, desired any, perm os.FileMode) error {
	desiredBytes, err := json.MarshalIndent(desired, "", "  ")
	if err != nil {
		return err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		fresh, marshalErr := json.MarshalIndent(map[string]any{key: desired}, "", "  ")
		if marshalErr != nil {
			return marshalErr
		}
		return os.WriteFile(path, fresh, perm)
	}

	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		fresh, marshalErr := json.MarshalIndent(map[string]any{key: desired}, "", "  ")
		if marshalErr != nil {
			return marshalErr
		}
		return os.WriteFile(path, fresh, perm)
	}

	if !json.Valid(trimmed) {
		return fmt.Errorf("%s 不是有效 JSON，无法手术式写入 %s", path, key)
	}

	updated, err := sjson.SetRawBytes(data, key, desiredBytes)
	if err != nil {
		return err
	}
	return os.WriteFile(path, updated, perm)
}

func tomlSectionKey(name string) string {
	if tomlBareKeyPattern.MatchString(name) {
		return name
	}
	return strconv.Quote(name)
}

func buildTomlStringArray(values []string) string {
	if len(values) == 0 {
		return "[]"
	}
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, strconv.Quote(value))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func buildCodexMCPServersBlock(desired map[string]map[string]any) string {
	if len(desired) == 0 {
		return ""
	}

	names := make([]string, 0, len(desired))
	for name := range desired {
		names = append(names, name)
	}
	sort.Strings(names)

	var out []string
	for _, name := range names {
		entry := desired[name]
		section := tomlSectionKey(name)
		out = append(out, fmt.Sprintf("[mcp_servers.%s]", section))

		if typeValue, _ := entry["type"].(string); strings.TrimSpace(typeValue) != "" {
			out = append(out, fmt.Sprintf("type = %s", strconv.Quote(typeValue)))
		}
		if urlValue, _ := entry["url"].(string); strings.TrimSpace(urlValue) != "" {
			out = append(out, fmt.Sprintf("url = %s", strconv.Quote(urlValue)))
		}
		if commandValue, _ := entry["command"].(string); strings.TrimSpace(commandValue) != "" {
			out = append(out, fmt.Sprintf("command = %s", strconv.Quote(commandValue)))
		}
		if argsValue, ok := entry["args"].([]string); ok && len(argsValue) > 0 {
			out = append(out, fmt.Sprintf("args = %s", buildTomlStringArray(argsValue)))
		} else if argsValue, ok := entry["args"].([]any); ok && len(argsValue) > 0 {
			args := make([]string, 0, len(argsValue))
			for _, item := range argsValue {
				args = append(args, fmt.Sprint(item))
			}
			out = append(out, fmt.Sprintf("args = %s", buildTomlStringArray(args)))
		}

		if envValue, ok := entry["env"].(map[string]string); ok && len(envValue) > 0 {
			out = append(out, fmt.Sprintf("[mcp_servers.%s.env]", section))
			keys := make([]string, 0, len(envValue))
			for key := range envValue {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				out = append(out, fmt.Sprintf("%s = %s", tomlSectionKey(key), strconv.Quote(envValue[key])))
			}
		} else if envValue, ok := entry["env"].(map[string]any); ok && len(envValue) > 0 {
			out = append(out, fmt.Sprintf("[mcp_servers.%s.env]", section))
			keys := make([]string, 0, len(envValue))
			for key := range envValue {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				out = append(out, fmt.Sprintf("%s = %s", tomlSectionKey(key), strconv.Quote(fmt.Sprint(envValue[key]))))
			}
		}

		out = append(out, "")
	}

	return strings.TrimRight(strings.Join(out, "\n"), "\n")
}

func extractTomlHeader(line string) string {
	matches := regexp.MustCompile(`^\s*(\[[^\]]+\])`).FindStringSubmatch(line)
	if len(matches) < 2 {
		return ""
	}
	return matches[1]
}

func isCodexMCPHeader(header string) bool {
	return header == "[mcp_servers]" || strings.HasPrefix(header, "[mcp_servers.")
}

func replaceCodexMCPServersSection(content string, block string) string {
	newline := "\n"
	if strings.Contains(content, "\r\n") {
		newline = "\r\n"
		content = strings.ReplaceAll(content, "\r\n", "\n")
	}

	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines)+8)
	blockLines := []string{}
	if block != "" {
		blockLines = strings.Split(block, "\n")
	}

	inserted := false
	skipping := false
	insertBlock := func() {
		if inserted || len(blockLines) == 0 {
			inserted = true
			return
		}
		if len(out) > 0 && strings.TrimSpace(out[len(out)-1]) != "" {
			out = append(out, "")
		}
		out = append(out, blockLines...)
		inserted = true
	}

	for _, line := range lines {
		header := extractTomlHeader(line)

		if skipping {
			if header != "" {
				if isCodexMCPHeader(header) {
					continue
				}
				insertBlock()
				skipping = false
			} else {
				continue
			}
		}

		if header != "" && isCodexMCPHeader(header) {
			skipping = true
			continue
		}

		out = append(out, line)
	}

	if skipping {
		insertBlock()
	}
	if !inserted && len(blockLines) > 0 {
		insertBlock()
	}

	result := strings.Join(out, "\n")
	if strings.HasSuffix(content, "\n") || len(blockLines) > 0 {
		result = strings.TrimRight(result, "\n") + "\n"
	}
	if newline != "\n" {
		result = strings.ReplaceAll(result, "\n", newline)
	}
	return result
}

func claudeConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, claudeMcpFile), nil
}

func codexConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, codexDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, codexConfigFile), nil
}

func geminiConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, geminiDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, geminiConfigFile), nil
}

func detectPlaceholders(url string, args []string) []string {
	set := make(map[string]struct{})
	collectPlaceholders(set, url)
	for _, arg := range args {
		collectPlaceholders(set, arg)
	}
	if len(set) == 0 {
		return []string{}
	}
	result := make([]string, 0, len(set))
	for key := range set {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}

func collectPlaceholders(set map[string]struct{}, value string) {
	if value == "" {
		return
	}
	matches := placeholderPattern.FindAllStringSubmatch(value, -1)
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		set[match[1]] = struct{}{}
	}
}
