package services

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestAtomicWriteJSONSupportsConcurrentReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "preferences.json")
	if err := AtomicWriteJSON(path, map[string]int{"writer": -1}); err != nil {
		t.Fatal(err)
	}

	const writerCount = 32
	start := make(chan struct{})
	errors := make(chan error, writerCount)
	var wait sync.WaitGroup
	for writer := 0; writer < writerCount; writer++ {
		wait.Add(1)
		go func(writer int) {
			defer wait.Done()
			<-start
			errors <- AtomicWriteJSON(path, map[string]int{"writer": writer})
		}(writer)
	}
	close(start)
	wait.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatalf("concurrent atomic write failed: %v", err)
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]int
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatalf("final file is not valid JSON: %v; data=%q", err, data)
	}
	if saved["writer"] < 0 || saved["writer"] >= writerCount {
		t.Fatalf("unexpected final writer: %#v", saved)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() != filepath.Base(path) {
			t.Fatalf("atomic write left temporary file behind: %s", entry.Name())
		}
	}
}
