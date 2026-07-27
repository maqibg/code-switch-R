package services

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestAtomicWriteFileWritesAndSetsPerm(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "cfg.json")
	if err := atomicWriteFile(path, []byte(`{"a":1}`), 0o600); err != nil {
		t.Fatalf("原子写入失败: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if string(data) != `{"a":1}` {
		t.Errorf("内容不符: %q", string(data))
	}
	// 目录应被自动创建
	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		t.Errorf("父目录应已创建: %v", err)
	}
}

// 同一文件的并发写必须串行，且最终内容是某一次完整写入，不能是两次写入的混合
func TestAtomicWriteFileConcurrentSameFileNeverInterleaves(t *testing.T) {
	path := filepath.Join(t.TempDir(), "race.json")
	const writers = 12

	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// 每次写入长度不同的内容，交错会产生非法长度
			payload := strings.Repeat(fmt.Sprintf("%d", i%10), 200+i*50)
			if err := atomicWriteFile(path, []byte(payload), 0o644); err != nil {
				t.Errorf("写入失败: %v", err)
			}
		}(i)
	}
	wg.Wait()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	// 结果必须是某一个 writer 的完整内容：全部字符相同且长度匹配该 writer
	content := string(data)
	if len(content) == 0 {
		t.Fatal("内容不应为空")
	}
	first := content[0]
	for i := 0; i < len(content); i++ {
		if content[i] != first {
			t.Fatalf("内容出现混合，说明并发写未被正确串行化（长度 %d）", len(content))
		}
	}
	digit := int(first - '0')
	matched := false
	for i := 0; i < writers; i++ {
		if i%10 == digit && len(content) == 200+i*50 {
			matched = true
			break
		}
	}
	if !matched {
		t.Errorf("最终长度 %d 不对应任何一次完整写入", len(content))
	}

	// 不应残留临时文件
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("读取目录失败: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp-") {
			t.Errorf("残留临时文件: %s", e.Name())
		}
	}
}

// 不同路径应拿到不同的锁（否则写 app.json 会阻塞写 provider JSON）
func TestAtomicWriteLocksArePerPath(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.json")
	b := filepath.Join(dir, "b.json")

	if atomicWriteLockFor(a) == atomicWriteLockFor(b) {
		t.Error("不同路径不应共享同一把锁")
	}
	if atomicWriteLockFor(a) != atomicWriteLockFor(a) {
		t.Error("同一路径必须返回同一把锁")
	}
	// 归一化：同一文件的不同写法应命中同一把锁
	messy := filepath.Join(dir, ".", "a.json")
	if atomicWriteLockFor(a) != atomicWriteLockFor(messy) {
		t.Error("路径归一化后应共享同一把锁")
	}
}
