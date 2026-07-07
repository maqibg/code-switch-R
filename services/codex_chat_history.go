package services

import (
	"encoding/json"
	"strings"
	"sync"
)

type CodexChatBridgeHistoryStore struct {
	mu         sync.RWMutex
	items      map[string][]map[string]any
	order      []string
	maxEntries int
}

func NewCodexChatBridgeHistoryStore(maxEntries int) *CodexChatBridgeHistoryStore {
	if maxEntries <= 0 {
		maxEntries = 128
	}
	return &CodexChatBridgeHistoryStore{
		items: make(map[string][]map[string]any), order: make([]string, 0, maxEntries), maxEntries: maxEntries,
	}
}

func (s *CodexChatBridgeHistoryStore) Load(responseID string) ([]map[string]any, bool) {
	if s == nil || strings.TrimSpace(responseID) == "" {
		return nil, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	messages, ok := s.items[responseID]
	if !ok {
		return nil, false
	}
	return cloneChatMessages(messages), true
}

func (s *CodexChatBridgeHistoryStore) Store(responseID string, messages []map[string]any) {
	if s == nil || strings.TrimSpace(responseID) == "" || len(messages) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.items[responseID]; !exists {
		s.order = append(s.order, responseID)
	}
	s.items[responseID] = cloneChatMessages(messages)
	for len(s.order) > s.maxEntries {
		oldest := s.order[0]
		s.order = s.order[1:]
		delete(s.items, oldest)
	}
}

func appendAssistantChatMessage(messages []map[string]any, content string) []map[string]any {
	result := cloneChatMessages(messages)
	return append(result, map[string]any{"role": "assistant", "content": content})
}

func codexPreviousResponseID(body map[string]any) string {
	return strings.TrimSpace(stringFromMap(body, "previous_response_id"))
}

func codexPreviousResponseIDFromBytes(bodyBytes []byte) string {
	var body map[string]any
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		return ""
	}
	return codexPreviousResponseID(body)
}
