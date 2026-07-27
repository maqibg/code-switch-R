package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	removedDeepSeekCodePlatform = "deepseekcode"
	removedDeepSeekCodeDir      = ".deepseek-code"
)

// CleanupRemovedDeepSeekCodeProxy 只恢复仍由已移除平台集成持有的字段。
// 用户已修改的字段具有更高优先级，清理过程不得覆盖。
func CleanupRemovedDeepSeekCodeProxy() error {
	exists, err := ProxyStateExists(removedDeepSeekCodePlatform)
	if err != nil || !exists {
		return err
	}

	state, err := LoadProxyState(removedDeepSeekCodePlatform)
	if err != nil {
		return fmt.Errorf("读取旧 DeepSeekCode 代理状态失败: %w", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	expectedPath := filepath.Join(home, removedDeepSeekCodeDir, "settings.json")
	if !samePath(state.TargetPath, expectedPath) {
		return fmt.Errorf("旧 DeepSeekCode 代理状态目标路径异常: %s", state.TargetPath)
	}

	content, err := os.ReadFile(expectedPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return DeleteProxyState(removedDeepSeekCodePlatform)
		}
		return err
	}

	payload := make(map[string]any)
	if len(content) > 0 {
		if err := json.Unmarshal(content, &payload); err != nil {
			return fmt.Errorf("解析旧 DeepSeekCode settings.json 失败: %w", err)
		}
	}
	if payload == nil {
		payload = make(map[string]any)
	}

	if restoreRemovedDeepSeekCodeProxyFields(payload, state) {
		if !state.FileExisted && len(payload) == 0 {
			if err := os.Remove(expectedPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		} else if err := AtomicWriteJSON(expectedPath, payload); err != nil {
			return err
		}
	}

	return DeleteProxyState(removedDeepSeekCodePlatform)
}

func restoreRemovedDeepSeekCodeProxyFields(payload map[string]any, state *ProxyState) bool {
	if state == nil {
		return false
	}
	env, _ := payload["env"].(map[string]any)
	if env == nil {
		return false
	}

	changed := false
	currentBaseURL := anyToString(env["DEEPSEEK_BASE_URL"])
	if sameProxyURL(currentBaseURL, state.InjectedBaseURL) {
		if state.OriginalBaseURL != nil {
			env["DEEPSEEK_BASE_URL"] = *state.OriginalBaseURL
		} else {
			delete(env, "DEEPSEEK_BASE_URL")
		}
		changed = true
	}

	if anyToString(env["DEEPSEEK_API_KEY"]) == state.InjectedAuthToken {
		if state.OriginalAuthToken != nil {
			env["DEEPSEEK_API_KEY"] = *state.OriginalAuthToken
		} else {
			delete(env, "DEEPSEEK_API_KEY")
		}
		changed = true
	}

	if changed {
		if len(env) == 0 && !state.EnvExisted {
			delete(payload, "env")
		} else {
			payload["env"] = env
		}
	}
	return changed
}

func sameProxyURL(left, right string) bool {
	normalize := func(value string) string {
		return strings.TrimSuffix(strings.TrimSpace(value), "/")
	}
	return strings.EqualFold(normalize(left), normalize(right))
}
