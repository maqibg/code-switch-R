package services

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	piBuiltinCatalogTimeout = 15 * time.Second
	piBuiltinOutputLimit    = 32 << 20
	piBuiltinFailureTTL     = 60 * time.Second
)

var (
	errPiBuiltinOutputLimit = errors.New("Pi 内置模型目录输出超过 32 MiB 限制")
	piBuiltinProviderOrder  = []string{
		"anthropic", "openai-codex", "openai", "google", "google-vertex", "deepseek", "zai",
		"zai-coding-cn", "xai", "moonshotai", "moonshotai-cn", "kimi-coding", "xiaomi",
		"xiaomi-token-plan-cn", "minimax-cn", "minimax", "opencode", "opencode-go", "openrouter", "nvidia",
	}
)

type PiBuiltinModel struct {
	ID               string             `json:"id"`
	Name             string             `json:"name,omitempty"`
	API              string             `json:"api,omitempty"`
	Provider         string             `json:"provider"`
	BaseURL          string             `json:"baseUrl,omitempty"`
	Reasoning        *bool              `json:"reasoning,omitempty"`
	ThinkingLevelMap map[string]*string `json:"thinkingLevelMap,omitempty"`
	Input            []string           `json:"input,omitempty"`
	Cost             *PiModelCost       `json:"cost,omitempty"`
	ContextWindow    *int               `json:"contextWindow,omitempty"`
	MaxTokens        *int               `json:"maxTokens,omitempty"`
	Headers          map[string]string  `json:"headers,omitempty"`
	Compat           map[string]any     `json:"compat,omitempty"`
}

type PiBuiltinProvider struct {
	ID     string           `json:"id"`
	Models []PiBuiltinModel `json:"models"`
}

type PiBuiltinCatalogSnapshot struct {
	PiVersion     string              `json:"piVersion"`
	ModelVersion  string              `json:"modelVersion"`
	PiPackagePath string              `json:"piPackagePath"`
	ModelPackage  string              `json:"modelPackagePath"`
	LoadedAt      string              `json:"loadedAt"`
	ProviderCount int                 `json:"providerCount"`
	ModelCount    int                 `json:"modelCount"`
	Providers     []PiBuiltinProvider `json:"providers"`
}

type PiBuiltinModelAddRequest struct {
	SourceProviderID    string `json:"sourceProviderId"`
	ModelID             string `json:"modelId"`
	TargetProviderID    string `json:"targetProviderId"`
	ExpectedFingerprint string `json:"expectedFingerprint"`
	ConflictAction      string `json:"conflictAction,omitempty"`
}

type PiBuiltinModelAddResult struct {
	Status       string `json:"status"`
	ConflictKind string `json:"conflictKind,omitempty"`
}

type piBuiltinInstallation struct {
	PiVersion     string
	ModelVersion  string
	PiPackagePath string
	ModelPackage  string
	ModulePath    string
	NodePath      string
}

type piPackageManifest struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type boundedBuffer struct {
	buffer bytes.Buffer
	limit  int
}

func (b *boundedBuffer) Write(data []byte) (int, error) {
	if b.buffer.Len()+len(data) > b.limit {
		remaining := b.limit - b.buffer.Len()
		written := 0
		if remaining > 0 {
			written, _ = b.buffer.Write(data[:remaining])
		}
		return written, errPiBuiltinOutputLimit
	}
	return b.buffer.Write(data)
}

func (b *boundedBuffer) Bytes() []byte  { return b.buffer.Bytes() }
func (b *boundedBuffer) String() string { return b.buffer.String() }

