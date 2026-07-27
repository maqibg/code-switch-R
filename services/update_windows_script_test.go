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
