package services

import (
	"encoding/json"
	"testing"
)

func TestCodexContinueTruncationPattern(t *testing.T) {
	if !isCodexReasoningTruncated(516, 518) {
		t.Fatal("expected 516 to match default truncation pattern")
	}
	if !isCodexReasoningTruncated(1034, 518) {
		t.Fatal("expected 1034 to match default truncation pattern")
	}
	for _, tokens := range []int64{0, 515, 517, 518, 1033} {
		if isCodexReasoningTruncated(tokens, 518) {
			t.Fatalf("expected %d to miss default truncation pattern", tokens)
		}
	}
}

func TestPrepareCodexInitialPayloadMergesEncryptedInclude(t *testing.T) {
	body := []byte(`{"model":"gpt-5","stream":true,"include":["message.output_text.logprobs"]}`)
	out, decoded, err := prepareCodexInitialPayload(body)
	if err != nil {
		t.Fatal(err)
	}
	if !codexContainsString(decoded["include"].([]any), codexContinueEncryptedInclude) {
		t.Fatal("expected decoded include to contain encrypted reasoning")
	}
	var persisted map[string]any
	if err := json.Unmarshal(out, &persisted); err != nil {
		t.Fatal(err)
	}
	include := persisted["include"].([]any)
	if len(include) != 2 {
		t.Fatalf("expected exactly 2 include entries, got %d", len(include))
	}
}

func TestBuildCodexContinuationPayloadAppendsReplayTail(t *testing.T) {
	base, err := decodeJSONMap([]byte(`{
		"model":"gpt-5",
		"stream":true,
		"previous_response_id":"resp_1",
		"input":[{"role":"user","content":"hello"}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	replay := []any{
		map[string]any{"type": "reasoning", "encrypted_content": "sealed"},
		codexCommentaryMarker(codexContinueMarker),
	}
	out, err := buildCodexContinuationPayload(base, replay)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatal(err)
	}
	if _, exists := payload["previous_response_id"]; exists {
		t.Fatal("expected previous_response_id to be removed")
	}
	if payload["stream"] != true {
		t.Fatal("expected stream to be true")
	}
	input := payload["input"].([]any)
	if len(input) != 3 {
		t.Fatalf("expected original input plus 2 replay entries, got %d", len(input))
	}
	if !codexContainsString(payload["include"].([]any), codexContinueEncryptedInclude) {
		t.Fatal("expected include to contain encrypted reasoning")
	}
}

func TestShouldUseCodexContinueGate(t *testing.T) {
	body := []byte(`{"stream":true,"model":"gpt-5"}`)
	provider := Provider{CodexReasoningContinueEnabled: true}
	if !shouldUseCodexContinue(provider, "/responses", body, true) {
		t.Fatal("expected native streaming codex provider to enable continue")
	}
	provider.UpstreamProtocol = "openai_chat"
	if shouldUseCodexContinue(provider, "/responses", body, true) {
		t.Fatal("expected openai_chat provider to disable continue")
	}
	provider.UpstreamProtocol = "auto"
	if shouldUseCodexContinue(provider, "/chat/completions", body, true) {
		t.Fatal("expected non-responses endpoint to disable continue")
	}
	if shouldUseCodexContinue(provider, "/responses", []byte(`{"stream":true,"reasoning":false}`), true) {
		t.Fatal("expected reasoning=false to disable continue")
	}
}

func TestCodexContinueLogEnabledDefaults(t *testing.T) {
	if !codexContinueLogEnabled(Provider{}) {
		t.Fatal("expected missing log setting to keep console logs enabled")
	}
	disabled := false
	if codexContinueLogEnabled(Provider{CodexReasoningContinueLogEnabled: &disabled}) {
		t.Fatal("expected explicit false to disable console logs")
	}
	enabled := true
	if !codexContinueLogEnabled(Provider{CodexReasoningContinueLogEnabled: &enabled}) {
		t.Fatal("expected explicit true to enable console logs")
	}
}

func TestCodexSSEParserHandlesSplitChunksAndDone(t *testing.T) {
	parser := &codexSSEParser{}
	part1 := []byte("event: response.output_text.delta\ndata: {\"type\":\"response.output")
	part2 := []byte("_text.delta\",\"delta\":\"hi\"}\n\ndata: [DONE]\n\n")
	if frames := parser.push(part1); len(frames) != 0 {
		t.Fatalf("expected first split chunk to produce no frames, got %d", len(frames))
	}
	frames := parser.push(part2)
	if len(frames) != 2 {
		t.Fatalf("expected 2 frames, got %d", len(frames))
	}
	if frames[0].done || frames[0].event["type"] != "response.output_text.delta" {
		t.Fatalf("unexpected first frame: %#v", frames[0])
	}
	if !frames[1].done {
		t.Fatal("expected second frame to be done")
	}
}
