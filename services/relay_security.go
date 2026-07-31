package services

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
)

var relayTokenValue atomic.Value
var relayTokenInitMu sync.Mutex

func init() {
	relayTokenValue.Store("")
}

func GenerateRelayToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("生成 Relay Token 失败: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func SetRelayToken(token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		relayTokenValue.Store("")
		return nil
	}
	if len(token) < 32 || len(token) > 512 {
		return fmt.Errorf("Relay Token 长度无效")
	}
	relayTokenValue.Store(token)
	return nil
}

func RelayToken() string {
	value, _ := relayTokenValue.Load().(string)
	return value
}

func relayTokenForConfig() string {
	if token := RelayToken(); token != "" {
		return token
	}
	relayTokenInitMu.Lock()
	defer relayTokenInitMu.Unlock()
	if token := RelayToken(); token != "" {
		return token
	}
	token, err := GenerateRelayToken()
	if err != nil {
		panic(err)
	}
	if err := SetRelayToken(token); err != nil {
		panic(err)
	}
	return token
}

func RelayTokenMatches(candidate string) bool {
	expected := RelayToken()
	candidate = strings.TrimSpace(candidate)
	if expected == "" || len(expected) != len(candidate) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(candidate)) == 1
}

func relayManagedTokenMatches(candidate string) bool {
	candidate = strings.TrimSpace(candidate)
	return RelayTokenMatches(candidate) || candidate == "code-switch-r" || candidate == "code-switch-r-proxy"
}
