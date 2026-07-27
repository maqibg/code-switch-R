package services

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// atomicWriteLocks 按目标路径分段加锁。
//
// 早先这里是一把全局互斥锁，任意两个原子写都会互相串行——写 app.json 会阻塞
// 写 provider JSON，而它们根本没有共享状态。改为按路径加锁后，只有针对同一个
// 文件的并发写才需要排队。配置文件路径数量有限且由固定的 path 辅助函数生成，
// 所以这个 map 不会无界增长。
var atomicWriteLocks sync.Map // map[string]*sync.Mutex

// atomicWriteLockKey 归一化路径作为锁的 key。
// Windows 上文件名大小写不敏感，必须统一大小写，否则同一文件的两种写法会拿到两把锁。
func atomicWriteLockKey(path string) string {
	cleaned := filepath.Clean(path)
	if runtime.GOOS == "windows" {
		return strings.ToLower(cleaned)
	}
	return cleaned
}

func atomicWriteLockFor(path string) *sync.Mutex {
	actual, _ := atomicWriteLocks.LoadOrStore(atomicWriteLockKey(path), &sync.Mutex{})
	return actual.(*sync.Mutex)
}

// atomicWriteFile 原子写入文件（跨平台）
// 策略：写入临时文件 → fsync → 重命名
// 防止断电或崩溃导致状态文件损坏
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	mu := atomicWriteLockFor(path)
	mu.Lock()
	defer mu.Unlock()

	dir := filepath.Dir(path)

	// 确保目录存在
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("创建目录失败 %s（目标文件: %s）: %w", dir, path, err)
	}

	// 创建临时文件（同目录，确保同文件系统）
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("创建临时文件失败 %s（目标文件: %s）: %w", dir, path, err)
	}
	tmpPath := tmp.Name()

	// 写入数据
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("写入临时文件失败 %s（目标文件: %s）: %w", tmpPath, path, err)
	}

	// 设置文件权限
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("设置权限失败 %s（目标文件: %s）: %w", tmpPath, path, err)
	}

	// 确保数据落盘（fsync）
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("同步临时文件失败 %s（目标文件: %s）: %w", tmpPath, path, err)
	}

	// 关闭文件
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("关闭临时文件失败 %s（目标文件: %s）: %w", tmpPath, path, err)
	}

	// 原子重命名（平台特定实现）
	return atomicRename(tmpPath, path)
}
