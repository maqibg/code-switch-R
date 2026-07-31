package services

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

const (
	GrokModeUnmanaged GrokRuntimeMode = "unmanaged"
	GrokModeRelay     GrokRuntimeMode = "grok_relay"
	GrokModeOAuth     GrokRuntimeMode = "grok_oauth"

	grokManagedModelKey = "code-switch-r"
	grokInboundModel    = "grok-build"
	grokRuntimeStateVer = 1
)

var grokTableHeaderPattern = regexp.MustCompile(`^\s*\[([^\]]+)\]\s*(?:#.*)?$`)

type GrokRuntimeMode string

type GrokConfigPaths struct {
	ConfigPath      string `json:"configPath"`
	AuthPath        string `json:"authPath"`
	CustomDirectory string `json:"customDirectory,omitempty"`
}

type GrokRuntimeStatus struct {
	Mode             GrokRuntimeMode `json:"mode"`
	AppliedAccountID string          `json:"appliedAccountId,omitempty"`
	ConfigPath       string          `json:"configPath"`
	AuthPath         string          `json:"authPath"`
	CustomDirectory  string          `json:"customDirectory,omitempty"`
	Managed          bool            `json:"managed"`
	Conflict         bool            `json:"conflict"`
	ConflictMessage  string          `json:"conflictMessage,omitempty"`
}

type grokRuntimeState struct {
	Version                      int             `json:"version"`
	CustomDirectory              string          `json:"customDirectory,omitempty"`
	Mode                         GrokRuntimeMode `json:"mode"`
	AppliedAccountID             string          `json:"appliedAccountId,omitempty"`
	TargetConfigPath             string          `json:"targetConfigPath,omitempty"`
	TargetAuthPath               string          `json:"targetAuthPath,omitempty"`
	OriginalModelsSectionExisted bool            `json:"originalModelsSectionExisted,omitempty"`
	OriginalDefaultExisted       bool            `json:"originalDefaultExisted,omitempty"`
	OriginalDefaultLine          string          `json:"originalDefaultLine,omitempty"`
	InjectedFingerprint          string          `json:"injectedFingerprint,omitempty"`
	LocalRelayKey                string          `json:"localRelayKey,omitempty"`
	CreatedAt                    string          `json:"createdAt,omitempty"`
}

type grokManagedFingerprintPayload struct {
	DefaultExists bool           `json:"defaultExists"`
	Default       any            `json:"default,omitempty"`
	ModelExists   bool           `json:"modelExists"`
	Model         map[string]any `json:"model,omitempty"`
}

func grokRuntimeStatePath() string {
	return filepath.Join(mustGetAppConfigDir(), "grok-runtime.json")
}

func loadGrokRuntimeState() (grokRuntimeState, error) {
	state := grokRuntimeState{Version: grokRuntimeStateVer, Mode: GrokModeUnmanaged}
	err := ReadJSONFile(grokRuntimeStatePath(), &state)
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return state, fmt.Errorf("读取 Grok 运行状态失败: %w", err)
	}
	if state.Version != grokRuntimeStateVer {
		return state, fmt.Errorf("不支持的 Grok 运行状态版本: %d", state.Version)
	}
	if state.Mode == "" {
		state.Mode = GrokModeUnmanaged
	}
	return state, nil
}

func saveGrokRuntimeState(state grokRuntimeState) error {
	state.Version = grokRuntimeStateVer
	return AtomicWriteJSON(grokRuntimeStatePath(), state)
}

func resolveGrokConfigPaths(state grokRuntimeState) (GrokConfigPaths, error) {
	if custom := strings.TrimSpace(state.CustomDirectory); custom != "" {
		absolute, err := filepath.Abs(custom)
		if err != nil {
			return GrokConfigPaths{}, fmt.Errorf("解析 Grok 自定义目录失败: %w", err)
		}
		return GrokConfigPaths{
			ConfigPath:      filepath.Join(absolute, "config.toml"),
			AuthPath:        filepath.Join(absolute, "auth.json"),
			CustomDirectory: absolute,
		}, nil
	}

	home, err := getUserHomeDir()
	if err != nil {
		return GrokConfigPaths{}, fmt.Errorf("读取用户目录失败: %w", err)
	}
	grokHome := strings.TrimSpace(os.Getenv("GROK_HOME"))
	if grokHome == "" {
		grokHome = filepath.Join(home, ".grok")
	}
	if !filepath.IsAbs(grokHome) {
		grokHome, err = filepath.Abs(grokHome)
		if err != nil {
			return GrokConfigPaths{}, fmt.Errorf("解析 GROK_HOME 失败: %w", err)
		}
	}
	configPath := strings.TrimSpace(os.Getenv("GROK_CONFIG"))
	if configPath == "" {
		configPath = filepath.Join(grokHome, "config.toml")
	} else if !filepath.IsAbs(configPath) {
		configPath, err = filepath.Abs(configPath)
		if err != nil {
			return GrokConfigPaths{}, fmt.Errorf("解析 GROK_CONFIG 失败: %w", err)
		}
	}
	return GrokConfigPaths{ConfigPath: configPath, AuthPath: filepath.Join(grokHome, "auth.json")}, nil
}

