package services

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"path"
	"runtime"
	"strings"
	"time"
)

const openCodeWSLConfigPathScript = `
set -eu
value="${OPENCODE_CONFIG:-$HOME/.config/opencode/opencode.jsonc}"
if [ -z "${OPENCODE_CONFIG:-}" ] && [ ! -f "$value" ] && [ -f "$HOME/.config/opencode/opencode.json" ]; then
  value="$HOME/.config/opencode/opencode.json"
fi
printf '%s' "$value"
`

const openCodeWSLHashScript = `
set -eu
IFS= read -r target
if [ -f "$target" ]; then
  sha256sum -- "$target" | awk '{print $1}'
fi
`

const openCodeWSLWriteScript = `
set -euo pipefail
IFS= read -r config_path
IFS= read -r prompt_path
IFS= read -r prompt_exists
IFS= read -r config_b64
IFS= read -r prompt_b64

config_dir="$(dirname -- "$config_path")"
prompt_dir="$(dirname -- "$prompt_path")"
mkdir -p "$config_dir"
config_tmp="$(mktemp "$config_path.codeswitch.XXXXXX")"
prompt_tmp=""
config_backup=""
prompt_backup=""
config_existed=0
prompt_existed=0
cleanup() {
  [ -z "$config_tmp" ] || rm -f -- "$config_tmp"
  [ -z "$prompt_tmp" ] || rm -f -- "$prompt_tmp"
  [ -z "$config_backup" ] || rm -f -- "$config_backup"
  [ -z "$prompt_backup" ] || rm -f -- "$prompt_backup"
}
rollback() {
  if [ "$config_existed" = "1" ]; then
    cp -f -- "$config_backup" "$config_path"
  else
    rm -f -- "$config_path"
  fi
  if [ "$prompt_existed" = "1" ]; then
    cp -f -- "$prompt_backup" "$prompt_path"
  elif [ "$prompt_exists" = "1" ]; then
    rm -f -- "$prompt_path"
  fi
  cleanup
}
trap rollback ERR

printf '%s' "$config_b64" | base64 --decode > "$config_tmp"
if [ -e "$config_path" ]; then
  config_existed=1
  config_backup="$(mktemp)"
  cp -f -- "$config_path" "$config_backup"
  chmod --reference="$config_path" "$config_tmp" 2>/dev/null || true
else
  chmod 600 "$config_tmp" 2>/dev/null || true
fi

if [ "$prompt_exists" = "1" ]; then
  mkdir -p "$prompt_dir"
  prompt_tmp="$(mktemp "$prompt_path.codeswitch.XXXXXX")"
  printf '%s' "$prompt_b64" | base64 --decode > "$prompt_tmp"
  if [ -e "$prompt_path" ]; then
    prompt_existed=1
    prompt_backup="$(mktemp)"
    cp -f -- "$prompt_path" "$prompt_backup"
    chmod --reference="$prompt_path" "$prompt_tmp" 2>/dev/null || true
  else
    chmod 600 "$prompt_tmp" 2>/dev/null || true
  fi
fi

mv -f -- "$config_tmp" "$config_path"
if [ "$prompt_exists" = "1" ]; then
  mv -f -- "$prompt_tmp" "$prompt_path"
fi
trap - ERR
cleanup
`

