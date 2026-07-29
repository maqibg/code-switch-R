package infra

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	ProjectAppConfigDirName = ".code-switch-R"
)

// GetUserHomeDir 获取并校验用户家目录
// 确保返回值非空、绝对路径，避免相对路径导致写入到工作目录等安全问题
func GetUserHomeDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("获取用户家目录失败: %w", err)
	}

	home = filepath.Clean(home)
	if home == "" || home == "." {
		return "", fmt.Errorf("无效的家目录路径: 空路径")
	}
	if !filepath.IsAbs(home) {
		return "", fmt.Errorf("无效的家目录路径: 非绝对路径: %s", home)
	}

	return home, nil
}

func GetExecutableDir() (string, error) {
	exePath, err := os.Executable()
	if err != nil || strings.TrimSpace(exePath) == "" {
		cwd, cwdErr := os.Getwd()
		if cwdErr != nil {
			if err != nil {
				return "", fmt.Errorf("获取可执行文件目录失败: %w", err)
			}
			return "", fmt.Errorf("获取工作目录失败: %w", cwdErr)
		}
		exePath = cwd
	}

	if resolved, err := filepath.EvalSymlinks(exePath); err == nil {
		exePath = resolved
	}
	exePath = filepath.Clean(exePath)
	if filepath.Ext(exePath) != "" {
		exePath = filepath.Dir(exePath)
	}
	if exePath == "" || exePath == "." || !filepath.IsAbs(exePath) {
		return "", fmt.Errorf("无效的程序目录: %s", exePath)
	}
	return exePath, nil
}

func GetAppConfigDir() (string, error) {
	exeDir, err := GetExecutableDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(exeDir, ProjectAppConfigDirName), nil
}

func EnsureAppConfigDir() (string, error) {
	dir, err := GetAppConfigDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func MustGetAppConfigDir() string {
	dir, err := GetAppConfigDir()
	if err == nil {
		return dir
	}
	return filepath.Join(".", ProjectAppConfigDirName)
}