func readOptionalGrokFile(path string) ([]byte, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

func validateGrokTOML(data []byte) error {
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil
	}
	var root map[string]any
	if err := toml.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("Grok config.toml 解析失败: %w", err)
	}
	return nil
}

func grokManagedFingerprint(data []byte) (string, error) {
	var root map[string]any
	if len(strings.TrimSpace(string(data))) > 0 {
		if err := toml.Unmarshal(data, &root); err != nil {
			return "", fmt.Errorf("Grok config.toml 解析失败: %w", err)
		}
	}
	payload := grokManagedFingerprintPayload{}
	if models, ok := mapStringAny(root["models"]); ok {
		payload.Default, payload.DefaultExists = models["default"]
	}
	if modelRoot, ok := mapStringAny(root["model"]); ok {
		if managed, ok := mapStringAny(modelRoot[grokManagedModelKey]); ok {
			payload.ModelExists = true
			payload.Model = managed
		}
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:]), nil
}

func mapStringAny(value any) (map[string]any, bool) {
	result, ok := value.(map[string]any)
	return result, ok
}

func splitGrokTOMLLines(data []byte) ([]string, string, bool) {
	text := string(data)
	eol := "\n"
	if strings.Contains(text, "\r\n") {
		eol = "\r\n"
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	finalNewline := strings.HasSuffix(text, "\n")
	text = strings.TrimSuffix(text, "\n")
	if text == "" {
		return []string{}, eol, finalNewline
	}
	return strings.Split(text, "\n"), eol, finalNewline
}

func joinGrokTOMLLines(lines []string, eol string, finalNewline bool) []byte {
	result := strings.Join(lines, eol)
	if finalNewline {
		result += eol
	}
	return []byte(result)
}

func normalizedGrokHeader(line string) string {
	match := grokTableHeaderPattern.FindStringSubmatch(strings.TrimSpace(line))
	if len(match) != 2 {
		return ""
	}
	header := strings.TrimSpace(match[1])
	header = strings.ReplaceAll(header, `"`, "")
	header = strings.ReplaceAll(header, `'`, "")
	header = strings.ReplaceAll(header, " ", "")
	return strings.ToLower(header)
}

func grokSectionBounds(lines []string, section string) (int, int, bool) {
	section = strings.ToLower(strings.TrimSpace(section))
	for index, line := range lines {
		if normalizedGrokHeader(line) != section {
			continue
		}
		end := len(lines)
		for cursor := index + 1; cursor < len(lines); cursor++ {
			if normalizedGrokHeader(lines[cursor]) != "" {
				end = cursor
				break
			}
		}
		return index, end, true
	}
	return 0, 0, false
}

func grokAssignmentKey(line string) string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return ""
	}
	index := strings.IndexByte(trimmed, '=')
	if index < 0 {
		return ""
	}
	key := strings.TrimSpace(trimmed[:index])
	key = strings.Trim(key, `"'`)
	return strings.ToLower(key)
}

func findGrokDefaultLine(lines []string) (string, bool) {
	start, end, ok := grokSectionBounds(lines, "models")
	if !ok {
		return "", false
	}
	for index := start + 1; index < end; index++ {
		if grokAssignmentKey(lines[index]) == "default" {
			return lines[index], true
		}
	}
	return "", false
}

func removeGrokManagedModel(lines []string) []string {
	start, end, ok := grokSectionBounds(lines, "model."+grokManagedModelKey)
	if !ok {
		return lines
	}
	result := append([]string{}, lines[:start]...)
	result = append(result, lines[end:]...)
	return result
}

