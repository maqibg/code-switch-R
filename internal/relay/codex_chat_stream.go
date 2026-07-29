package relay

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type CodexChatSSEConverter struct {
	responseID            string
	model                 string
	createdAt             int64
	itemID                string
	responseStarted       bool
	contentStarted        bool
	contentCompleted      bool
	messageOutputIndex    int
	messageOutputIndexSet bool
	finishPending         bool
	responseDone          bool
	usage                 map[string]any
	outputText            strings.Builder
	toolCalls             map[int]*codexStreamToolCall
	toolOrder             []int
	err                   error
}

func NewCodexChatSSEConverter(model string) *CodexChatSSEConverter {
	return &CodexChatSSEConverter{
		responseID: "resp_" + uuid.NewString(),
		model:      model,
		createdAt:  time.Now().Unix(),
		itemID:     "msg_" + uuid.NewString(),
		usage:      chatUsageToResponsesUsage(nil),
	}
}

func (c *CodexChatSSEConverter) ProcessPayload(payload string) string {
	var out strings.Builder
	for _, line := range strings.Split(strings.TrimSpace(payload), "\n") {
		if converted := c.ProcessLine(line); converted != "" {
			out.WriteString(converted)
		}
	}
	return out.String()
}

func (c *CodexChatSSEConverter) ProcessLine(line string) string {
	if c.err != nil {
		return ""
	}
	line = strings.TrimSpace(line)
	if line == "" || !strings.HasPrefix(line, "data:") {
		return ""
	}
	data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
	if data == "[DONE]" {
		return c.finishResponse()
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(data), &body); err != nil {
		return ""
	}
	c.applyChunkMetadata(body)
	if usage, ok := body["usage"].(map[string]any); ok && len(usage) > 0 {
		c.usage = chatUsageToResponsesUsage(usage)
	}
	return c.convertChatDelta(body)
}

func (c *CodexChatSSEConverter) convertChatDelta(body map[string]any) string {
	delta := gjsonGetString(body, "choices.0.delta.content")
	if reasoning := gjsonGetString(body, "choices.0.delta.reasoning_content"); reasoning != "" {
		c.err = fmt.Errorf("上游 Chat 流返回 reasoning_content，无法转换为语义等价的 Responses reasoning 输出")
		return ""
	}
	finishReason := gjsonGetString(body, "choices.0.finish_reason")
	var out strings.Builder
	if delta != "" {
		c.outputText.WriteString(delta)
		c.writeStartEvents(&out)
		writeCodexSSE(&out, "response.output_text.delta", map[string]any{
			"type": "response.output_text.delta", "item_id": c.itemID,
			"output_index": c.messageOutputIndex, "content_index": 0, "delta": delta,
		})
	}
	c.writeToolCallDeltas(&out, firstChatDeltaToolCalls(body))
	if finishReason == "" && c.finishPending && hasChatUsage(body) {
		out.WriteString(c.finishResponse())
		return out.String()
	}
	if finishReason != "" {
		if c.contentStarted || len(c.toolOrder) == 0 {
			c.writeDoneEvents(&out)
		}
		c.writeToolCallDoneEvents(&out)
		if hasChatUsage(body) {
			out.WriteString(c.finishResponse())
		} else {
			c.finishPending = true
		}
	}
	return out.String()
}

func (c *CodexChatSSEConverter) Err() error {
	if c == nil {
		return fmt.Errorf("Responses SSE 转换器未初始化")
	}
	return c.err
}

func (c *CodexChatSSEConverter) ResponseID() string {
	if c == nil {
		return ""
	}
	return c.responseID
}

func (c *CodexChatSSEConverter) OutputText() string {
	if c == nil {
		return ""
	}
	return c.outputText.String()
}

func (c *CodexChatSSEConverter) AssistantChatMessage() map[string]any {
	if c == nil {
		return nil
	}
	message := map[string]any{"role": "assistant"}
	if text := c.outputText.String(); text != "" {
		message["content"] = text
	} else if len(c.toolOrder) > 0 {
		message["content"] = nil
	} else {
		message["content"] = ""
	}
	if toolCalls := c.chatToolCalls(); len(toolCalls) > 0 {
		message["tool_calls"] = toolCalls
	}
	return message
}

func (c *CodexChatSSEConverter) applyChunkMetadata(body map[string]any) {
	if id := stringFromMap(body, "id"); id != "" {
		c.responseID = id
	}
	if model := stringFromMap(body, "model"); model != "" {
		c.model = model
	}
	if created := int64FromMap(body, "created"); created > 0 {
		c.createdAt = created
	}
}

func (c *CodexChatSSEConverter) writeStartEvents(out *strings.Builder) {
	if c.contentStarted {
		return
	}
	c.writeResponseStartEvents(out)
	if !c.messageOutputIndexSet {
		c.messageOutputIndex = c.nextOutputIndex()
		c.messageOutputIndexSet = true
	}
	writeCodexSSE(out, "response.output_item.added", map[string]any{
		"type": "response.output_item.added", "output_index": c.messageOutputIndex,
		"item": map[string]any{"id": c.itemID, "type": "message", "status": "in_progress", "role": "assistant", "content": []any{}},
	})
	writeCodexSSE(out, "response.content_part.added", map[string]any{
		"type": "response.content_part.added", "item_id": c.itemID, "output_index": c.messageOutputIndex, "content_index": 0,
		"part": map[string]any{"type": "output_text", "text": "", "annotations": []any{}},
	})
	c.contentStarted = true
}

