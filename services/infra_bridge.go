package services

import "codeswitch/internal/infra"

// 迁移期兼容层：文件系统、路径、日志等基础工具的实现已搬到
// codeswitch/internal/infra（A4/A5 拆包第 1 步）。
//
// services 包内的既有调用点仍用原名，通过下面的别名转发；
// 后续按域拆包时各域改为直接 import infra，全部迁完后删除本文件。
const projectAppConfigDirName = infra.ProjectAppConfigDirName

var (
	// 原子写与文件工具（原 atomic_write.go / fileutils.go）
	atomicWriteFile  = infra.AtomicWriteFile
	AtomicWriteJSON  = infra.AtomicWriteJSON
	AtomicWriteBytes = infra.AtomicWriteBytes
	AtomicWriteText  = infra.AtomicWriteText
	CreateBackup     = infra.CreateBackup
	RestoreBackup    = infra.RestoreBackup
	ReadJSONFile     = infra.ReadJSONFile
	FileExists       = infra.FileExists
	EnsureDir        = infra.EnsureDir
	FindLatestBackup = infra.FindLatestBackup
	OpenInExplorer   = infra.OpenInExplorer

	// 路径（原 userhome.go）
	getUserHomeDir      = infra.GetUserHomeDir
	getExecutableDir    = infra.GetExecutableDir
	getAppConfigDir     = infra.GetAppConfigDir
	ensureAppConfigDir  = infra.EnsureAppConfigDir
	mustGetAppConfigDir = infra.MustGetAppConfigDir

	// 进程与备份（原 cmd_*.go / config_backup.go）
	hideWindowCmd          = infra.HideWindowCmd
	writeTimestampedBackup = infra.WriteTimestampedBackup

	// 结构化日志（原 applog.go）
	logInfo                = infra.LogInfo
	logWarn                = infra.LogWarn
	logError               = infra.LogError
	logDebug               = infra.LogDebug
	registerConsoleLogSink = infra.RegisterConsoleLogSink
	setAppLogRawOutput     = infra.SetAppLogRawOutput
)