func removeGrokDefault(lines []string) []string {
	start, end, ok := grokSectionBounds(lines, "models")
	if !ok {
		return lines
	}
	result := make([]string, 0, len(lines))
	result = append(result, lines[:start+1]...)
	for index := start + 1; index < end; index++ {
		if grokAssignmentKey(lines[index]) != "default" {
			result = append(result, lines[index])
		}
	}
	result = append(result, lines[end:]...)
	return result
}

func setGrokDefault(lines []string, value string) []string {
	start, end, ok := grokSectionBounds(lines, "models")
	assignment := "default = " + strconv.Quote(value)
	if !ok {
		if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
			lines = append(lines, "")
		}
		return append(lines, "[models]", assignment)
	}
	for index := start + 1; index < end; index++ {
		if grokAssignmentKey(lines[index]) == "default" {
			lines[index] = assignment
			return lines
		}
	}
	result := append([]string{}, lines[:start+1]...)
	result = append(result, assignment)
	result = append(result, lines[start+1:]...)
	return result
}

func restoreGrokDefault(lines []string, state grokRuntimeState) []string {
	lines = removeGrokDefault(lines)
	if !state.OriginalDefaultExisted {
		if !state.OriginalModelsSectionExisted {
			start, end, ok := grokSectionBounds(lines, "models")
			if ok {
				onlyEmpty := true
				for index := start + 1; index < end; index++ {
					trimmed := strings.TrimSpace(lines[index])
					if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
						onlyEmpty = false
						break
					}
				}
				if onlyEmpty {
					lines = append(append([]string{}, lines[:start]...), lines[end:]...)
				}
			}
		}
		return lines
	}
	start, _, ok := grokSectionBounds(lines, "models")
	if !ok {
		if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
			lines = append(lines, "")
		}
		return append(lines, "[models]", state.OriginalDefaultLine)
	}
	result := append([]string{}, lines[:start+1]...)
	result = append(result, state.OriginalDefaultLine)
	result = append(result, lines[start+1:]...)
	return result
}

func appendGrokManagedModel(lines []string, relayBaseURL, localKey string) []string {
	if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
		lines = append(lines, "")
	}
	return append(lines,
		"[model."+grokManagedModelKey+"]",
		"model = "+strconv.Quote(grokInboundModel),
		"base_url = "+strconv.Quote(strings.TrimRight(relayBaseURL, "/")),
		"api_key = "+strconv.Quote(localKey),
		"api_backend = \"responses\"",
	)
}

func grokRelayConfigText(data []byte, relayBaseURL, localKey string) ([]byte, error) {
	if err := validateGrokTOML(data); err != nil {
		return nil, err
	}
	lines, eol, finalNewline := splitGrokTOMLLines(data)
	lines = removeGrokManagedModel(lines)
	lines = setGrokDefault(lines, grokManagedModelKey)
	lines = appendGrokManagedModel(lines, relayBaseURL, localKey)
	result := joinGrokTOMLLines(lines, eol, finalNewline)
	if err := validateGrokTOML(result); err != nil {
		return nil, err
	}
	return result, nil
}

func grokOAuthConfigText(data []byte) ([]byte, error) {
	if err := validateGrokTOML(data); err != nil {
		return nil, err
	}
	lines, eol, finalNewline := splitGrokTOMLLines(data)
	lines = removeGrokManagedModel(lines)
	lines = removeGrokDefault(lines)
	result := joinGrokTOMLLines(lines, eol, finalNewline)
	if err := validateGrokTOML(result); err != nil {
		return nil, err
	}
	return result, nil
}

func grokUnmanagedConfigText(data []byte, state grokRuntimeState) ([]byte, error) {
	if err := validateGrokTOML(data); err != nil {
		return nil, err
	}
	lines, eol, finalNewline := splitGrokTOMLLines(data)
	lines = removeGrokManagedModel(lines)
	lines = restoreGrokDefault(lines, state)
	result := joinGrokTOMLLines(lines, eol, finalNewline)
	if err := validateGrokTOML(result); err != nil {
		return nil, err
	}
	return result, nil
}

