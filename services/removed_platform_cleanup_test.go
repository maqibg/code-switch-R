package services

import "testing"

func TestRestoreRemovedDeepSeekCodeProxyFieldsRestoresOwnedValues(t *testing.T) {
	originalURL := "https://api.example.com"
	originalKey := "original-key"
	payload := map[string]any{
		"env": map[string]any{
			"DEEPSEEK_BASE_URL": "http://127.0.0.1:18100/deepseekcode/",
			"DEEPSEEK_API_KEY":  "code-switch-r",
			"OTHER":             "keep",
		},
	}
	state := &ProxyState{
		EnvExisted:        true,
		OriginalBaseURL:   &originalURL,
		OriginalAuthToken: &originalKey,
		InjectedBaseURL:   "http://127.0.0.1:18100/deepseekcode",
		InjectedAuthToken: "code-switch-r",
	}

	if !restoreRemovedDeepSeekCodeProxyFields(payload, state) {
		t.Fatal("expected removed platform fields to be restored")
	}
	env := payload["env"].(map[string]any)
	if env["DEEPSEEK_BASE_URL"] != originalURL || env["DEEPSEEK_API_KEY"] != originalKey {
		t.Fatalf("restored env = %#v", env)
	}
	if env["OTHER"] != "keep" {
		t.Fatalf("unrelated env field was changed: %#v", env)
	}
}

func TestRestoreRemovedDeepSeekCodeProxyFieldsPreservesExternalChanges(t *testing.T) {
	payload := map[string]any{
		"env": map[string]any{
			"DEEPSEEK_BASE_URL": "https://user.example.com",
			"DEEPSEEK_API_KEY":  "user-key",
		},
	}
	state := &ProxyState{
		InjectedBaseURL:   "http://127.0.0.1:18100/deepseekcode",
		InjectedAuthToken: "code-switch-r",
	}

	if restoreRemovedDeepSeekCodeProxyFields(payload, state) {
		t.Fatal("externally modified fields must not be restored")
	}
	env := payload["env"].(map[string]any)
	if env["DEEPSEEK_BASE_URL"] != "https://user.example.com" || env["DEEPSEEK_API_KEY"] != "user-key" {
		t.Fatalf("external changes were overwritten: %#v", env)
	}
}

func TestRestoreRemovedDeepSeekCodeProxyFieldsRestoresOnlyStillOwnedField(t *testing.T) {
	originalKey := "original-key"
	payload := map[string]any{
		"env": map[string]any{
			"DEEPSEEK_BASE_URL": "https://user.example.com",
			"DEEPSEEK_API_KEY":  "code-switch-r",
		},
	}
	state := &ProxyState{
		OriginalAuthToken: &originalKey,
		InjectedBaseURL:   "http://127.0.0.1:18100/deepseekcode",
		InjectedAuthToken: "code-switch-r",
	}

	if !restoreRemovedDeepSeekCodeProxyFields(payload, state) {
		t.Fatal("still-owned API key should be restored")
	}
	env := payload["env"].(map[string]any)
	if env["DEEPSEEK_BASE_URL"] != "https://user.example.com" || env["DEEPSEEK_API_KEY"] != originalKey {
		t.Fatalf("unexpected mixed restoration result: %#v", env)
	}
}
