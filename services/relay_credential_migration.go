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

// RefreshManagedRelayCredentials upgrades only configurations already owned by
// code-switch-R. Each platform is isolated so one stale external CLI file does
// not prevent the application and other platform migrations from starting.
func RefreshManagedRelayCredentials(
	claude *ClaudeSettingsService,
	codex *CodexSettingsService,
	gemini *GeminiService,
	reasonix *ReasonixSettingsService,
	pi *PiSettingsService,
	grok *GrokBuildService,
) error {
	steps := []struct {
		name string
		run  func() error
	}{
		{"Claude Code", func() error { return claudeProxyPlatform.refreshManagedCredential(claude.relayAddr) }},
		{"Codex", codex.refreshManagedCredential},
		{"Gemini", gemini.refreshManagedCredential},
		{"Reasonix", func() error { return reasonixProxyPlatform.refreshManagedCredential(reasonix.relayAddr) }},
		{"Pi", pi.refreshManagedCredentials},
		{"Grok", grok.refreshManagedRelayCredential},
	}
	var errs []error
	for _, step := range steps {
		if err := step.run(); err != nil {
			errs = append(errs, fmt.Errorf("升级 %s Relay 凭据失败: %w", step.name, err))
		}
	}
	return errors.Join(errs...)
}

func (p jsonProxyPlatform) refreshManagedCredential(relayAddr string) error {
	state, err := LoadProxyState(p.platform)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	configPath, _, err := p.paths()
	if err != nil {
		return err
	}
	if filepath.Clean(state.TargetPath) != filepath.Clean(configPath) {
		return fmt.Errorf("托管目标路径已从 %s 变为 %s", state.TargetPath, configPath)
	}
	payload, exists, err := p.readJSONConfig(configPath)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("托管配置不存在: %s", configPath)
	}
	container := p.access.container(payload, false)
	if container == nil || !urlMatchesProxy(anyToString(container[p.access.baseURLKey]), state.InjectedBaseURL) {
		return fmt.Errorf("代理地址已被外部修改")
	}
	if anyToString(container[p.access.authTokenKey]) != state.InjectedAuthToken {
		return fmt.Errorf("代理凭据已被外部修改")
	}
	nextURL := p.baseURL(relayAddr)
	nextToken := relayTokenForConfig()
	if urlMatchesProxy(state.InjectedBaseURL, nextURL) && state.InjectedAuthToken == nextToken {
		return nil
	}
	original, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	container[p.access.baseURLKey] = nextURL
	container[p.access.authTokenKey] = nextToken
	if p.access.afterWrite != nil {
		p.access.afterWrite(payload, container, nil)
	}
	if err := AtomicWriteJSON(configPath, payload); err != nil {
		return err
	}
	state.InjectedBaseURL = nextURL
	state.InjectedAuthToken = nextToken
	if err := SaveProxyState(p.platform, state); err != nil {
		if rollbackErr := AtomicWriteBytes(configPath, original); rollbackErr != nil {
			return fmt.Errorf("保存托管状态失败: %w; 配置回滚失败: %v", err, rollbackErr)
		}
		return fmt.Errorf("保存托管状态失败，配置已回滚: %w", err)
	}
	return nil
}

