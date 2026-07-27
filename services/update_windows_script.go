package services

import "fmt"

// buildWindowsPortableUpdateScript 生成便携版自更新使用的 PowerShell 脚本。
//
// 脚本内禁止给 PowerShell 只读自动变量赋值（$PID、$HOME、$PWD 等）。
// 脚本头部是 $ErrorActionPreference = 'Stop'，一旦给只读变量赋值会立即终止，
// 而此时应用已经退出，结果是"更新静默失败且无人知情"，因此用 $targetPid 承载进程号。
func buildWindowsPortableUpdateScript(oldExePath, newExePath string, targetPid int) string {
	return fmt.Sprintf(`
$ErrorActionPreference = 'Stop'
$oldExe = '%s'
$newExe = '%s'
$targetPid = %d
$maxWait = 60

# 等待旧进程退出
$waited = 0
while ($waited -lt $maxWait) {
    try {
        $proc = Get-Process -Id $targetPid -ErrorAction SilentlyContinue
        if (-not $proc) { break }
    } catch { break }
    Start-Sleep -Milliseconds 500
    $waited += 0.5
}

if ($waited -ge $maxWait) {
    Write-Error "Timeout waiting for process to exit"
    exit 1
}

# 同卷 staging
$stagingPath = "$oldExe.new"
Copy-Item -Path $newExe -Destination $stagingPath -Force

# 验证复制成功
if (-not (Test-Path $stagingPath)) {
    Write-Error "Failed to copy new executable"
    exit 1
}

# 重命名交换（原子操作）
$backupPath = "$oldExe.old.exe"
$retries = 20
for ($i = 0; $i -lt $retries; $i++) {
    try {
        if (Test-Path $backupPath) { Remove-Item $backupPath -Force }
        Rename-Item -Path $oldExe -NewName (Split-Path $backupPath -Leaf) -Force
        Rename-Item -Path $stagingPath -NewName (Split-Path $oldExe -Leaf) -Force
        break
    } catch {
        if ($i -eq ($retries - 1)) {
            # 回滚
            if (Test-Path $backupPath) {
                Rename-Item -Path $backupPath -NewName (Split-Path $oldExe -Leaf) -Force -ErrorAction SilentlyContinue
            }
            throw
        }
        Start-Sleep -Milliseconds 100
    }
}

# 启动新版本
Start-Process -FilePath $oldExe -WorkingDirectory (Split-Path $oldExe)

# 清理（延迟）
Start-Sleep -Seconds 2
Remove-Item $backupPath -Force -ErrorAction SilentlyContinue
Remove-Item $newExe -Force -ErrorAction SilentlyContinue
`, oldExePath, newExePath, targetPid)
}
