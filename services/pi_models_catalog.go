package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

type PiModelsCatalogModel struct {
	ID            string `json:"id"`
	Name          string `json:"name,omitempty"`
	API           string `json:"api,omitempty"`
	BaseURL       string `json:"baseUrl,omitempty"`
	Reasoning     *bool  `json:"reasoning,omitempty"`
	ContextWindow *int   `json:"contextWindow,omitempty"`
	MaxTokens     *int   `json:"maxTokens,omitempty"`
	Override      bool   `json:"override,omitempty"`
}

type PiModelsCatalogTemplate struct {
	ProviderID string                 `json:"providerId"`
	Name       string                 `json:"name,omitempty"`
	BaseURL    string                 `json:"baseUrl,omitempty"`
	API        string                 `json:"api,omitempty"`
	IsGateway  bool                   `json:"isGateway,omitempty"`
	Managed    bool                   `json:"managed"`
	Conflict   bool                   `json:"conflict"`
	Models     []PiModelsCatalogModel `json:"models"`
}

type PiModelsCatalogSnapshot struct {
	Path        string                    `json:"path"`
	ConfigDir   string                    `json:"configDir"`
	Detected    bool                      `json:"detected"`
	Exists      bool                      `json:"exists"`
	Initialized bool                      `json:"initialized"`
	ModifiedAt  string                    `json:"modifiedAt,omitempty"`
	Fingerprint string                    `json:"fingerprint,omitempty"`
	Error       string                    `json:"error,omitempty"`
	ErrorLine   int                       `json:"errorLine,omitempty"`
	ErrorColumn int                       `json:"errorColumn,omitempty"`
	Templates   []PiModelsCatalogTemplate `json:"templates"`
}

type piModelsCatalogProviderFile struct {
	Name           string                     `json:"name"`
	BaseURL        string                     `json:"baseUrl"`
	API            string                     `json:"api"`
	Models         []PiModelEntry             `json:"models"`
	ModelOverrides map[string]PiModelOverride `json:"modelOverrides"`
}

func (s *PiSettingsService) ModelsCatalog() (PiModelsCatalogSnapshot, error) {
	info, statErr := os.Stat(s.configDir)
	if errors.Is(statErr, os.ErrNotExist) {
		return PiModelsCatalogSnapshot{
			Path: s.modelsPath(), ConfigDir: s.configDir, Templates: []PiModelsCatalogTemplate{},
		}, nil
	}
	if statErr != nil {
		return piCatalogErrorSnapshot(s.configDir, s.modelsPath(), false, fmt.Errorf("检查 Pi 配置目录失败: %w", statErr)), nil
	}
	if !info.IsDir() {
		return piCatalogErrorSnapshot(s.configDir, s.modelsPath(), false, fmt.Errorf("Pi 配置路径不是目录: %s", s.configDir)), nil
	}
	s.modelsMu.Lock()
	snapshot, err := readPiModelsCatalog(s.modelsPath())
	s.modelsMu.Unlock()
	if err != nil {
		return piCatalogErrorSnapshot(s.configDir, s.modelsPath(), true, err), nil
	}
	snapshot.ConfigDir = s.configDir
	snapshot.Detected = true
	snapshot.Initialized = snapshot.Exists && len(snapshot.Templates) > 0
	state, stateErr := s.loadPlatformState()
	if stateErr != nil {
		snapshot.Error = stateErr.Error()
		return snapshot, nil
	}
	_, rawPlatforms, _, rawErr := readPiModelsProviderDocument(s.modelsPath())
	if rawErr != nil {
		snapshot.Error = rawErr.Error()
		return snapshot, nil
	}
	var authRoot map[string]json.RawMessage
	for _, entry := range state.Platforms {
		if entry.InjectedAuthHash == "" {
			continue
		}
		authRoot, _, rawErr = readJSONObjectOrDefault(s.authPath(), map[string]json.RawMessage{})
		if rawErr != nil {
			snapshot.Error = fmt.Sprintf("读取 Pi auth.json 失败: %v", rawErr)
			return snapshot, nil
		}
		break
	}
	for index := range snapshot.Templates {
		platform := &snapshot.Templates[index]
		entry, tracked := state.Platforms[platform.ProviderID]
		if !tracked {
			continue
		}
		current, exists := rawPlatforms[platform.ProviderID]
		platform.Managed = exists && piManagedProviderConnectionMatches(current, s.platformBaseURL(platform.ProviderID))
		if platform.Managed && entry.InjectedAuthHash != "" {
			platform.Managed = piManagedAuthMatches(authRoot, platform.ProviderID, entry)
		}
		platform.Conflict = !platform.Managed
	}
	return snapshot, nil
}