func (css *CodexSettingsService) refreshManagedCredential() error {
	state, err := LoadProxyState("codex")
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	settingsPath, _, err := css.paths()
	if err != nil {
		return err
	}
	if filepath.Clean(state.TargetPath) != filepath.Clean(settingsPath) {
		return fmt.Errorf("托管目标路径已从 %s 变为 %s", state.TargetPath, settingsPath)
	}
	configBefore, configExisted, err := readOptionalFile(settingsPath)
	if err != nil || !configExisted {
		return fmt.Errorf("读取 Codex config.toml 失败: %w", err)
	}
	var raw map[string]any
	if err := toml.Unmarshal(configBefore, &raw); err != nil {
		return fmt.Errorf("Codex config.toml 解析失败: %w", err)
	}
	if err := css.validateManagedConfigFields(raw, state); err != nil {
		return err
	}
	authPath := state.AuthFilePath
	if strings.TrimSpace(authPath) == "" {
		authPath, _, err = css.authPaths()
		if err != nil {
			return err
		}
	}
	authBefore, authExisted, err := readOptionalFile(authPath)
	if err != nil {
		return fmt.Errorf("读取 Codex auth.json 失败: %w", err)
	}
	auth := make(map[string]any)
	if authExisted {
		if err := json.Unmarshal(authBefore, &auth); err != nil {
			return fmt.Errorf("Codex auth.json 解析失败: %w", err)
		}
		if auth == nil {
			return fmt.Errorf("Codex auth.json 顶层必须是 JSON 对象")
		}
		if anyToString(auth[codexEnvKey]) != state.InjectedAuthToken {
			return fmt.Errorf("Codex OPENAI_API_KEY 已被外部修改，拒绝覆盖")
		}
	}
	nextURL := css.baseURL()
	nextToken := relayTokenForConfig()
	if authExisted && urlMatchesProxy(state.InjectedBaseURL, nextURL) && state.InjectedAuthToken == nextToken {
		return nil
	}
	provider := ensureProviderTable(ensureTomlTable(raw, "model_providers"), codexProviderKey)
	provider["name"] = codexProviderKey
	provider["base_url"] = nextURL
	provider["wire_api"] = codexWireAPI
	provider["requires_openai_auth"] = false
	auth[codexEnvKey] = nextToken
	if err := css.writeConfigToml(settingsPath, raw); err != nil {
		return err
	}
	if err := AtomicWriteJSON(authPath, auth); err != nil {
		_ = restoreOptionalFile(settingsPath, configBefore, configExisted)
		return err
	}
	state.InjectedBaseURL = nextURL
	state.InjectedAuthToken = nextToken
	if err := SaveProxyState("codex", state); err != nil {
		configRollback := restoreOptionalFile(settingsPath, configBefore, configExisted)
		authRollback := restoreOptionalFile(authPath, authBefore, authExisted)
		return fmt.Errorf("保存 Codex 托管状态失败: %w; 配置回滚: %v; 认证回滚: %v", err, configRollback, authRollback)
	}
	return nil
}

func (s *GeminiService) refreshManagedCredential() error {
	state, err := LoadProxyState("gemini")
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	envPath := getGeminiEnvPath()
	if filepath.Clean(state.TargetPath) != filepath.Clean(envPath) {
		return fmt.Errorf("托管目标路径已从 %s 变为 %s", state.TargetPath, envPath)
	}
	before, existed, err := readOptionalFile(envPath)
	if err != nil || !existed {
		return fmt.Errorf("读取 Gemini .env 失败: %w", err)
	}
	env, err := readGeminiEnv()
	if err != nil {
		return err
	}
	if !urlMatchesProxy(env[geminiBaseURLKey], state.InjectedBaseURL) || env[geminiAPIKeyKey] != state.InjectedAuthToken {
		return fmt.Errorf("Gemini 托管字段已被外部修改")
	}
	nextURL := buildProxyURL(s.relayAddr)
	nextToken := relayTokenForConfig()
	if urlMatchesProxy(state.InjectedBaseURL, nextURL) && state.InjectedAuthToken == nextToken {
		return nil
	}
	env[geminiBaseURLKey] = nextURL
	env[geminiAPIKeyKey] = nextToken
	if err := writeGeminiEnv(env); err != nil {
		return err
	}
	state.InjectedBaseURL = nextURL
	state.InjectedAuthToken = nextToken
	if err := SaveProxyState("gemini", state); err != nil {
		if rollbackErr := restoreOptionalFile(envPath, before, existed); rollbackErr != nil {
			return fmt.Errorf("保存 Gemini 托管状态失败: %w; 配置回滚失败: %v", err, rollbackErr)
		}
		return fmt.Errorf("保存 Gemini 托管状态失败，配置已回滚: %w", err)
	}
	return nil
}

func (s *PiSettingsService) refreshManagedCredentials() error {
	s.modelsMu.Lock()
	defer s.modelsMu.Unlock()
	if err := s.refreshLegacyGatewayCredential(); err != nil {
		return err
	}
	return s.refreshPlatformCredentials()
}

