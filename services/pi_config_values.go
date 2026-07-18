package services

import (
	"bytes"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"
	"time"
)

const (
	piConfigCommandTimeout   = 10 * time.Second
	piConfigCommandOutputMax = 1024 * 1024
)

type piConfigCommandOutput struct {
	buffer   bytes.Buffer
	exceeded bool
}

func (w *piConfigCommandOutput) Write(data []byte) (int, error) {
	remaining := piConfigCommandOutputMax - w.buffer.Len()
	if remaining > 0 {
		if len(data) < remaining {
			remaining = len(data)
		}
		_, _ = w.buffer.Write(data[:remaining])
	}
	if remaining < len(data) {
		w.exceeded = true
	}
	return len(data), nil
}

func resolvePiProviderConfigValues(provider Provider, platform string) (Provider, error) {
	if !strings.EqualFold(strings.TrimSpace(platform), "pi") {
		return provider, nil
	}
	if provider.APIKey != "" {
		resolved, err := resolvePiConfigValue(provider.APIKey, "API Key")
		if err != nil {
			return Provider{}, err
		}
		provider.APIKey = resolved
	}
	if len(provider.Headers) == 0 {
		return provider, nil
	}
	resolvedHeaders := make(map[string]string, len(provider.Headers))
	for key, value := range provider.Headers {
		resolved, err := resolvePiConfigValue(value, fmt.Sprintf("Header %q", key))
		if err != nil {
			return Provider{}, err
		}
		resolvedHeaders[key] = resolved
	}
	provider.Headers = resolvedHeaders
	return provider, nil
}

func resolvePiConfigValue(config, description string) (string, error) {
	if strings.HasPrefix(config, "!") {
		return executePiConfigCommand(strings.TrimPrefix(config, "!"), description)
	}

	var resolved strings.Builder
	missing := make(map[string]struct{})
	for index := 0; index < len(config); {
		if config[index] != '$' {
			resolved.WriteByte(config[index])
			index++
			continue
		}
		if index+1 >= len(config) {
			resolved.WriteByte('$')
			index++
			continue
		}
		next := config[index+1]
		if next == '$' || next == '!' {
			resolved.WriteByte(next)
			index += 2
			continue
		}

		name, consumed := parsePiEnvironmentReference(config[index:])
		if consumed == 0 {
			resolved.WriteByte('$')
			index++
			continue
		}
		value, exists := os.LookupEnv(name)
		if !exists || value == "" {
			missing[name] = struct{}{}
		} else {
			resolved.WriteString(value)
		}
		index += consumed
	}
	if len(missing) > 0 {
		names := make([]string, 0, len(missing))
		for name := range missing {
			names = append(names, name)
		}
		sort.Strings(names)
		return "", fmt.Errorf("解析 Pi %s 失败，缺少环境变量: %s", description, strings.Join(names, ", "))
	}
	return resolved.String(), nil
}

func escapePiConfigLiteral(value string) string {
	escaped := strings.ReplaceAll(value, "$", "$$")
	if strings.HasPrefix(escaped, "!") {
		return "$!" + strings.TrimPrefix(escaped, "!")
	}
	return escaped
}

func parsePiEnvironmentReference(value string) (name string, consumed int) {
	if len(value) < 2 || value[0] != '$' {
		return "", 0
	}
	if value[1] == '{' {
		end := strings.IndexByte(value[2:], '}')
		if end < 0 {
			return "", 0
		}
		end += 2
		name = value[2:end]
		if !isPiEnvironmentName(name) {
			return "", 0
		}
		return name, end + 1
	}
	end := 1
	for end < len(value) && isPiEnvironmentNameCharacter(value[end], end == 1) {
		end++
	}
	if end == 1 {
		return "", 0
	}
	return value[1:end], end
}

func isPiEnvironmentName(value string) bool {
	if value == "" {
		return false
	}
	for index := 0; index < len(value); index++ {
		if !isPiEnvironmentNameCharacter(value[index], index == 0) {
			return false
		}
	}
	return true
}

func isPiEnvironmentNameCharacter(value byte, first bool) bool {
	if value == '_' || value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z' {
		return true
	}
	return !first && value >= '0' && value <= '9'
}

func executePiConfigCommand(command, description string) (string, error) {
	if strings.TrimSpace(command) == "" {
		return "", fmt.Errorf("解析 Pi %s 失败，配置命令为空", description)
	}
	var cmdName string
	var cmdArgs []string
	if runtime.GOOS == "windows" {
		cmdName = os.Getenv("ComSpec")
		if cmdName == "" {
			cmdName = "cmd.exe"
		}
		cmdArgs = []string{"/d", "/s", "/c", command}
	} else {
		cmdName = os.Getenv("SHELL")
		if cmdName == "" {
			cmdName = "/bin/sh"
		}
		cmdArgs = []string{"-c", command}
	}

	cmd := hideWindowCmd(cmdName, cmdArgs...)
	output := &piConfigCommandOutput{}
	cmd.Stdout = output
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("解析 Pi %s 失败，无法启动配置命令: %w", description, err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	timer := time.NewTimer(piConfigCommandTimeout)
	defer timer.Stop()
	select {
	case err := <-done:
		if err != nil {
			return "", fmt.Errorf("解析 Pi %s 失败，配置命令执行失败: %w", description, err)
		}
	case <-timer.C:
		_ = cmd.Process.Kill()
		<-done
		return "", fmt.Errorf("解析 Pi %s 失败，配置命令执行超过 10 秒", description)
	}
	if output.exceeded {
		return "", fmt.Errorf("解析 Pi %s 失败，配置命令输出超过 1 MiB", description)
	}
	resolved := strings.TrimSpace(output.buffer.String())
	if resolved == "" {
		return "", fmt.Errorf("解析 Pi %s 失败，配置命令没有输出", description)
	}
	return resolved, nil
}
