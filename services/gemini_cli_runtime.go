package services

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type GeminiCLIRuntimeSource string

const (
	GeminiCLIRuntimeEnvironment GeminiCLIRuntimeSource = "environment"
	GeminiCLIRuntimeDefault     GeminiCLIRuntimeSource = "default"
)

type GeminiCLIRuntime struct {
	Root   string                 `json:"root"`
	Source GeminiCLIRuntimeSource `json:"source"`
}

// ResolveGeminiCLIRuntime 统一解析 Gemini CLI 的配置根目录。
// GEMINI_CLI_HOME 既兼容父目录形式，也兼容用户直接填写 .gemini 的形式。
func ResolveGeminiCLIRuntime() (GeminiCLIRuntime, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return GeminiCLIRuntime{}, fmt.Errorf("无法获取用户主目录: %w", err)
	}
	return ResolveGeminiCLIRuntimeFrom(home, os.Getenv("GEMINI_CLI_HOME"))
}

func ResolveGeminiCLIRuntimeFrom(home, configured string) (GeminiCLIRuntime, error) {
	home = strings.TrimSpace(home)
	if home == "" {
		return GeminiCLIRuntime{}, fmt.Errorf("Gemini CLI runtime 缺少 home")
	}
	configured = strings.TrimSpace(configured)
	if configured == "" {
		return GeminiCLIRuntime{Root: filepath.Join(home, ".gemini"), Source: GeminiCLIRuntimeDefault}, nil
	}
	if !filepath.IsAbs(configured) {
		configured = filepath.Join(home, configured)
	}
	if !strings.EqualFold(filepath.Base(filepath.Clean(configured)), ".gemini") {
		configured = filepath.Join(configured, ".gemini")
	}
	return GeminiCLIRuntime{Root: filepath.Clean(configured), Source: GeminiCLIRuntimeEnvironment}, nil
}

func geminiCLIRoot() string {
	runtime, err := ResolveGeminiCLIRuntime()
	if err == nil && runtime.Root != "" {
		return runtime.Root
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".gemini")
}

func GeminiCLIManagedEnvKeys() []string {
	return []string{
		"GEMINI_API_KEY", "GOOGLE_API_KEY", "GOOGLE_GEMINI_BASE_URL", "GEMINI_MODEL",
		"GOOGLE_GENAI_USE_VERTEXAI", "GOOGLE_CLOUD_PROJECT", "GOOGLE_CLOUD_LOCATION",
		"GOOGLE_APPLICATION_CREDENTIALS", "GOOGLE_GEMINI_API_VERSION", "GEMINI_CUSTOM_HEADERS",
	}
}

func clearGeminiCLIManagedEnv(values map[string]string) {
	for _, key := range GeminiCLIManagedEnvKeys() {
		delete(values, key)
	}
}

func isGeminiCLIManagedEnvKey(key string) bool {
	for _, managed := range GeminiCLIManagedEnvKeys() {
		if key == managed {
			return true
		}
	}
	return false
}

// GeminiCLIPromptFileName 根据 settings.json.context.fileName 解析 Prompt 文件名。
// 数组取第一个有效字符串；无效或缺失时回退 GEMINI.md。
func GeminiCLIPromptFileName(settings map[string]any) string {
	contextConfig, _ := settings["context"].(map[string]any)
	switch value := contextConfig["fileName"].(type) {
	case string:
		if name := strings.TrimSpace(value); name != "" {
			return filepath.Base(name)
		}
	case []any:
		for _, raw := range value {
			if name, ok := raw.(string); ok && strings.TrimSpace(name) != "" {
				return filepath.Base(strings.TrimSpace(name))
			}
		}
	}
	return "GEMINI.md"
}
