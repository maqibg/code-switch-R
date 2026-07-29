package relay

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/daodao97/xgo/xrequest"
	"github.com/tidwall/sjson"
)

func (state *codexFoldState) nextSeq() int64 {
	current := state.seq
	state.seq++
	return current
}

func writeCodexSequencedEvent(w http.ResponseWriter, event map[string]any, state *codexFoldState) {
	event["sequence_number"] = state.nextSeq()
	writeCodexSSEEvent(w, event)
	flushWriter(w)
}

func writeCodexSSEEvent(w http.ResponseWriter, event map[string]any) {
	eventType := stringField(event, "type")
	if eventType == "" {
		eventType = "message"
	}
	data, _ := json.Marshal(event)
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, data)
}

func writeCodexSequencedRawEvent(w http.ResponseWriter, data []byte, eventType string, outputIndex int, state *codexFoldState) {
	updated, err := sjson.SetBytes(data, "output_index", outputIndex)
	if err != nil {
		return
	}
	updated, err = sjson.SetBytes(updated, "sequence_number", state.nextSeq())
	if err != nil {
		return
	}
	if eventType == "" {
		eventType = "message"
	}
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, updated)
	flushWriter(w)
}

func writeCodexSSEDone(w http.ResponseWriter) {
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
}

func isEventStream(resp *xrequest.Response) bool {
	return resp != nil && resp.RawResponse != nil && strings.Contains(strings.ToLower(resp.RawResponse.Header.Get("Content-Type")), "text/event-stream")
}

func flushWriter(w http.ResponseWriter) {
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func deleteHeaderCaseInsensitive(headers map[string]string, name string) {
	for key := range headers {
		if strings.EqualFold(key, name) {
			delete(headers, key)
		}
	}
}

func outputItemType(event map[string]any) string {
	item := mapField(event, "item")
	return stringField(item, "type")
}

func terminalUsage(terminal map[string]any) map[string]any {
	return mapField(mapField(terminal, "response"), "usage")
}

func lastReasoningHasEncrypted(items []any) bool {
	if len(items) == 0 {
		return false
	}
	item, _ := items[len(items)-1].(map[string]any)
	encrypted, _ := item["encrypted_content"].(string)
	return encrypted != ""
}

func isCodexTerminalEvent(eventType string) bool {
	return eventType == "response.completed" || eventType == "response.incomplete" || eventType == "response.failed"
}

func codexCommentaryMarker(marker string) any {
	return map[string]any{
		"type": "message",
		"role": "assistant",
		"content": []any{
			map[string]any{"type": "output_text", "text": marker},
		},
		"phase": "commentary",
	}
}

func setOutputIndex(event map[string]any, index int) {
	if _, exists := event["output_index"]; exists {
		event["output_index"] = index
	}
}

func stringField(value map[string]any, key string) string {
	if value == nil {
		return ""
	}
	text, _ := value[key].(string)
	return text
}

func mapField(value map[string]any, key string) map[string]any {
	if value == nil {
		return nil
	}
	field, _ := value[key].(map[string]any)
	return field
}

func nestedInt64(root map[string]any, path ...string) int64 {
	var current any = root
	for _, key := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return 0
		}
		current = object[key]
	}
	switch value := current.(type) {
	case float64:
		return int64(value)
	case int64:
		return value
	case int:
		return int64(value)
	case json.Number:
		n, _ := value.Int64()
		return n
	default:
		return 0
	}
}

func decodeJSONMap(data []byte) (map[string]any, error) {
	var body map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&body); err != nil {
		return nil, err
	}
	if body == nil {
		body = map[string]any{}
	}
	return body, nil
}

func cloneJSONMap(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	cloned, _ := cloneJSONValue(value).(map[string]any)
	if cloned == nil {
		return map[string]any{}
	}
	return cloned
}

func cloneJSONValue(value any) any {
	data, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var out any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&out); err != nil {
		return value
	}
	return out
}
