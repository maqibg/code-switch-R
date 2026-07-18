package services

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestNormalizeFrontendPreferencesPlatformOrder(t *testing.T) {
	prefs := normalizeFrontendPreferences(FrontendPreferences{
		Theme:             "dark",
		Locale:            "zh",
		HomePlatformOrder: []string{" codex ", "unknown", "codex", "claude"},
		PiPlatformOrder:   []string{" anthropic ", "", "anthropic", "openai-codex"},
	})

	wantHome := []string{"codex", "claude", "gemini", "deepseekcode", "reasonix", "others"}
	if !slices.Equal(prefs.HomePlatformOrder, wantHome) {
		t.Fatalf("home platform order = %#v, want %#v", prefs.HomePlatformOrder, wantHome)
	}
	wantPi := []string{"anthropic", "openai-codex"}
	if !slices.Equal(prefs.PiPlatformOrder, wantPi) {
		t.Fatalf("Pi platform order = %#v, want %#v", prefs.PiPlatformOrder, wantPi)
	}
	if prefs.VisitedPages == nil {
		t.Fatal("visited pages must remain a non-nil empty slice")
	}
}

func TestDefaultFrontendPreferencesIncludesStablePlatformOrder(t *testing.T) {
	prefs := defaultFrontendPreferences()
	if !slices.Equal(prefs.HomePlatformOrder, defaultHomePlatformOrder) {
		t.Fatalf("home platform order = %#v, want %#v", prefs.HomePlatformOrder, defaultHomePlatformOrder)
	}
	if prefs.PiPlatformOrder == nil || len(prefs.PiPlatformOrder) != 0 {
		t.Fatalf("Pi platform order = %#v, want non-nil empty slice", prefs.PiPlatformOrder)
	}
}

func TestLoadFrontendPreferencesAddsPlatformOrderToLegacyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), frontendPreferencesFileName)
	legacy := []byte(`{"theme":"light","locale":"en","sidebar_collapsed":true,"visited_pages":["/"]}`)
	if err := os.WriteFile(path, legacy, 0o600); err != nil {
		t.Fatalf("write legacy preferences: %v", err)
	}

	prefs, err := loadFrontendPreferences(path)
	if err != nil {
		t.Fatalf("load legacy preferences: %v", err)
	}
	if !slices.Equal(prefs.HomePlatformOrder, defaultHomePlatformOrder) {
		t.Fatalf("home platform order = %#v, want %#v", prefs.HomePlatformOrder, defaultHomePlatformOrder)
	}
	if prefs.PiPlatformOrder == nil || len(prefs.PiPlatformOrder) != 0 {
		t.Fatalf("Pi platform order = %#v, want non-nil empty slice", prefs.PiPlatformOrder)
	}
}