func (s *PiSettingsService) BuiltinModelsCatalog(forceRefresh bool) (PiBuiltinCatalogSnapshot, error) {
	s.builtinCatalogMu.Lock()
	defer s.builtinCatalogMu.Unlock()
	if !forceRefresh && s.builtinCatalog != nil {
		return clonePiBuiltinCatalog(*s.builtinCatalog), nil
	}
	if !forceRefresh && s.builtinCatalogErr != nil && time.Now().Before(s.builtinRetryAt) {
		return PiBuiltinCatalogSnapshot{}, s.builtinCatalogErr
	}
	loader := s.builtinLoader
	if loader == nil {
		loader = loadInstalledPiBuiltinCatalog
	}
	snapshot, err := loader()
	if err != nil {
		s.builtinCatalogErr = err
		s.builtinRetryAt = time.Now().Add(piBuiltinFailureTTL)
		return PiBuiltinCatalogSnapshot{}, err
	}
	normalizePiBuiltinCatalog(&snapshot)
	if len(snapshot.Providers) == 0 || snapshot.ModelCount == 0 {
		err := fmt.Errorf("Pi 内置模型目录为空，请确认当前 Pi 安装完整")
		s.builtinCatalogErr = err
		s.builtinRetryAt = time.Now().Add(piBuiltinFailureTTL)
		return PiBuiltinCatalogSnapshot{}, err
	}
	cached := clonePiBuiltinCatalog(snapshot)
	s.builtinCatalog = &cached
	s.builtinCatalogErr = nil
	s.builtinRetryAt = time.Time{}
	return clonePiBuiltinCatalog(cached), nil
}

func (s *PiSettingsService) AddBuiltinModelToPlatform(input PiBuiltinModelAddRequest) (PiBuiltinModelAddResult, error) {
	sourceProviderID := strings.TrimSpace(input.SourceProviderID)
	modelID := strings.TrimSpace(input.ModelID)
	targetProviderID := strings.TrimSpace(input.TargetProviderID)
	conflictAction := strings.TrimSpace(input.ConflictAction)
	if sourceProviderID == "" || modelID == "" || targetProviderID == "" {
		return PiBuiltinModelAddResult{}, fmt.Errorf("内置模型来源平台、模型 ID 和目标平台不能为空")
	}
	if conflictAction != "" && conflictAction != "replace" {
		return PiBuiltinModelAddResult{}, fmt.Errorf("不支持的内置模型冲突动作: %s", conflictAction)
	}

	s.builtinCatalogMu.Lock()
	if s.builtinCatalog == nil {
		s.builtinCatalogMu.Unlock()
		return PiBuiltinModelAddResult{}, fmt.Errorf("Pi 内置模型目录尚未加载，请先打开内置模型页并刷新")
	}
	model, found := findPiBuiltinModel(*s.builtinCatalog, sourceProviderID, modelID)
	s.builtinCatalogMu.Unlock()
	if !found {
		return PiBuiltinModelAddResult{}, fmt.Errorf("Pi 内置模型不存在: %s/%s；请刷新内置模型目录", sourceProviderID, modelID)
	}

	s.modelsMu.Lock()
	defer s.modelsMu.Unlock()
	root, providers, fingerprint, err := readPiModelsProviderDocument(s.modelsPath())
	if err != nil {
		return PiBuiltinModelAddResult{}, err
	}
	if err := requirePiModelsFingerprint(input.ExpectedFingerprint, fingerprint); err != nil {
		return PiBuiltinModelAddResult{}, err
	}
	raw, exists := providers[targetProviderID]
	if !exists {
		return PiBuiltinModelAddResult{}, fmt.Errorf("Pi models.json Provider 不存在: %s", targetProviderID)
	}
	var target piModelsProviderFile
	if err := json.Unmarshal(raw, &target); err != nil {
		return PiBuiltinModelAddResult{}, fmt.Errorf("解析 Pi models.json Provider %q 失败: %w", targetProviderID, err)
	}
	existingIndex := -1
	for index, existing := range target.Models {
		if strings.TrimSpace(existing.ID) == modelID {
			existingIndex = index
			break
		}
	}
	_, overrideExists := target.ModelOverrides[modelID]
	if existingIndex >= 0 || overrideExists {
		conflictKind := "model"
		if existingIndex >= 0 && overrideExists {
			conflictKind = "model_and_override"
		} else if overrideExists {
			conflictKind = "model_override"
		}
		if conflictAction != "replace" {
			return PiBuiltinModelAddResult{Status: "conflict", ConflictKind: conflictKind}, nil
		}
	}

	entry := model.toPlatformModel(strings.TrimSpace(target.API) == "")
	status := "added"
	if existingIndex >= 0 {
		target.Models[existingIndex] = entry
		status = "replaced"
	} else {
		target.Models = append(target.Models, entry)
	}
	if overrideExists {
		delete(target.ModelOverrides, modelID)
		status = "replaced"
	}
	template := providerTemplateFromFile(targetProviderID, fingerprint, target)
	template, err = normalizePiModelsProviderTemplate(template)
	if err != nil {
		return PiBuiltinModelAddResult{}, fmt.Errorf("内置模型 %q 无法添加到目标平台: %w", modelID, err)
	}
	if err := writePiModelsProviderDocument(s.modelsPath(), root, providers, template); err != nil {
		return PiBuiltinModelAddResult{}, err
	}
	return PiBuiltinModelAddResult{Status: status}, nil
}