func piCatalogErrorSnapshot(configDir, path string, detected bool, err error) PiModelsCatalogSnapshot {
	snapshot := PiModelsCatalogSnapshot{
		Path: path, ConfigDir: configDir, Detected: detected, Exists: detected,
		Error: err.Error(), Templates: []PiModelsCatalogTemplate{},
	}
	var syntaxError *json.SyntaxError
	if errors.As(err, &syntaxError) {
		if data, readErr := os.ReadFile(path); readErr == nil {
			snapshot.ErrorLine, snapshot.ErrorColumn = jsonOffsetLineColumn(stripJSONComments(data), syntaxError.Offset)
		}
	}
	return snapshot
}

func jsonOffsetLineColumn(data []byte, offset int64) (int, int) {
	line, column := 1, 1
	limit := int(offset) - 1
	if limit > len(data) {
		limit = len(data)
	}
	for index := 0; index < limit; index++ {
		if data[index] == '\n' {
			line, column = line+1, 1
		} else {
			column++
		}
	}
	return line, column
}

func readPiModelsCatalog(path string) (PiModelsCatalogSnapshot, error) {
	snapshot := PiModelsCatalogSnapshot{Path: path, Templates: []PiModelsCatalogTemplate{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return snapshot, nil
	}
	if err != nil {
		return snapshot, fmt.Errorf("读取 Pi models.json 目录失败: %w", err)
	}
	cleaned := stripJSONComments(data)
	var root map[string]json.RawMessage
	if err := json.Unmarshal(cleaned, &root); err != nil {
		return snapshot, fmt.Errorf("解析 Pi models.json 目录失败: %w", err)
	}
	providerObjects, err := nestedJSONObject(root, "providers")
	if errors.Is(err, os.ErrNotExist) {
		providerObjects = map[string]json.RawMessage{}
	} else if err != nil {
		return snapshot, err
	}
	providerIDs := make([]string, 0, len(providerObjects))
	for id := range providerObjects {
		providerIDs = append(providerIDs, id)
	}
	sort.Strings(providerIDs)
	for _, id := range providerIDs {
		var source piModelsCatalogProviderFile
		if err := json.Unmarshal(providerObjects[id], &source); err != nil {
			return snapshot, fmt.Errorf("解析 Pi Provider %q 失败: %w", id, err)
		}
		providerID := strings.TrimSpace(id)
		providerAPI := strings.TrimSpace(source.API)
		modelsByID := make(map[string]PiModelsCatalogModel, len(source.Models))
		for _, model := range source.Models {
			modelID := strings.TrimSpace(model.ID)
			api := strings.TrimSpace(model.API)
			if api == "" {
				api = providerAPI
			}
			modelsByID[modelID] = PiModelsCatalogModel{
				ID: modelID, Name: strings.TrimSpace(model.Name), API: api,
				BaseURL:   strings.TrimSpace(model.BaseURL),
				Reasoning: model.Reasoning, ContextWindow: model.ContextWindow, MaxTokens: model.MaxTokens,
			}
		}
		modelIDs := make([]string, 0, len(modelsByID))
		for modelID := range modelsByID {
			modelIDs = append(modelIDs, modelID)
		}
		sort.Strings(modelIDs)

		models := make([]PiModelsCatalogModel, 0, len(modelsByID)+len(source.ModelOverrides))
		for _, modelID := range modelIDs {
			models = append(models, modelsByID[modelID])
		}
		for modelID, override := range source.ModelOverrides {
			modelID = strings.TrimSpace(modelID)
			if _, replaced := modelsByID[modelID]; replaced {
				continue
			}
			models = append(models, PiModelsCatalogModel{
				ID: modelID, Name: strings.TrimSpace(override.Name), API: providerAPI,
				Reasoning: override.Reasoning, ContextWindow: override.ContextWindow, MaxTokens: override.MaxTokens, Override: true,
			})
		}
		sort.SliceStable(models, func(i, j int) bool { return models[i].ID < models[j].ID })
		snapshot.Templates = append(snapshot.Templates, PiModelsCatalogTemplate{
			ProviderID: providerID,
			Name:       strings.TrimSpace(source.Name),
			BaseURL:    strings.TrimSpace(source.BaseURL),
			API:        providerAPI,
			IsGateway:  id == piGatewayProviderKey,
			Models:     models,
		})
	}
	snapshot.Exists = true
	if info, statErr := os.Stat(path); statErr == nil {
		snapshot.ModifiedAt = info.ModTime().UTC().Format(time.RFC3339Nano)
	}
	snapshot.Fingerprint = fingerprintPiModelsJSON(cleaned)
	return snapshot, nil
}
