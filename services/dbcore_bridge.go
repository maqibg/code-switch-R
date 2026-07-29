package services

import "codeswitch/internal/dbcore"

// 迁移期兼容层：数据库连接与写入原语已搬到 codeswitch/internal/dbcore
// （A4/A5 拆包第 2 步）。migrations 与 InitDatabase 编排仍在本包。
//
// services 包内的既有调用点仍用原名，通过下面的别名转发；
// 后续按域拆包时各域改为直接 import dbcore，全部迁完后删除本文件。

// 事务与批量语句的类型别名（类型别名保证与 dbcore 完全同型，调用点零改动）
type (
	dbStatement  = dbcore.Statement
	dbTxExecutor = dbcore.TxExecutor
)

const appDatabaseFilename = dbcore.AppDatabaseFilename

// CloseDatabase 关闭主库连接（main.go 在 shutdown 时调用）
var CloseDatabase = dbcore.Close

var (
	dbHandle            = dbcore.Handle
	dbExec              = dbcore.Exec
	dbExecCtx           = dbcore.ExecCtx
	dbExecInImmediateTx = dbcore.ExecInImmediateTx
	dbExecInTx          = dbcore.ExecInTx
	buildAppSQLiteDSN   = dbcore.BuildAppSQLiteDSN
	verifySQLitePragmas = dbcore.VerifySQLitePragmas
	boolToInt           = dbcore.BoolToInt
	nullableProviderID  = dbcore.NullableID
)
