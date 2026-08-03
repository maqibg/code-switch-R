package relay

import (
	"codeswitch/services"
)

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type geminiMatrixSSESource struct {
	metadata  matrixChatMetadata
	text      string
	toolArgs  map[int]string
	toolNames map[int]string
	finished  bool
	err       error
}

func newGeminiMatrixSSESource(model string) *geminiMatrixSSESource {
	return &geminiMatrixSSESource{
		metadata:  newMatrixChatMetadata(model),
		toolArgs:  make(map[int]string),
		toolNames: make(map[int]string),
	}
}

func (s *geminiMatrixSSESource) ProcessLine(line string) string {
	if s.finished || s.err != nil {
		return ""
	}
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "event:") || isSSEControlLine(trimmed) {
		return ""
	}
	if !strings.HasPrefix(trimmed, "data:") {
		s.err = fmt.Errorf("Gemini 流包含无法识别的行: %s", truncateRelayError(trimmed))
		return ""
	}
	payload := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
	if payload == "[DONE]" {
		s.finished = true
		return "data: [DONE]\n\n"
	}
	var response map[string]any
	if err := json.Unmarshal([]byte(payload), &response); err != nil {
		s.err = fmt.Errorf("Gemini 流事件不是合法 JSON: %w", err)
		return ""
	}
	if _, exists := response["error"]; exists {
		s.err = fmt.Errorf("Gemini 流返回错误: %s", truncateRelayError(payload))
		return ""
	}
	if model := stringFromMap(response, "modelVersion"); model != "" {
		s.metadata.apply("", model, 0)
	}
	var out strings.Builder
	candidates, _ := response["candidates"].([]any)
	if len(candidates) > 0 {
		candidate, _ := candidates[0].(map[string]any)
		content, _ := candidate["content"].(map[string]any)
		parts, _ := content["parts"].([]any)
		for index, raw := range parts {
			part, _ := raw.(map[string]any)
			if text := s.textDelta(stringFromMap(part, "text")); text != "" {
				out.WriteString(s.metadata.chunk(map[string]any{"content": text}, nil, nil))
			}
			if call, ok := part["functionCall"].(map[string]any); ok {
				name := stringFromMap(call, "name")
				if name == "" {
					s.err = fmt.Errorf("Gemini 流 functionCall 缺少 name")
					return ""
				}
				callID := stringFromMap(call, "id")
				if callID == "" {
					callID = fmt.Sprintf("gemini_call_%d", index)
				}
				fullArguments := mustMarshalString(call["args"])
				previous := s.toolArgs[index]
				s.toolArgs[index] = fullArguments
				s.toolNames[index] = name
				delta := streamDelta(previous, fullArguments)
				callDelta := map[string]any{"index": index, "id": callID, "type": "function", "function": map[string]any{"name": name}}
				if delta != "" {
					callDelta["function"] = map[string]any{"name": name, "arguments": delta}
				}
				if signature := stringFromMap(part, "thoughtSignature"); signature != "" {
					callDelta["x_gemini_thought_signature"] = signature
				}
				out.WriteString(s.metadata.chunk(map[string]any{"tool_calls": []any{callDelta}}, nil, nil))
			}
		}
		finish := strings.ToLower(stringFromMap(candidate, "finishReason"))
		if finish != "" {
			out.WriteString(s.finish(finish, response["usageMetadata"]))
		}
	} else if response["usageMetadata"] != nil {
		out.WriteString(s.metadata.chunk(map[string]any{}, nil, geminiUsageToChatUsage(response["usageMetadata"])))
	}
	return out.String()
}

func (s *geminiMatrixSSESource) textDelta(full string) string {
	if full == "" {
		return ""
	}
	delta := streamDelta(s.text, full)
	if strings.HasPrefix(full, s.text) {
		s.text = full
	} else if !strings.HasPrefix(s.text, full) {
		s.text += delta
	}
	return delta
}

func (s *geminiMatrixSSESource) finish(reason string, rawUsage any) string {
	if s.finished {
		return ""
	}
	finishReason := "stop"
	switch reason {
	case "max_tokens", "length":
		finishReason = "length"
	case "safety", "recitation":
		finishReason = "content_filter"
	case "malformed_function_call":
		finishReason = "tool_calls"
	}
	s.finished = true
	return s.metadata.chunk(map[string]any{}, finishReason, geminiUsageToChatUsage(rawUsage)) + "data: [DONE]\n\n"
}

func (s *geminiMatrixSSESource) Err() error { return s.err }

type geminiMatrixSSETarget struct {
	finished       bool
	err            error
	toolArguments  map[int]string
	toolNames      map[int]string
	toolIDs        map[int]string
	toolSignatures map[int]string
	emittedTools   map[int]bool
}

func newGeminiMatrixSSETarget() *geminiMatrixSSETarget {
	return &geminiMatrixSSETarget{
		toolArguments:  make(map[int]string),
		toolNames:      make(map[int]string),
		toolIDs:        make(map[int]string),
		toolSignatures: make(map[int]string),
		emittedTools:   make(map[int]bool),
	}
}

