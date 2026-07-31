package services

import (
	"regexp"
	"strings"
	"testing"
)

// PowerShell 只读自动变量：给它们赋值会在 $ErrorActionPreference='Stop' 下终止脚本。
var powershellReadOnlyVars = []string{
	"PID", "HOME", "PWD", "PSHOME", "HOST", "TRUE", "FALSE", "NULL",
	"PSVersionTable", "PSCulture", "ExecutionContext", "MyInvocation",
}

func TestBuildWindowsPortableUpdateScriptAvoidsReadOnlyAssignment(t *testing.T) {
	script := buildWindowsPortableUpdateScript(`C:\App\code-switch-R.exe`, `C:\Temp\new.exe`, 4321)

	for _, name := range powershellReadOnlyVars {
		// 匹配 "$Name =" / "$Name=" 形式的赋值，大小写不敏感（PowerShell 变量名不区分大小写）
		pattern := regexp.MustCompile(`(?i)\$` + regexp.QuoteMeta(name) + `\s*=`)
		if pattern.MatchString(script) {
			t.Fatalf("脚本给 PowerShell 只读变量 $%s 赋值，会导致更新静默失败:\n%s", name, script)
		}
	}
}

func TestBuildWindowsPortableUpdateScriptEmbedsArguments(t *testing.T) {
	oldExe := `C:\App\code-switch-R.exe`
	newExe := `C:\Temp\downloaded.exe`
	script := buildWindowsPortableUpdateScript(oldExe, newExe, 4321)

	if !strings.Contains(script, "$targetPid = 4321") {
		t.Errorf("脚本未正确写入目标进程号:\n%s", script)
	}
	if !strings.Contains(script, "$oldExe = '"+oldExe+"'") {
		t.Errorf("脚本未正确写入旧可执行文件路径:\n%s", script)
	}
	if !strings.Contains(script, "$newExe = '"+newExe+"'") {
		t.Errorf("脚本未正确写入新可执行文件路径:\n%s", script)
	}
	// 进程号必须被实际使用，否则等待旧进程退出的逻辑是空转
	if !strings.Contains(script, "Get-Process -Id $targetPid") {
		t.Errorf("脚本未使用 $targetPid 等待旧进程退出:\n%s", script)
	}
}

func TestPortableUpdateUsesReleaseRepository(t *testing.T) {
	if latestJSONURL != "https://github.com/maqibg/code-switch-R/releases/latest/download/latest.json" {
		t.Fatalf("latest.json 地址与发布仓库不一致: %s", latestJSONURL)
	}
	if githubAPIURL != "https://api.github.com/repos/maqibg/code-switch-R/releases/latest" {
		t.Fatalf("GitHub API 地址与发布仓库不一致: %s", githubAPIURL)
	}

	allowed := []string{
		"https://github.com/maqibg/code-switch-R/releases/download/v2.7.0/codeSwitchR.exe",
		"https://github.com/maqibg/code-switch-R/releases/latest/download/latest.json",
		"https://objects.githubusercontent.com/github-production-release-asset/example",
	}
	for _, url := range allowed {
		if !isURLAllowed(url) {
			t.Errorf("应允许发布资产 URL: %s", url)
		}
	}

	rejected := []string{
		"https://github.com/Rogers-F/code-switch-R/releases/download/v2.7.0/codeSwitchR.exe",
		"https://github.com/maqibg/code-switch-R.evil/releases/download/v2.7.0/codeSwitchR.exe",
		"http://github.com/maqibg/code-switch-R/releases/download/v2.7.0/codeSwitchR.exe",
	}
	for _, url := range rejected {
		if isURLAllowed(url) {
			t.Errorf("不应允许非发布仓库 URL: %s", url)
		}
	}
}