func (s *PiSettingsService) refreshLegacyGatewayCredential() error {
	var state PiProxyState
	if err := ReadJSONFile(s.statePath, &state); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	modelsRoot, modelsExisted, err := readJSONObjectOrDefault(s.modelsPath(), map[string]json.RawMessage{})
	if err != nil {
		return err
	}
	authRoot, authExisted, err := readJSONObjectOrDefault(s.authPath(), map[string]json.RawMessage{})
	if err != nil {
		return err
	}
	providers, err := ensureNestedJSONObject(modelsRoot, "providers")
	if err != nil {
		return err
	}
	currentProvider, providerExists := providers[piGatewayProviderKey]
	currentAuth, authExists := authRoot[piGatewayProviderKey]
	if !providerExists || canonicalJSONHash(currentProvider) != state.InjectedProviderHash ||
		!authExists || canonicalJSONHash(currentAuth) != state.InjectedAuthHash {
		return fmt.Errorf("Pi gateway 托管字段已被外部修改")
	}
	gatewayRaw, err := s.buildGatewayRaw()
	if err != nil {
		return err
	}
	authRaw, _ := json.Marshal(PiAuthEntry{Type: "api_key", Key: relayTokenForConfig()})
	if canonicalJSONHash(gatewayRaw) == state.InjectedProviderHash && canonicalJSONHash(authRaw) == state.InjectedAuthHash {
		return nil
	}
	modelsBefore, _, _ := readOptionalFile(s.modelsPath())
	authBefore, _, _ := readOptionalFile(s.authPath())
	providers[piGatewayProviderKey] = gatewayRaw
	modelsRoot["providers"], _ = json.Marshal(providers)
	authRoot[piGatewayProviderKey] = authRaw
	if err := writePiConfigPair(s.modelsPath(), modelsRoot, modelsExisted, s.authPath(), authRoot, authExisted, true); err != nil {
		return err
	}
	state.InjectedProviderHash = canonicalJSONHash(gatewayRaw)
	state.InjectedAuthHash = canonicalJSONHash(authRaw)
	if err := AtomicWriteJSON(s.statePath, state); err != nil {
		modelsRollback := restoreOptionalFile(s.modelsPath(), modelsBefore, modelsExisted)
		authRollback := restoreOptionalFile(s.authPath(), authBefore, authExisted)
		return fmt.Errorf("保存 Pi gateway 状态失败: %w; models 回滚: %v; auth 回滚: %v", err, modelsRollback, authRollback)
	}
	return nil
}

func (s *PiSettingsService) refreshPlatformCredentials() error {
	state, err := s.loadPlatformState()
	if err != nil || len(state.Platforms) == 0 {
		return err
	}
	root, providers, _, err := readPiModelsProviderDocument(s.modelsPath())
	if err != nil {
		return err
	}
	authRoot, authExisted, err := readJSONObjectOrDefault(s.authPath(), map[string]json.RawMessage{})
	if err != nil {
		return err
	}
	changed := false
	for providerID, entry := range state.Platforms {
		current, exists := providers[providerID]
		if !exists || canonicalJSONHash(current) != entry.InjectedProviderHash {
			return fmt.Errorf("Pi 平台 %q 托管字段已被外部修改", providerID)
		}
		currentAuth, authExists := authRoot[providerID]
		if entry.InjectedAuthHash != "" && (!authExists || canonicalJSONHash(currentAuth) != entry.InjectedAuthHash) {
			return fmt.Errorf("Pi 平台 %q 认证已被外部修改", providerID)
		}
		managedRaw, err := buildManagedPiPlatformRaw(current, s.platformBaseURL(providerID))
		if err != nil {
			return err
		}
		managedAuthRaw, _ := json.Marshal(PiAuthEntry{Type: "api_key", Key: relayTokenForConfig()})
		if canonicalJSONHash(managedRaw) == entry.InjectedProviderHash &&
			(entry.InjectedAuthHash == "" || canonicalJSONHash(managedAuthRaw) == entry.InjectedAuthHash) {
			continue
		}
		providers[providerID] = managedRaw
		authRoot[providerID] = managedAuthRaw
		entry.InjectedProviderHash = canonicalJSONHash(managedRaw)
		entry.InjectedAuthHash = canonicalJSONHash(managedAuthRaw)
		state.Platforms[providerID] = entry
		changed = true
	}
	if !changed {
		return nil
	}
	modelsBefore, modelsExisted, err := readOptionalFile(s.modelsPath())
	if err != nil {
		return err
	}
	authBefore, _, err := readOptionalFile(s.authPath())
	if err != nil {
		return err
	}
	root["providers"], _ = json.Marshal(providers)
	if err := writePiConfigPair(s.modelsPath(), root, modelsExisted, s.authPath(), authRoot, authExisted, true); err != nil {
		return err
	}
	if err := s.savePlatformState(state); err != nil {
		modelsRollback := restoreOptionalFile(s.modelsPath(), modelsBefore, modelsExisted)
		authRollback := restoreOptionalFile(s.authPath(), authBefore, authExisted)
		return fmt.Errorf("保存 Pi 平台托管状态失败: %w; models 回滚: %v; auth 回滚: %v", err, modelsRollback, authRollback)
	}
	return nil
}

func (s *GrokBuildService) refreshManagedRelayCredential() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := loadGrokRuntimeState()
	if err != nil || state.Mode != GrokModeRelay {
		return err
	}
	_, err = applyGrokConfigMode(state, GrokModeRelay, s.relayBaseURL())
	return err
}
