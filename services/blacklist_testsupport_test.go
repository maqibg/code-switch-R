package services

// 黑名单测试共用助手。原定义在 relay_failover_committed_test.go，
// relay 拆包后该文件迁走，留下侧的黑名单测试在此保留一份。

import (
	"testing"

	"github.com/daodao97/xgo/xdb"
)

// failureCountFor 直接查库读取指定目标的连续失败计数
func failureCountFor(t *testing.T, scope, providerName string) int {
	t.Helper()
	db, err := xdb.DB("default")
	if err != nil {
		return 0
	}
	target := BlacklistTargetFor(scope, Provider{Name: providerName})
	var count int
	err = db.QueryRow(
		`SELECT failure_count FROM provider_blacklist
		 WHERE platform = ? AND COALESCE(source_id, '') = ? AND provider_name = ?`,
		target.platform, target.sourceID, providerName,
	).Scan(&count)
	if err != nil {
		return 0
	}
	return count
}
