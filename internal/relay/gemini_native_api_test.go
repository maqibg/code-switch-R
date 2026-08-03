package relay

import (
	"codeswitch/services"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGeminiNativeModelsDetailsAndCountTokens(t *testing.T) {
	env := newFailoverEnv(t)
	env.enableDegradeMode(t)
	upstream := newGeminiUpstream(t, upstreamScript{body: `{"models":[{"name":"models/gemini-2.5-flash","displayName":"Flash","supportedGenerationMethods":["generateContent","streamGenerateContent","countTokens"]}]}`})
	env.addGeminiProvider(t, "g-native", "Native", upstream.server.URL, 1)

	for _, request := range []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodGet, path: "/gemini/v1beta/models"},
		{method: http.MethodGet, path: "/gemini/v1beta/models/gemini-2.5-flash"},
		{method: http.MethodPost, path: "/gemini/v1beta/models/gemini-2.5-flash:countTokens", body: `{"contents":[]}`},
	} {
		recorder := serveRelayRequest(t, env, request.method, request.path, request.body)
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s %s 返回 %d: %s", request.method, request.path, recorder.Code, recorder.Body.String())
		}
	}
	if got := upstream.Hits(); got != 3 {
		t.Fatalf("Native 三类端点应各请求一次，实际 %d", got)
	}
}

func serveRelayRequest(t *testing.T, env *failoverEnv, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	env.router.ServeHTTP(recorder, request)
	return recorder
}

func TestGeminiNativeRejectsClientCredentialAndCLIAccount(t *testing.T) {
	env := newFailoverEnv(t)
	env.enableDegradeMode(t)
	upstream := newGeminiUpstream(t)
	if err := env.gemini.AddProvider(services.GeminiProvider{
		ID: "g-cli", Name: "CLI", BaseURL: upstream.server.URL, Enabled: true,
		CredentialType: string(services.GeminiCredentialCLIOAuth),
	}); err != nil {
		t.Fatal(err)
	}
	recorder := serveRelayRequest(t, env, http.MethodPost, "/gemini/v1beta/models/gemini-2.5-flash:generateContent", `{"contents":[]}`)
	if recorder.Code != http.StatusNotFound || !strings.Contains(recorder.Body.String(), "no active gemini provider") {
		t.Fatalf("CLI Account 不得进入 Native Relay: %d %s", recorder.Code, recorder.Body.String())
	}
}