func (c *CodexChatSSEConverter) writeResponseStartEvents(out *strings.Builder) {
	if c.responseStarted {
		return
	}
	writeCodexSSE(out, "response.created", map[string]any{"type": "response.created", "response": c.responseSnapshot("in_progress")})
	writeCodexSSE(out, "response.in_progress", map[string]any{"type": "response.in_progress", "response": c.responseSnapshot("in_progress")})
	c.responseStarted = true
}

func (c *CodexChatSSEConverter) writeDoneEvents(out *strings.Builder) {
	if !c.contentStarted {
		c.writeStartEvents(out)
	}
	if c.contentCompleted {
		return
	}
	text := c.outputText.String()
	writeCodexSSE(out, "response.output_text.done", map[string]any{
		"type": "response.output_text.done", "item_id": c.itemID, "output_index": c.messageOutputIndex, "content_index": 0, "text": text,
	})
	writeCodexSSE(out, "response.content_part.done", map[string]any{
		"type": "response.content_part.done", "item_id": c.itemID, "output_index": c.messageOutputIndex, "content_index": 0,
		"part": codexOutputTextContentBlock(text),
	})
	writeCodexSSE(out, "response.output_item.done", map[string]any{
		"type": "response.output_item.done", "output_index": c.messageOutputIndex, "item": c.messageOutputItem("completed"),
	})
	c.contentCompleted = true
}

func (c *CodexChatSSEConverter) finishResponse() string {
	if c.responseDone {
		return ""
	}
	var out strings.Builder
	if c.contentStarted || len(c.toolOrder) == 0 {
		c.writeDoneEvents(&out)
	}
	c.writeToolCallDoneEvents(&out)
	writeCodexSSE(&out, "response.completed", map[string]any{"type": "response.completed", "response": c.responseSnapshot("completed")})
	c.finishPending = false
	c.responseDone = true
	return out.String()
}

func hasChatUsage(body map[string]any) bool {
	usage, ok := body["usage"].(map[string]any)
	return ok && len(usage) > 0
}

func (c *CodexChatSSEConverter) nextOutputIndex() int {
	next := 0
	if c.messageOutputIndexSet {
		next = c.messageOutputIndex + 1
	}
	for _, index := range c.toolOrder {
		tool := c.toolCalls[index]
		if tool != nil && tool.started && tool.outputIndex >= next {
			next = tool.outputIndex + 1
		}
	}
	return next
}

func (c *CodexChatSSEConverter) responseSnapshot(status string) map[string]any {
	return map[string]any{
		"id": c.responseID, "object": "response", "created_at": c.createdAt,
		"status": status, "model": c.model, "output": c.snapshotOutputItems(), "usage": c.usage,
	}
}

func (c *CodexChatSSEConverter) snapshotOutputItems() []any {
	output := make([]any, 0, len(c.toolOrder)+1)
	if c.contentStarted || c.contentCompleted {
		status := "in_progress"
		if c.contentCompleted {
			status = "completed"
		}
		output = append(output, c.messageOutputItem(status))
	}
	for _, index := range c.toolOrder {
		tool := c.toolCalls[index]
		if tool == nil || !tool.started {
			continue
		}
		status := "in_progress"
		if tool.done {
			status = "completed"
		}
		output = append(output, c.toolCallItem(tool, status))
	}
	return output
}

func (c *CodexChatSSEConverter) messageOutputItem(status string) map[string]any {
	return map[string]any{
		"id": c.itemID, "type": "message", "status": status, "role": "assistant",
		"content": []any{codexOutputTextContentBlock(c.outputText.String())},
	}
}

func codexOutputTextContentBlock(text string) map[string]any {
	return map[string]any{"type": "output_text", "text": text, "annotations": []any{}}
}

func writeCodexSSE(out *strings.Builder, event string, payload map[string]any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	out.WriteString("event: ")
	out.WriteString(event)
	out.WriteString("\ndata: ")
	out.Write(data)
	out.WriteString("\n\n")
}

func gjsonGetString(body map[string]any, path string) string {
	choices, ok := body["choices"].([]any)
	if !ok || len(choices) == 0 {
		return ""
	}
	choice, ok := choices[0].(map[string]any)
	if !ok {
		return ""
	}
	switch path {
	case "choices.0.finish_reason":
		value, _ := choice["finish_reason"].(string)
		return value
	case "choices.0.delta.content", "choices.0.delta.reasoning_content":
		delta, _ := choice["delta"].(map[string]any)
		key := strings.TrimPrefix(path, "choices.0.delta.")
		value, _ := delta[key].(string)
		return value
	default:
		return ""
	}
}
