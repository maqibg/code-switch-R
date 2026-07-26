package services

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAppSettingsCacheRefreshesAfterExternalChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.json")
	service := &AppSettingsService{path: path}
	initial := service.defaultSettings()
	initial.BudgetTotal = 12
	writeAppSettingsTestFile(t, path, initial)

	loaded, err := service.GetAppSettings()
	if err != nil {
		t.Fatalf("GetAppSettings() error = %v", err)
	}
	if loaded.BudgetTotal != 12 {
		t.Fatalf("BudgetTotal = %v, want 12", loaded.BudgetTotal)
	}

	updated := initial
	updated.BudgetTotal = 12345
	writeAppSettingsTestFile(t, path, updated)
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatalf("Chtimes() error = %v", err)
	}

	loaded, err = service.GetAppSettings()
	if err != nil {
		t.Fatalf("GetAppSettings() after external change error = %v", err)
	}
	if loaded.BudgetTotal != 12345 {
		t.Fatalf("BudgetTotal after external change = %v, want 12345", loaded.BudgetTotal)
	}
}

func TestSaveAppSettingsUpdatesCache(t *testing.T) {
	service := &AppSettingsService{path: filepath.Join(t.TempDir(), "app.json")}
	settings := service.defaultSettings()
	settings.EnableRoundRobin = true

	if _, err := service.SaveAppSettings(settings); err != nil {
		t.Fatalf("SaveAppSettings() error = %v", err)
	}
	loaded, err := service.GetAppSettings()
	if err != nil {
		t.Fatalf("GetAppSettings() error = %v", err)
	}
	if !loaded.EnableRoundRobin {
		t.Fatal("EnableRoundRobin = false, want true")
	}
}

func writeAppSettingsTestFile(t *testing.T, path string, settings AppSettings) {
	t.Helper()
	data, err := json.Marshal(settings)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
}