func (t *geminiMatrixSSETarget) ProcessPayload(payload string) string {
	if t.finished || t.err != nil {
		return ""
	}
	var out strings.Builder
	for _, line := range strings.Split(strings.TrimSpace(payload), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "event:") || isSSEControlLine(trimmed) {
			continue
		}
		if !strings.HasPrefix(trimmed, "data:") {
			t.err = fmt.Errorf("Chat 中间流包含无法转换到 Gemini 的行: %s", truncateRelayError(trimmed))
			break
		}
		data := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
		if data == "[DONE]" {
			t.finished = true
			continue
		}
		var chunk map[string]any
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			t.err = fmt.Errorf("Chat 中间流不是合法 JSON: %w", err)
			break
		}
		converted, err := t.chatChunkToGemini(chunk)
		if err != nil {
			t.err = err
			break
		}
		if converted != "" {
			out.WriteString(converted)
		}
	}
	return out.String()
}

func (t *geminiMatrixSSETarget) chatChunkToGemini(chunk map[string]any) (string, error) {
	result := map[string]any{}
	candidates := make([]any, 0, 1)
	choices, _ := chunk["choices"].([]any)
	if len(choices) > 0 {
		choice, _ := choices[0].(map[string]any)
		delta, _ := choice["delta"].(map[string]any)
		parts := make([]any, 0)
		if text := stringFromMap(delta, "content"); text != "" {
			parts = append(parts, map[string]any{"text": text})
		}
		if calls, ok := delta["tool_calls"].([]any); ok {
			for _, raw := range calls {
				call, _ := raw.(map[string]any)
				index := int(int64FromAny(call["index"]))
				function, _ := call["function"].(map[string]any)
				if name := stringFromMap(function, "name"); name != "" {
					t.toolNames[index] = name
				}
				if id := stringFromMap(call, "id"); id != "" {
					t.toolIDs[index] = id
				}
				if signature := stringFromMap(call, "x_gemini_thought_signature"); signature != "" {
					t.toolSignatures[index] = signature
				}
				fragment := stringFromMap(function, "arguments")
				if fragment != "" {
					t.toolArguments[index] = appendStreamToolArguments(t.toolArguments[index], fragment)
				}
				part, complete, err := t.buildGeminiToolPart(index, false)
				if err != nil {
					return "", err
				}
				if complete {
					parts = append(parts, part)
				}
			}
		}
		finish := strings.ToUpper(stringFromMap(choice, "finish_reason"))
		if finish != "" {
			flushed, err := t.flushGeminiToolParts()
			if err != nil {
				return "", err
			}
			parts = append(parts, flushed...)
		}
		candidate := map[string]any{}
		if len(parts) > 0 {
			candidate["content"] = map[string]any{"role": "model", "parts": parts}
		}
		if finish != "" {
			candidate["finishReason"] = finish
		}
		if len(candidate) > 0 {
			candidates = append(candidates, candidate)
		}
	}
	if len(candidates) > 0 {
		result["candidates"] = candidates
	}
	if usage, ok := chunk["usage"].(map[string]any); ok {
		result["usageMetadata"] = chatUsageToGeminiUsage(usage)
	}
	if len(result) == 0 {
		return "", nil
	}
	return string(encodeSSEData(result)), nil
}

func (t *geminiMatrixSSETarget) buildGeminiToolPart(index int, force bool) (map[string]any, bool, error) {
	name := strings.TrimSpace(t.toolNames[index])
	if name == "" {
		return nil, false, nil
	}
	rawArguments := strings.TrimSpace(t.toolArguments[index])
	if rawArguments == "" {
		if !force {
			return nil, false, nil
		}
		rawArguments = "{}"
	}
	args := map[string]any{}
	if err := json.Unmarshal([]byte(rawArguments), &args); err != nil {
		if force {
			return nil, false, services.NewClientRequestRejectedError("流式 Gemini tool arguments 结束时仍不是完整 JSON")
		}
		return nil, false, nil
	}
	if t.emittedTools[index] && !force {
		return nil, false, nil
	}
	call := map[string]any{"name": name, "args": args}
	if id := strings.TrimSpace(t.toolIDs[index]); id != "" {
		call["id"] = id
	}
	part := map[string]any{"functionCall": call}
	if signature := strings.TrimSpace(t.toolSignatures[index]); signature != "" {
		part["thoughtSignature"] = signature
	}
	t.emittedTools[index] = true
	return part, true, nil
}

func (t *geminiMatrixSSETarget) flushGeminiToolParts() ([]any, error) {
	indices := make([]int, 0, len(t.toolNames))
	for index := range t.toolNames {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	parts := make([]any, 0, len(indices))
	for _, index := range indices {
		part, complete, err := t.buildGeminiToolPart(index, true)
		if err != nil {
			return nil, err
		}
		if complete {
			parts = append(parts, part)
		}
	}
	return parts, nil
}

func appendStreamToolArguments(previous, fragment string) string {
	if previous == "" {
		return fragment
	}
	if strings.HasPrefix(fragment, previous) {
		return fragment
	}
	return previous + fragment
}

func (t *geminiMatrixSSETarget) Err() error { return t.err }

func streamDelta(previous, full string) string {
	if previous == "" {
		return full
	}
	if strings.HasPrefix(full, previous) {
		return full[len(previous):]
	}
	if strings.HasPrefix(previous, full) {
		return ""
	}
	return full
}
