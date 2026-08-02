package services

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	"github.com/daodao97/xgo/xdb"
)

const (
	requestCostBackfillBatch = 800
	retentionDeleteBatch     = 2000
	logMaintenanceInterval   = 24 * time.Hour
)

func (ls *LogService) StartMaintenance() {
	if ls == nil {
		return
	}
	ls.maintenanceMu.Lock()
	if ls.maintenanceStop != nil {
		ls.maintenanceMu.Unlock()
		return
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	ls.maintenanceStop = stop
	ls.maintenanceDone = done
	ls.maintenanceMu.Unlock()

	go ls.runMaintenance(stop, done)
}

func (ls *LogService) StopMaintenance() {
	if ls == nil {
		return
	}
	ls.maintenanceMu.Lock()
	stop := ls.maintenanceStop
	done := ls.maintenanceDone
	ls.maintenanceStop = nil
	ls.maintenanceDone = nil
	if stop != nil {
		close(stop)
	}
	ls.maintenanceMu.Unlock()
	if done != nil {
		<-done
	}
}

func (ls *LogService) runMaintenance(stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	for {
		updated, complete, err := backfillDecimalMoneyBatch(decimalMoneyBackfillBatch)
		if err != nil {
			log.Printf("精确金额迁移失败: %v", err)
			break
		}
		if complete {
			break
		}
		select {
		case <-stop:
			return
		case <-time.After(25 * time.Millisecond):
		}
		if updated == 0 {
			// 没有可更新的行时仍让出调度，避免损坏数据库状态导致忙循环。
			select {
			case <-stop:
				return
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
	if ls.pricingService != nil {
		for {
			updated, err := ls.backfillStoredRequestCostsBatch(requestCostBackfillBatch)
			if err != nil {
				log.Printf("request_log 成本回填失败: %v", err)
				break
			}
			if updated < requestCostBackfillBatch {
				break
			}
			select {
			case <-stop:
				return
			case <-time.After(25 * time.Millisecond):
			}
		}
	}
	if _, err := ls.ApplyRetentionPolicy(); err != nil {
		log.Printf("历史记录保留策略执行失败: %v", err)
	}

	ticker := time.NewTicker(logMaintenanceInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if _, err := ls.ApplyRetentionPolicy(); err != nil {
				log.Printf("历史记录保留策略执行失败: %v", err)
			}
		case <-stop:
			return
		}
	}
}

func (ls *LogService) ApplyRetentionPolicy() (RecordCleanupResult, error) {
	if ls == nil || ls.appSettings == nil {
		return RecordCleanupResult{}, nil
	}
	settings, err := ls.appSettings.GetAppSettings()
	if err != nil {
		return RecordCleanupResult{}, fmt.Errorf("读取日志保留设置失败: %w", err)
	}
	if settings.LogRetentionDays <= 0 {
		storage, storageErr := ls.GetRecordStorageInfo()
		return RecordCleanupResult{Storage: storage}, storageErr
	}
	return ls.CleanupExpiredRecords(settings.LogRetentionDays)
}

func (ls *LogService) CleanupExpiredRecords(days int) (RecordCleanupResult, error) {
	if days < 1 || days > 3650 {
		return RecordCleanupResult{}, fmt.Errorf("日志保留天数必须在 1-3650 之间")
	}
	ls.cleanupMu.Lock()
	defer ls.cleanupMu.Unlock()

	db, err := xdb.DB("default")
	if err != nil {
		return RecordCleanupResult{}, fmt.Errorf("获取数据库连接失败: %w", err)
	}
	cutoff := formatCreatedAtBoundary(nowInBeijing().AddDate(0, 0, -days))
	requestCount, err := countRowsBefore(db, "request_log", cutoff)
	if err != nil {
		return RecordCleanupResult{}, err
	}
	attemptCount, err := countRowsBefore(db, "relay_attempt", cutoff)
	if err != nil {
		return RecordCleanupResult{}, err
	}
	if err := deleteRowsBeforeInBatches("request_log", cutoff, requestCount); err != nil {
		return RecordCleanupResult{}, err
	}
	if err := deleteRowsBeforeInBatches("relay_attempt", cutoff, attemptCount); err != nil {
		return RecordCleanupResult{}, err
	}

	result := RecordCleanupResult{
		DeletedRequestLogs:   requestCount,
		DeletedRelayAttempts: attemptCount,
	}
	if _, err := db.Exec("PRAGMA wal_checkpoint(PASSIVE)"); err != nil {
		result.Warning = fmt.Sprintf("WAL checkpoint 失败: %v", err)
	}
	storage, err := ls.GetRecordStorageInfo()
	if err != nil {
		return result, err
	}
	result.Storage = storage
	return result, nil
}

func countRowsBefore(db *sql.DB, table, cutoff string) (int64, error) {
	query := ""
	switch table {
	case "request_log":
		query = "SELECT COUNT(*) FROM request_log WHERE created_at < ?"
	case "relay_attempt":
		query = "SELECT COUNT(*) FROM relay_attempt WHERE created_at < ?"
	default:
		return 0, fmt.Errorf("不支持的保留策略表: %s", table)
	}
	var count int64
	if err := db.QueryRow(query, cutoff).Scan(&count); err != nil {
		if isNoSuchTableErr(err) {
			return 0, nil
		}
		return 0, err
	}
	return count, nil
}

func deleteRowsBeforeInBatches(table, cutoff string, count int64) error {
	query := ""
	switch table {
	case "request_log":
		query = `DELETE FROM request_log WHERE id IN (SELECT id FROM request_log WHERE created_at < ? ORDER BY id LIMIT ?)`
	case "relay_attempt":
		query = `DELETE FROM relay_attempt WHERE id IN (SELECT id FROM relay_attempt WHERE created_at < ? ORDER BY id LIMIT ?)`
	default:
		return fmt.Errorf("不支持的保留策略表: %s", table)
	}
	for remaining := count; remaining > 0; remaining -= retentionDeleteBatch {
		if err := dbExec(query, cutoff, retentionDeleteBatch); err != nil {
			return fmt.Errorf("清理 %s 过期记录失败: %w", table, err)
		}
	}
	return nil
}
