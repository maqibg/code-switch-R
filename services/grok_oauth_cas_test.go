package services

import (
	"strings"
	"testing"
)

func TestReplaceGrokOAuthAccountCASRejectsStaleRefresh(t *testing.T) {
	original := grokOAuthAccount{
		ID:        "account-1",
		UpdatedAt: "2026-07-31T00:00:00Z",
		AuthEntry: map[string]any{"key": "old-token", "refresh_token": "old-refresh"},
	}
	store := grokOAuthStore{Accounts: []grokOAuthAccount{original}}
	expected := grokOAuthAccountRevision(original)
	store.Accounts[0].AuthEntry = map[string]any{"key": "newer-token", "refresh_token": "newer-refresh"}

	stale := original
	stale.AuthEntry = map[string]any{"key": "stale-token", "refresh_token": "stale-refresh"}
	err := replaceGrokOAuthAccountCAS(&store, original.ID, expected, stale)
	if err == nil || !strings.Contains(err.Error(), "其他操作更新") {
		t.Fatalf("陈旧刷新应触发 CAS 冲突，实际: %v", err)
	}
	if store.Accounts[0].AuthEntry["key"] != "newer-token" {
		t.Fatalf("CAS 冲突不得覆盖较新 Token: %#v", store.Accounts[0].AuthEntry)
	}
}

func TestReplaceGrokOAuthAccountCASCommitsMatchingRevision(t *testing.T) {
	original := grokOAuthAccount{ID: "account-1", AuthEntry: map[string]any{"key": "old-token"}}
	store := grokOAuthStore{Accounts: []grokOAuthAccount{original}}
	next := original
	next.AuthEntry = map[string]any{"key": "new-token"}
	if err := replaceGrokOAuthAccountCAS(&store, original.ID, grokOAuthAccountRevision(original), next); err != nil {
		t.Fatal(err)
	}
	if store.Accounts[0].AuthEntry["key"] != "new-token" {
		t.Fatalf("匹配版本应提交新 Token: %#v", store.Accounts[0].AuthEntry)
	}
}