func (model PiBuiltinModel) toPlatformModel(keepSourceAPI bool) PiModelEntry {
	entry := PiModelEntry{
		ID: model.ID, Name: model.Name, Reasoning: model.Reasoning,
		ThinkingLevelMap: model.ThinkingLevelMap, Input: model.Input, Cost: model.Cost,
		ContextWindow: model.ContextWindow, MaxTokens: model.MaxTokens, Compat: model.Compat,
	}
	if keepSourceAPI {
		entry.API = model.API
	}
	return clonePiModelEntry(entry)
}

func findPiBuiltinModel(catalog PiBuiltinCatalogSnapshot, providerID, modelID string) (PiBuiltinModel, bool) {
	for _, provider := range catalog.Providers {
		if provider.ID != providerID {
			continue
		}
		for _, model := range provider.Models {
			if model.ID == modelID {
				data, _ := json.Marshal(model)
				var cloned PiBuiltinModel
				_ = json.Unmarshal(data, &cloned)
				return cloned, true
			}
		}
	}
	return PiBuiltinModel{}, false
}

func loadInstalledPiBuiltinCatalog() (PiBuiltinCatalogSnapshot, error) {
	installation, err := locatePiBuiltinInstallation("", "")
	if err != nil {
		return PiBuiltinCatalogSnapshot{}, err
	}
	const script = `import { pathToFileURL } from "node:url";
const modulePath = process.argv[1];
const source = await import(pathToFileURL(modulePath).href);
if (typeof source.getBuiltinProviders !== "function" || typeof source.getBuiltinModels !== "function") {
  throw new Error("providers/all.js does not export the built-in catalog API");
}
const providers = source.getBuiltinProviders().map((id) => ({ id, models: source.getBuiltinModels(id) }));
process.stdout.write(JSON.stringify({ providers }));`
	cmd := hideWindowCmd(installation.NodePath, "--input-type=module", "--eval", script, installation.ModulePath)
	stdout := &boundedBuffer{limit: piBuiltinOutputLimit}
	stderr := &boundedBuffer{limit: 64 << 10}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return PiBuiltinCatalogSnapshot{}, fmt.Errorf("启动 Node 解析 Pi 内置模型失败: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			if errors.Is(err, errPiBuiltinOutputLimit) {
				return PiBuiltinCatalogSnapshot{}, errPiBuiltinOutputLimit
			}
			detail := strings.TrimSpace(stderr.String())
			if detail != "" {
				return PiBuiltinCatalogSnapshot{}, fmt.Errorf("解析 Pi 内置模型失败: %s", detail)
			}
			return PiBuiltinCatalogSnapshot{}, fmt.Errorf("解析 Pi 内置模型失败: %w", err)
		}
	case <-time.After(piBuiltinCatalogTimeout):
		_ = cmd.Process.Kill()
		<-done
		return PiBuiltinCatalogSnapshot{}, fmt.Errorf("解析 Pi 内置模型超时（%s）", piBuiltinCatalogTimeout)
	}
	var payload struct {
		Providers []PiBuiltinProvider `json:"providers"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		return PiBuiltinCatalogSnapshot{}, fmt.Errorf("读取 Pi 内置模型输出失败: %w", err)
	}
	return PiBuiltinCatalogSnapshot{
		PiVersion: installation.PiVersion, ModelVersion: installation.ModelVersion,
		PiPackagePath: installation.PiPackagePath, ModelPackage: installation.ModelPackage,
		LoadedAt: time.Now().UTC().Format(time.RFC3339Nano), Providers: payload.Providers,
	}, nil
}

func locatePiBuiltinInstallation(piCommandOverride, nodeOverride string) (piBuiltinInstallation, error) {
	commandPaths := make([]string, 0, 3)
	if strings.TrimSpace(piCommandOverride) != "" {
		commandPaths = append(commandPaths, piCommandOverride)
	} else {
		for _, name := range []string{"pi.cmd", "pi", "pi.ps1"} {
			if path, err := exec.LookPath(name); err == nil {
				commandPaths = append(commandPaths, path)
			}
		}
	}
	commandPaths = uniquePaths(commandPaths)
	if len(commandPaths) == 0 {
		return piBuiltinInstallation{}, fmt.Errorf("未在 PATH 中找到 Pi；请先安装可用的 pi CLI")
	}
	var checked []string
	for _, commandPath := range commandPaths {
		packagePaths := piCodingAgentCandidates(commandPath)
		for _, packagePath := range packagePaths {
			checked = append(checked, packagePath)
			manifest, err := readPiPackageManifest(packagePath)
			if err != nil || !isPiCodingAgentPackage(manifest.Name) {
				continue
			}
			modelPath, modelManifest, err := locatePiAIModelPackage(packagePath, manifest)
			if err != nil {
				continue
			}
			modulePath := filepath.Join(modelPath, "dist", "providers", "all.js")
			if info, err := os.Stat(modulePath); err != nil || info.IsDir() {
				continue
			}
			nodePath, err := locateNodeExecutable(commandPath, nodeOverride)
			if err != nil {
				return piBuiltinInstallation{}, err
			}
			return piBuiltinInstallation{
				PiVersion: manifest.Version, ModelVersion: modelManifest.Version,
				PiPackagePath: packagePath, ModelPackage: modelPath, ModulePath: modulePath, NodePath: nodePath,
			}, nil
		}
	}
	return piBuiltinInstallation{}, fmt.Errorf("已找到 Pi 命令，但未找到带 providers/all.js 的 @earendil-works 或 @mariozechner Pi npm 安装；独立二进制暂不支持。已检查: %s", strings.Join(uniquePaths(checked), ", "))
}

func piCodingAgentCandidates(commandPath string) []string {
	abs, err := filepath.Abs(commandPath)
	if err == nil {
		commandPath = abs
	}
	dir := filepath.Dir(commandPath)
	result := []string{
		filepath.Join(dir, "node_modules", "@earendil-works", "pi-coding-agent"),
		filepath.Join(dir, "node_modules", "@mariozechner", "pi-coding-agent"),
	}
	if resolved, err := filepath.EvalSymlinks(commandPath); err == nil {
		current := filepath.Dir(resolved)
		for depth := 0; depth < 5; depth++ {
			result = append(result, current)
			parent := filepath.Dir(current)
			if parent == current {
				break
			}
			current = parent
		}
	}
	return uniquePaths(result)
}

func locatePiAIModelPackage(codingAgentPath string, manifest piPackageManifest) (string, piPackageManifest, error) {
	scope := strings.TrimSuffix(manifest.Name, "/pi-coding-agent")
	packageNames := []string{scope + "/pi-ai", "@earendil-works/pi-ai", "@mariozechner/pi-ai"}
	topNodeModules := filepath.Dir(filepath.Dir(codingAgentPath))
	for _, packageName := range packageNames {
		parts := strings.Split(packageName, "/")
		if len(parts) != 2 {
			continue
		}
		candidates := []string{
			filepath.Join(codingAgentPath, "node_modules", parts[0], parts[1]),
			filepath.Join(topNodeModules, parts[0], parts[1]),
		}
		for _, candidate := range candidates {
			modelManifest, err := readPiPackageManifest(candidate)
			if err == nil && (modelManifest.Name == "@earendil-works/pi-ai" || modelManifest.Name == "@mariozechner/pi-ai") && modelManifest.Version != "" {
				return candidate, modelManifest, nil
			}
		}
	}
	return "", piPackageManifest{}, fmt.Errorf("Pi coding-agent %s 缺少 pi-ai 依赖", manifest.Version)
}

func readPiPackageManifest(packagePath string) (piPackageManifest, error) {
	data, err := os.ReadFile(filepath.Join(packagePath, "package.json"))
	if err != nil {
		return piPackageManifest{}, err
	}
	var manifest piPackageManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return piPackageManifest{}, err
	}
	if strings.TrimSpace(manifest.Version) == "" {
		return piPackageManifest{}, fmt.Errorf("package.json 缺少 version")
	}
	return manifest, nil
}

func isPiCodingAgentPackage(name string) bool {
	return name == "@earendil-works/pi-coding-agent" || name == "@mariozechner/pi-coding-agent"
}

func locateNodeExecutable(piCommandPath, override string) (string, error) {
	if strings.TrimSpace(override) != "" {
		if info, err := os.Stat(override); err == nil && !info.IsDir() {
			return override, nil
		}
		return "", fmt.Errorf("指定的 Node 可执行文件不可用: %s", override)
	}
	for _, name := range []string{"node.exe", "node"} {
		candidate := filepath.Join(filepath.Dir(piCommandPath), name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	if path, err := exec.LookPath("node"); err == nil {
		return path, nil
	}
	return "", fmt.Errorf("已找到 Pi npm 包，但 PATH 中没有 Node 可执行文件")
}

func normalizePiBuiltinCatalog(snapshot *PiBuiltinCatalogSnapshot) {
	seenProviders := make(map[string]struct{}, len(snapshot.Providers))
	providers := make([]PiBuiltinProvider, 0, len(snapshot.Providers))
	modelCount := 0
	for _, provider := range snapshot.Providers {
		provider.ID = strings.TrimSpace(provider.ID)
		if provider.ID == "" {
			continue
		}
		if _, duplicate := seenProviders[provider.ID]; duplicate {
			continue
		}
		seenProviders[provider.ID] = struct{}{}
		seenModels := make(map[string]struct{}, len(provider.Models))
		models := make([]PiBuiltinModel, 0, len(provider.Models))
		for _, model := range provider.Models {
			model.ID = strings.TrimSpace(model.ID)
			if model.ID == "" {
				continue
			}
			if _, duplicate := seenModels[model.ID]; duplicate {
				continue
			}
			seenModels[model.ID] = struct{}{}
			if strings.TrimSpace(model.Provider) == "" {
				model.Provider = provider.ID
			}
			models = append(models, model)
		}
		sort.SliceStable(models, func(i, j int) bool { return strings.ToLower(models[i].ID) < strings.ToLower(models[j].ID) })
		provider.Models = models
		modelCount += len(models)
		providers = append(providers, provider)
	}
	sortPiBuiltinProviders(providers)
	snapshot.Providers = providers
	snapshot.ProviderCount = len(providers)
	snapshot.ModelCount = modelCount
}

func sortPiBuiltinProviders(providers []PiBuiltinProvider) {
	priority := make(map[string]int, len(piBuiltinProviderOrder))
	for index, id := range piBuiltinProviderOrder {
		priority[id] = index
	}
	sort.SliceStable(providers, func(i, j int) bool {
		left, leftPriority := priority[strings.ToLower(providers[i].ID)]
		right, rightPriority := priority[strings.ToLower(providers[j].ID)]
		if leftPriority != rightPriority {
			return leftPriority
		}
		if leftPriority && left != right {
			return left < right
		}
		return strings.ToLower(providers[i].ID) < strings.ToLower(providers[j].ID)
	})
}

func clonePiBuiltinCatalog(source PiBuiltinCatalogSnapshot) PiBuiltinCatalogSnapshot {
	data, _ := json.Marshal(source)
	var cloned PiBuiltinCatalogSnapshot
	_ = json.Unmarshal(data, &cloned)
	if cloned.Providers == nil {
		cloned.Providers = []PiBuiltinProvider{}
	}
	return cloned
}

func uniquePaths(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		path = filepath.Clean(strings.TrimSpace(path))
		if path == "." || path == "" {
			continue
		}
		key := strings.ToLower(path)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, path)
	}
	return result
}