func (s *OpenCodeService) ListWSLTargets() ([]OpenCodeWSLTargetInfo, error) {
	if runtime.GOOS != "windows" {
		return []OpenCodeWSLTargetInfo{}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	distros, err := listOpenCodeWSLDistros()
	if err != nil {
		return nil, err
	}
	state, err := loadOpenCodeState()
	if err != nil {
		return nil, err
	}
	result := make([]OpenCodeWSLTargetInfo, 0, len(distros))
	for _, distro := range distros {
		configPath, pathErr := resolveOpenCodeWSLConfigPath(distro)
		if pathErr != nil {
			result = append(result, OpenCodeWSLTargetInfo{Distro: distro, Error: pathErr.Error()})
			continue
		}
		promptPath := path.Join(path.Dir(configPath), "AGENTS.md")
		hash, hashErr := openCodeWSLFileHash(distro, configPath)
		entry := state.WSL[distro]
		info := OpenCodeWSLTargetInfo{
			Distro: distro, ConfigPath: configPath, PromptPath: promptPath,
			Exists: hash != "", Hash: hash, LastSyncHash: entry.LastSyncHash, LastSyncAt: entry.LastSyncAt,
		}
		if hashErr != nil {
			info.Error = hashErr.Error()
		}
		result = append(result, info)
	}
	return result, nil
}

func (s *OpenCodeService) SyncWSLConfig(input OpenCodeWSLSyncInput) (OpenCodeWSLSyncResult, error) {
	if runtime.GOOS != "windows" {
		return OpenCodeWSLSyncResult{}, fmt.Errorf("OpenCode WSL 同步只支持 Windows")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	distro := strings.TrimSpace(input.Distro)
	if distro == "" {
		return OpenCodeWSLSyncResult{}, fmt.Errorf("WSL 发行版不能为空")
	}
	allowed, err := openCodeWSLDistroExists(distro)
	if err != nil {
		return OpenCodeWSLSyncResult{}, err
	}
	if !allowed {
		return OpenCodeWSLSyncResult{}, fmt.Errorf("未找到 WSL 发行版 %q", distro)
	}
	configPath := strings.TrimSpace(input.ConfigPath)
	if configPath == "" {
		configPath, err = resolveOpenCodeWSLConfigPath(distro)
		if err != nil {
			return OpenCodeWSLSyncResult{}, err
		}
	}
	if err := validateOpenCodeWSLPath(configPath); err != nil {
		return OpenCodeWSLSyncResult{}, err
	}
	configSource, _, _, err := s.resolveTarget()
	if err != nil {
		return OpenCodeWSLSyncResult{}, err
	}
	configData, err := os.ReadFile(configSource)
	if err != nil {
		return OpenCodeWSLSyncResult{}, fmt.Errorf("读取 Windows OpenCode 配置失败: %w", err)
	}
	prompt, err := s.getGlobalPromptLocked()
	if err != nil {
		return OpenCodeWSLSyncResult{}, err
	}
	promptPath := path.Join(path.Dir(configPath), "AGENTS.md")
	configEncoded := base64.StdEncoding.EncodeToString(configData)
	promptEncoded := base64.StdEncoding.EncodeToString([]byte(prompt.Content))
	inputData := strings.Join([]string{configPath, promptPath, boolString(prompt.Exists), configEncoded, promptEncoded, ""}, "\n")
	if _, err := runOpenCodeWSLScript(distro, openCodeWSLWriteScript, []byte(inputData)); err != nil {
		return OpenCodeWSLSyncResult{}, fmt.Errorf("写入 WSL OpenCode 配置失败: %w", err)
	}
	hash, err := openCodeWSLFileHash(distro, configPath)
	if err != nil || hash == "" {
		if err == nil {
			err = fmt.Errorf("同步后无法读取目标 hash")
		}
		return OpenCodeWSLSyncResult{}, fmt.Errorf("WSL OpenCode 配置回读失败: %w", err)
	}
	state, err := loadOpenCodeState()
	if err != nil {
		return OpenCodeWSLSyncResult{}, err
	}
	syncedAt := time.Now().UTC().Format(time.RFC3339Nano)
	state.WSL[distro] = openCodeWSLState{Distro: distro, ConfigPath: configPath, PromptPath: promptPath, LastSyncHash: hash, LastSyncAt: syncedAt}
	if err := saveOpenCodeState(state); err != nil {
		return OpenCodeWSLSyncResult{}, fmt.Errorf("保存 WSL 同步状态失败（目标文件已写入）: %w", err)
	}
	return OpenCodeWSLSyncResult{Distro: distro, ConfigPath: configPath, PromptPath: promptPath, Hash: hash, SyncedAt: syncedAt}, nil
}

func listOpenCodeWSLDistros() ([]string, error) {
	if runtime.GOOS != "windows" {
		return []string{}, nil
	}
	output, err := hideWindowCmd("wsl", "--list", "--quiet").Output()
	if err != nil {
		return nil, fmt.Errorf("读取 WSL 发行版失败: %w", err)
	}
	decoded := decodeUTF16LE(output)
	seen := make(map[string]struct{})
	distros := make([]string, 0)
	for _, line := range strings.Split(decoded, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "*"))
		if line == "" {
			continue
		}
		if _, exists := seen[line]; exists {
			continue
		}
		seen[line] = struct{}{}
		distros = append(distros, line)
	}
	return distros, nil
}

func openCodeWSLDistroExists(distro string) (bool, error) {
	distros, err := listOpenCodeWSLDistros()
	if err != nil {
		return false, err
	}
	for _, candidate := range distros {
		if candidate == distro {
			return true, nil
		}
	}
	return false, nil
}

func resolveOpenCodeWSLConfigPath(distro string) (string, error) {
	output, err := runOpenCodeWSLScript(distro, openCodeWSLConfigPathScript, nil)
	if err != nil {
		return "", fmt.Errorf("解析 WSL OpenCode 配置路径失败: %w", err)
	}
	value := strings.TrimSpace(string(output))
	if err := validateOpenCodeWSLPath(value); err != nil {
		return "", err
	}
	return value, nil
}

func openCodeWSLFileHash(distro, target string) (string, error) {
	if err := validateOpenCodeWSLPath(target); err != nil {
		return "", err
	}
	output, err := runOpenCodeWSLScript(distro, openCodeWSLHashScript, []byte(target+"\n"))
	if err != nil {
		return "", fmt.Errorf("读取 WSL OpenCode 配置 hash 失败: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

func validateOpenCodeWSLPath(value string) error {
	value = strings.TrimSpace(value)
	if value == "" || !strings.HasPrefix(value, "/") {
		return fmt.Errorf("WSL OpenCode 目标路径必须是绝对路径")
	}
	if strings.ContainsAny(value, "\\\x00\r\n") {
		return fmt.Errorf("WSL OpenCode 目标路径包含非法字符")
	}
	for _, component := range strings.Split(value, "/") {
		if component == ".." {
			return fmt.Errorf("WSL OpenCode 目标路径不能包含 ..")
		}
	}
	return nil
}

func runOpenCodeWSLScript(distro, script string, input []byte) ([]byte, error) {
	if runtime.GOOS != "windows" {
		return nil, fmt.Errorf("WSL 只支持 Windows")
	}
	cmd := hideWindowCmd("wsl", "-d", distro, "bash", "-s")
	payload := append([]byte(script+"\n"), input...)
	cmd.Stdin = bytes.NewReader(payload)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%v: %s", err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

func boolString(value bool) string {
	if value {
		return "1"
	}
	return "0"
}
