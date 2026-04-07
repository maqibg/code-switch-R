package services

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const frontendPreferencesFileName = "frontend-preferences.json"

type FrontendPreferences struct {
	Theme                  string   `json:"theme"`
	Locale                 string   `json:"locale"`
	SidebarCollapsed       bool     `json:"sidebar_collapsed"`
	VisitedPages           []string `json:"visited_pages"`
	DismissedUpdateVersion string   `json:"dismissed_update_version"`
}

type FrontendPreferencesService struct{}

func NewFrontendPreferencesService() *FrontendPreferencesService {
	return &FrontendPreferencesService{}
}

func defaultFrontendPreferences() FrontendPreferences {
	return FrontendPreferences{
		Theme:            "dark",
		Locale:           "zh",
		SidebarCollapsed: false,
		VisitedPages:     []string{},
	}
}

func normalizeFrontendPreferences(prefs FrontendPreferences) FrontendPreferences {
	switch prefs.Theme {
	case "light", "dark", "systemdefault":
	default:
		prefs.Theme = "dark"
	}

	switch prefs.Locale {
	case "zh", "en":
	default:
		prefs.Locale = "zh"
	}

	if len(prefs.VisitedPages) == 0 {
		prefs.VisitedPages = []string{}
		return prefs
	}

	seen := make(map[string]struct{}, len(prefs.VisitedPages))
	filtered := make([]string, 0, len(prefs.VisitedPages))
	for _, page := range prefs.VisitedPages {
		page = strings.TrimSpace(page)
		if page == "" || !strings.HasPrefix(page, "/") {
			continue
		}
		if _, ok := seen[page]; ok {
			continue
		}
		seen[page] = struct{}{}
		filtered = append(filtered, page)
	}
	prefs.VisitedPages = filtered
	return prefs
}

func getFrontendPreferencesPath() (string, error) {
	configDir, err := ensureAppConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, frontendPreferencesFileName), nil
}

func loadFrontendPreferences(path string) (FrontendPreferences, error) {
	prefs := defaultFrontendPreferences()
	if !FileExists(path) {
		return prefs, nil
	}
	if err := ReadJSONFile(path, &prefs); err != nil {
		return prefs, err
	}
	return normalizeFrontendPreferences(prefs), nil
}

func saveFrontendPreferences(path string, prefs FrontendPreferences) (FrontendPreferences, error) {
	prefs = normalizeFrontendPreferences(prefs)
	if err := AtomicWriteJSON(path, prefs); err != nil {
		return prefs, err
	}
	return prefs, nil
}

func (s *FrontendPreferencesService) GetPreferences() (FrontendPreferences, error) {
	path, err := getFrontendPreferencesPath()
	if err != nil {
		return defaultFrontendPreferences(), err
	}
	return loadFrontendPreferences(path)
}

func (s *FrontendPreferencesService) SavePreferences(prefs FrontendPreferences) (FrontendPreferences, error) {
	path, err := getFrontendPreferencesPath()
	if err != nil {
		return defaultFrontendPreferences(), err
	}
	return saveFrontendPreferences(path, prefs)
}

func importLegacyFrontendPreferencesIfNeeded(sourceDir, targetDir string) (int, int64, error) {
	targetPath := filepath.Join(targetDir, frontendPreferencesFileName)
	if FileExists(targetPath) {
		return 0, 0, nil
	}

	prefs, ok, err := recoverLegacyFrontendPreferences(sourceDir)
	if err != nil || !ok {
		return 0, 0, err
	}

	if _, err := saveFrontendPreferences(targetPath, prefs); err != nil {
		return 0, 0, err
	}

	info, err := os.Stat(targetPath)
	if err != nil {
		return 0, 0, err
	}
	return 1, info.Size(), nil
}

func recoverLegacyFrontendPreferences(sourceDir string) (FrontendPreferences, bool, error) {
	home, err := getUserHomeDir()
	if err != nil {
		return FrontendPreferences{}, false, err
	}

	baseName := strings.ToLower(filepath.Base(sourceDir))
	candidates := make([]string, 0, 2)
	switch baseName {
	case ".code-switch-test":
		candidates = append(candidates,
			filepath.Join(home, "AppData", "Roaming", "code-switch-test.exe"),
			filepath.Join(home, "AppData", "Roaming", "code-switch-test-console.exe"),
		)
	case ".code-switch-r":
		candidates = append(candidates,
			filepath.Join(home, "AppData", "Roaming", "code-switch-R.exe"),
		)
	}

	for _, root := range candidates {
		prefs, ok, err := loadLegacyFrontendPreferencesFromWebView(root)
		if err != nil {
			return FrontendPreferences{}, false, err
		}
		if ok {
			return prefs, true, nil
		}
	}

	return FrontendPreferences{}, false, nil
}

func loadLegacyFrontendPreferencesFromWebView(root string) (FrontendPreferences, bool, error) {
	leveldbDir := filepath.Join(root, "EBWebView", "Default", "Local Storage", "leveldb")
	entries, err := os.ReadDir(leveldbDir)
	if err != nil {
		if os.IsNotExist(err) {
			return FrontendPreferences{}, false, nil
		}
		return FrontendPreferences{}, false, err
	}

	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.ToLower(entry.Name())
		if strings.HasSuffix(name, ".log") || strings.HasSuffix(name, ".ldb") {
			files = append(files, filepath.Join(leveldbDir, entry.Name()))
		}
	}
	slices.Sort(files)

	prefs := defaultFrontendPreferences()
	found := false
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		if theme, ok := extractLegacyLocalStorageValue(data, "theme"); ok {
			switch theme {
			case "light", "dark", "systemdefault":
				prefs.Theme = theme
				found = true
			}
		}

		if visited, ok := extractLegacyLocalStorageValue(data, "visited-pages"); ok {
			var pages []string
			if err := json.Unmarshal([]byte(visited), &pages); err == nil {
				prefs.VisitedPages = pages
				found = true
			}
		}
	}

	if !found {
		return FrontendPreferences{}, false, nil
	}
	return normalizeFrontendPreferences(prefs), true, nil
}

func extractLegacyLocalStorageValue(data []byte, key string) (string, bool) {
	marker := append([]byte{0x01}, []byte(key)...)
	start := 0
	latest := ""
	for {
		relative := bytes.Index(data[start:], marker)
		if relative < 0 {
			break
		}
		index := start + relative + len(marker)
		if index+2 > len(data) {
			break
		}

		length := int(data[index])
		index++
		if index >= len(data) || data[index] != 0x01 {
			start = index
			continue
		}
		index++
		if length <= 0 || index+length > len(data) {
			start = index
			continue
		}

		value := strings.TrimSpace(string(data[index : index+length]))
		if value != "" {
			latest = value
		}
		start = index
	}

	if latest == "" {
		return "", false
	}
	return latest, true
}
