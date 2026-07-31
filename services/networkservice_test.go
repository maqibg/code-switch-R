package services

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type failingRelayRuntime struct {
	addr     string
	restarts []string
	err      error
}

func (runtime *failingRelayRuntime) Addr() string { return runtime.addr }

func (runtime *failingRelayRuntime) Restart(addr string) error {
	runtime.restarts = append(runtime.restarts, addr)
	return runtime.err
}

func TestNetworkSettingsNormalizeRemovedListenModes(t *testing.T) {
	path := filepath.Join(t.TempDir(), networkSettingsFile)
	payload := map[string]any{
		"listenMode":    "lan",
		"customAddress": "0.0.0.0:18100",
		"wslAutoConfig": true,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	service := &NetworkService{settingsPath: path}
	settings, err := service.GetNetworkSettings()
	if err != nil {
		t.Fatal(err)
	}
	if settings.ListenMode != ListenModeLocalhost || settings.CurrentAddress != "127.0.0.1:18100" {
		t.Fatalf("旧监听模式未收敛到 localhost: %+v", settings)
	}
}

func TestSaveNetworkSettingsRollsBackFileWhenRelayRestartFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), networkSettingsFile)
	original := []byte(`{"listenMode":"localhost","currentAddress":"127.0.0.1:19000","relayToken":"01234567890123456789012345678901"}`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	runtime := &failingRelayRuntime{addr: "127.0.0.1:19000", err: errors.New("address already in use")}
	service := &NetworkService{settingsPath: path, relayRuntime: runtime}

	err := service.SaveNetworkSettings(service.defaultSettings())
	if err == nil {
		t.Fatal("监听重启失败时应返回错误")
	}
	if len(runtime.restarts) != 1 || runtime.restarts[0] != "127.0.0.1:18100" {
		t.Fatalf("未尝试切换到目标监听地址: %#v", runtime.restarts)
	}
	current, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(current) != string(original) {
		t.Fatalf("网络设置未回滚\nwant: %s\n got: %s", original, current)
	}
}
