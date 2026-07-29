package relay

import (
	"encoding/json"
	"strings"
	"sync"

	"github.com/tidwall/gjson"
)

const codexChatHistoryMaxBytes = 64 << 20

type codexChatHistoryEntry struct {
	messages []map[string]any
	bytes    int
}

type CodexChatBridgeHistoryStore struct {
	mu         sync.RWMutex
	items      map[string]codexChatHistoryEntry
	order      []string
	maxEntries int
	maxBytes   int
	totalBytes int
}

func NewCodexChatBridgeHistoryStore(maxEntries int) *CodexChatBridgeHistoryStore {
	if maxEntries <= 0 {
		maxEntries = 128
	}
	return &CodexChatBridgeHistoryStore{
		items: make(map[string]codexChatHistoryEntry), order: make([]string, 0, maxEntries),
		maxEntries: maxEntries, maxBytes: codexChatHistoryMaxBytes,
	}
}

func (s *CodexChatBridgeHistoryStore) Load(responseID string) ([]map[string]any, bool) {
	if s == nil || strings.TrimSpace(responseID) == "" {
		return nil, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.items[responseID]
	if !ok {
		return nil, false
	}
	return cloneChatMessages(entry.messages), true
}

func (s *CodexChatBridgeHistoryStore) LoadReadOnly(responseID string) ([]map[string]any, bool) {
	if s == nil || strings.TrimSpace(responseID) == "" {
		return nil, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.items[responseID]
	return entry.messages, ok
}

func (s *CodexChatBridgeHistoryStore) Store(responseID string, messages []map[string]any) {
	s.store(responseID, cloneChatMessages(messages))
}

func (s *CodexChatBridgeHistoryStore) StoreOwned(responseID string, messages []map[string]any) {
	s.store(responseID, messages)
}

func (s *CodexChatBridgeHistoryStore) store(responseID string, messages []map[string]any) {
	if s == nil || strings.TrimSpace(responseID) == "" || len(messages) == 0 {
		return
	}
	raw, err := json.Marshal(messages)
	if err != nil || len(raw) > s.maxBytes {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if previous, exists := s.items[responseID]; !exists {
		s.order = append(s.order, responseID)
	} else {
		s.totalBytes -= previous.bytes
	}
	s.items[responseID] = codexChatHistoryEntry{messages: messages, bytes: len(raw)}
	s.totalBytes += len(raw)
	for len(s.order) > s.maxEntries || s.totalBytes > s.maxBytes {
		oldest := s.order[0]
		s.order = s.order[1:]
		s.totalBytes -= s.items[oldest].bytes
		delete(s.items, oldest)
	}
}

func appendAssistantChatMessage(messages []map[string]any, content string) []map[string]any {
	return appendAssistantChatMessageFromChat(messages, map[string]any{"role": "assistant", "content": content})
}

func appendAssistantChatMessageFromChat(messages []map[string]any, assistant map[string]any) []map[string]any {
	if len(assistant) == 0 {
		return cloneChatMessages(messages)
	}
	result := cloneChatMessages(messages)
	return append(result, cloneChatMessages([]map[string]any{assistant})[0])
}

func codexPreviousResponseID(body map[string]any) string {
	return strings.TrimSpace(stringFromMap(body, "previous_response_id"))
}

func codexPreviousResponseIDFromBytes(bodyBytes []byte) string {
	return strings.TrimSpace(gjson.GetBytes(bodyBytes, "previous_response_id").String())
}
