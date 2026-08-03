package relay

import (
	"codeswitch/services"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const defaultGeminiCredentialCooldown = 30 * time.Second

func geminiCredentialCooldownKey(provider services.Provider, model string) string {
	return fmt.Sprintf("%d:%s:%s", provider.ID, provider.Name, services.NormalizeGeminiModelID(model))
}

func (prs *ProviderRelayService) geminiCredentialCooling(provider services.Provider, model string) (time.Time, bool) {
	key := geminiCredentialCooldownKey(provider, model)
	prs.geminiCooldownMu.Lock()
	defer prs.geminiCooldownMu.Unlock()
	until, ok := prs.geminiCooldownUntil[key]
	if !ok {
		return time.Time{}, false
	}
	if !time.Now().Before(until) {
		delete(prs.geminiCooldownUntil, key)
		return time.Time{}, false
	}
	return until, true
}

func (prs *ProviderRelayService) markGeminiCredentialCooldown(provider services.Provider, model string, status int, retryAfter string) {
	if status != http.StatusTooManyRequests {
		return
	}
	duration := defaultGeminiCredentialCooldown
	if seconds, err := strconv.Atoi(strings.TrimSpace(retryAfter)); err == nil && seconds > 0 && seconds <= 3600 {
		duration = time.Duration(seconds) * time.Second
	}
	key := geminiCredentialCooldownKey(provider, model)
	prs.geminiCooldownMu.Lock()
	prs.geminiCooldownUntil[key] = time.Now().Add(duration)
	prs.geminiCooldownMu.Unlock()
}

func (prs *ProviderRelayService) clearGeminiCredentialCooldown(provider services.Provider, model string) {
	key := geminiCredentialCooldownKey(provider, model)
	prs.geminiCooldownMu.Lock()
	delete(prs.geminiCooldownUntil, key)
	prs.geminiCooldownMu.Unlock()
}