func captureGrokTakeoverBaseline(data []byte, state *grokRuntimeState) error {
	if err := validateGrokTOML(data); err != nil {
		return err
	}
	var root map[string]any
	if len(strings.TrimSpace(string(data))) > 0 {
		if err := toml.Unmarshal(data, &root); err != nil {
			return err
		}
	}
	if modelRoot, ok := mapStringAny(root["model"]); ok {
		if _, exists := modelRoot[grokManagedModelKey]; exists {
			return fmt.Errorf("Grok config.toml 已存在 [model.%s]，请先改名或显式放弃该配置", grokManagedModelKey)
		}
	}
	lines, _, _ := splitGrokTOMLLines(data)
	_, _, state.OriginalModelsSectionExisted = grokSectionBounds(lines, "models")
	state.OriginalDefaultLine, state.OriginalDefaultExisted = findGrokDefaultLine(lines)
	return nil
}

func grokConfigConflict(data []byte, state grokRuntimeState) error {
	if state.Mode == GrokModeUnmanaged || state.InjectedFingerprint == "" {
		return nil
	}
	if filepath.Clean(state.TargetConfigPath) == "." {
		return fmt.Errorf("Grok 接管状态缺少目标路径")
	}
	fingerprint, err := grokManagedFingerprint(data)
	if err != nil {
		return err
	}
	if fingerprint != state.InjectedFingerprint {
		return fmt.Errorf("Grok 受管字段已被外部修改，已停止切换；请重新接管或放弃接管")
	}
	return nil
}

func applyGrokConfigMode(state grokRuntimeState, target GrokRuntimeMode, relayBaseURL string) (grokRuntimeState, error) {
	paths, err := resolveGrokConfigPaths(state)
	if err != nil {
		return state, err
	}
	if state.Mode != GrokModeUnmanaged && state.TargetConfigPath != "" &&
		filepath.Clean(state.TargetConfigPath) != filepath.Clean(paths.ConfigPath) {
		return state, fmt.Errorf("Grok 配置路径已从 %s 变为 %s，请先处理旧路径接管状态", state.TargetConfigPath, paths.ConfigPath)
	}
	current, currentExisted, err := readOptionalGrokFile(paths.ConfigPath)
	if err != nil {
		return state, fmt.Errorf("读取 Grok config.toml 失败: %w", err)
	}
	if err := grokConfigConflict(current, state); err != nil {
		return state, err
	}

	nextState := state
	if state.Mode == GrokModeUnmanaged && target != GrokModeUnmanaged {
		if err := captureGrokTakeoverBaseline(current, &nextState); err != nil {
			return state, err
		}
		nextState.TargetConfigPath = paths.ConfigPath
		nextState.TargetAuthPath = paths.AuthPath
		nextState.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if target == GrokModeRelay {
		nextState.LocalRelayKey = relayTokenForConfig()
	}

	var next []byte
	switch target {
	case GrokModeRelay:
		if strings.TrimSpace(relayBaseURL) == "" {
			return state, fmt.Errorf("Grok Relay 地址不能为空")
		}
		next, err = grokRelayConfigText(current, relayBaseURL, nextState.LocalRelayKey)
	case GrokModeOAuth:
		next, err = grokOAuthConfigText(current)
	case GrokModeUnmanaged:
		next, err = grokUnmanagedConfigText(current, nextState)
	default:
		return state, fmt.Errorf("不支持的 Grok 运行模式: %s", target)
	}
	if err != nil {
		return state, err
	}
	if err := os.MkdirAll(filepath.Dir(paths.ConfigPath), 0o700); err != nil {
		return state, fmt.Errorf("创建 Grok 配置目录失败: %w", err)
	}
	if err := AtomicWriteBytes(paths.ConfigPath, next); err != nil {
		return state, fmt.Errorf("写入 Grok config.toml 失败: %w", err)
	}

	nextState.Mode = target
	if target != GrokModeOAuth {
		nextState.AppliedAccountID = ""
	}
	if target == GrokModeUnmanaged {
		nextState.TargetConfigPath = ""
		nextState.TargetAuthPath = ""
		nextState.OriginalModelsSectionExisted = false
		nextState.OriginalDefaultExisted = false
		nextState.OriginalDefaultLine = ""
		nextState.InjectedFingerprint = ""
		nextState.CreatedAt = ""
	} else {
		nextState.InjectedFingerprint, err = grokManagedFingerprint(next)
		if err != nil {
			_ = restoreOptionalGrokFile(paths.ConfigPath, current, currentExisted)
			return state, err
		}
	}
	if err := saveGrokRuntimeState(nextState); err != nil {
		_ = restoreOptionalGrokFile(paths.ConfigPath, current, currentExisted)
		return state, fmt.Errorf("保存 Grok 运行状态失败，配置已回滚: %w", err)
	}
	return nextState, nil
}
