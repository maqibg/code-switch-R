package services

import (
	"strings"

	"github.com/google/uuid"
)

type codexStreamToolCall struct {
	index       int
	outputIndex int
	itemID      string
	callID      string
	name        string
	arguments   strings.Builder
	started     bool
	done        bool
}

func firstChatDeltaToolCalls(body map[string]any) []any {
	choices, _ := body["choices"].([]any)
	if len(choices) == 0 {
		return nil
	}
	choice, _ := choices[0].(map[string]any)
	delta, _ := choice["delta"].(map[string]any)
	toolCalls, _ := delta["tool_calls"].([]any)
	return toolCalls
}

func (c *CodexChatSSEConverter) writeToolCallDeltas(out *strings.Builder, deltas []any) {
	for _, item := range deltas {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		tool := c.streamToolCall(int(int64FromAny(obj["index"])))
		if callID := stringFromMap(obj, "id"); callID != "" {
			tool.callID = callID
		}
		function, _ := obj["function"].(map[string]any)
		if name := stringFromMap(function, "name"); name != "" {
			tool.name = name
		}
		c.writeToolCallStart(out, tool)
		if arguments := stringFromMap(function, "arguments"); arguments != "" {
			tool.arguments.WriteString(arguments)
			writeCodexSSE(out, "response.function_call_arguments.delta", map[string]any{
				"type": "response.function_call_arguments.delta", "item_id": tool.itemID,
				"output_index": tool.outputIndex, "delta": arguments,
			})
		}
	}
}

func (c *CodexChatSSEConverter) streamToolCall(index int) *codexStreamToolCall {
	if c.toolCalls == nil {
		c.toolCalls = make(map[int]*codexStreamToolCall)
	}
	if tool := c.toolCalls[index]; tool != nil {
		return tool
	}
	tool := &codexStreamToolCall{
		index:  index,
		itemID: "fc_" + uuid.NewString(),
		callID: "call_" + uuid.NewString(),
	}
	c.toolCalls[index] = tool
	c.toolOrder = append(c.toolOrder, index)
	return tool
}

func (c *CodexChatSSEConverter) writeToolCallStart(out *strings.Builder, tool *codexStreamToolCall) {
	if tool == nil || tool.started {
		return
	}
	c.writeResponseStartEvents(out)
	tool.outputIndex = c.nextOutputIndex()
	writeCodexSSE(out, "response.output_item.added", map[string]any{
		"type":         "response.output_item.added",
		"output_index": tool.outputIndex,
		"item":         c.toolCallItem(tool, "in_progress"),
	})
	tool.started = true
}

func (c *CodexChatSSEConverter) writeToolCallDoneEvents(out *strings.Builder) {
	for _, index := range c.toolOrder {
		tool := c.toolCalls[index]
		if tool == nil || tool.done {
			continue
		}
		c.writeToolCallStart(out, tool)
		writeCodexSSE(out, "response.function_call_arguments.done", map[string]any{
			"type": "response.function_call_arguments.done", "item_id": tool.itemID,
			"output_index": tool.outputIndex, "arguments": tool.arguments.String(),
		})
		writeCodexSSE(out, "response.output_item.done", map[string]any{
			"type":         "response.output_item.done",
			"output_index": tool.outputIndex,
			"item":         c.toolCallItem(tool, "completed"),
		})
		tool.done = true
	}
}

func (c *CodexChatSSEConverter) toolCallItem(tool *codexStreamToolCall, status string) map[string]any {
	return map[string]any{
		"id":        tool.itemID,
		"type":      "function_call",
		"status":    status,
		"call_id":   tool.callID,
		"name":      tool.name,
		"arguments": tool.arguments.String(),
	}
}

func (c *CodexChatSSEConverter) chatToolCalls() []any {
	if c == nil || len(c.toolOrder) == 0 {
		return nil
	}
	items := make([]any, 0, len(c.toolOrder))
	for _, index := range c.toolOrder {
		tool := c.toolCalls[index]
		if tool == nil || !tool.started {
			continue
		}
		items = append(items, map[string]any{
			"id":   tool.callID,
			"type": "function",
			"function": map[string]any{
				"name":      tool.name,
				"arguments": tool.arguments.String(),
			},
		})
	}
	return items
}
