package infra

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// maxTimestampedBackups 每个文件保留的时间戳快照数量上限
const maxTimestampedBackups = 5

// WriteTimestampedBackup 为即将被改写的用户配置文件留一份带时间戳的快照。
//
// 存在的理由：各平台的「基线备份」只在首次启用代理时写入一次，
// 用户在代理启用期间对配置文件的后续编辑不在基线备份里。
// 一旦写入路径出问题，只有基线备份是不够恢复的。
//
// 快照写在原文件同目录下，命名为 <原名>.bak.<时间戳>，只保留最近若干份。
func WriteTimestampedBackup(originalPath string, content []byte) error {
	if len(content) == 0 {
		return nil
	}
	dir := filepath.Dir(originalPath)
	base := filepath.Base(originalPath)
	snapshot := filepath.Join(dir, fmt.Sprintf("%s.bak.%s", base, time.Now().Format("20060102-150405")))

	if err := os.WriteFile(snapshot, content, 0o600); err != nil {
		return fmt.Errorf("写入配置快照失败: %w", err)
	}
	pruneTimestampedBackups(dir, base)
	return nil
}

// pruneTimestampedBackups 按文件名排序（时间戳格式可直接字典序比较）删除超量的旧快照
func pruneTimestampedBackups(dir, base string) {
	prefix := base + ".bak."
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if len(name) > len(prefix) && name[:len(prefix)] == prefix {
			names = append(names, name)
		}
	}
	if len(names) <= maxTimestampedBackups {
		return
	}

	sort.Strings(names)
	for _, name := range names[:len(names)-maxTimestampedBackups] {
		_ = os.Remove(filepath.Join(dir, name))
	}
}
