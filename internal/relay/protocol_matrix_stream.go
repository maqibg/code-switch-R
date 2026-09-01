package relay

import (
	"codeswitch/services"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	relayprotocol "codeswitch/services/protocol"

	"github.com/google/uuid"
)

// ProtocolMatrixSSEConverter converts every transformed stream through an
// OpenAI Chat-compatible incremental representation. This keeps one streaming
// implementation per wire protocol instead of one implementation per matrix edge.
type ProtocolMatrixSSEConverter struct {
	source matrixSSESource
	target matrixSSETarget
}

type matrixSSESource interface {
	ProcessLine(string) string
	Err() error
}

type matrixSSETarget interface {
	ProcessPayload(string) string
	Err() error
}

func NewProtocolMatrixSSEConverter(source, target relayprotocol.Protocol, model string) *ProtocolMatrixSSEConverter {
	converter := &ProtocolMatrixSSEConverter{}
	switch source {
	case relayprotocol.OpenAIChat:
		converter.source = &chatMatrixSSESource{}
	case relayprotocol.AnthropicMessages:
		converter.source = newAnthropicMatrixSSESource(model)
	case relayprotocol.OpenAIResponses:
		converter.source = newResponsesMatrixSSESource(model)
	case relayprotocol.GeminiNative:
		converter.source = newGeminiMatrixSSESource(model)
	default:
		converter.source = &unsupportedMatrixSSESource{err: fmt.Errorf("不支持的流式源协议: %s", source)}
	}
	switch target {
	case relayprotocol.OpenAIChat:
		converter.target = &chatMatrixSSETarget{}
	case relayprotocol.OpenAIResponses:
		converter.target = &responsesMatrixSSETarget{converter: NewCodexChatSSEConverter(model)}
	case relayprotocol.AnthropicMessages:
		converter.target = newAnthropicMatrixSSETarget(model)
	case relayprotocol.GeminiNative:
		converter.target = newGeminiMatrixSSETarget()
	default:
		converter.target = &unsupportedMatrixSSETarget{err: fmt.Errorf("不支持的流式目标协议: %s", target)}
	}
	return converter
}

func (c *ProtocolMatrixSSEConverter) ProcessLine(line string) string {
	if c == nil || c.source == nil || c.target == nil || c.Err() != nil {
		return ""
	}
	canonical := c.source.ProcessLine(line)
	if canonical == "" || c.source.Err() != nil {
		return ""
	}
	return c.target.ProcessPayload(canonical)
}

func (c *ProtocolMatrixSSEConverter) Err() error {
	if c == nil {
		return fmt.Errorf("协议流转换器未初始化")
	}
	if c.source != nil && c.source.Err() != nil {
		return c.source.Err()
	}
	if c.target != nil {
		return c.target.Err()
	}
	return nil
}

func (c *ProtocolMatrixSSEConverter) ResponseID() string {
	if c == nil {
		return ""
	}
	if target, ok := c.target.(*responsesMatrixSSETarget); ok {
		return target.converter.ResponseID()
	}
	return ""
}

func (c *ProtocolMatrixSSEConverter) AssistantChatMessage() map[string]any {
	if c == nil {
		return nil
	}
	if target, ok := c.target.(*responsesMatrixSSETarget); ok {
		return target.converter.AssistantChatMessage()
	}
	return nil
}

func protocolMatrixHook(converter *ProtocolMatrixSSEConverter, target relayprotocol.Protocol, usage *services.RequestLog) func([]byte) (bool, []byte) {
	return func(data []byte) (bool, []byte) {
		converted := converter.ProcessLine(string(data))
		if converted == "" {
			return false, nil
		}
		parseEventPayload(converted, protocolUsageParser(target), usage)
		return true, []byte(converted)
	}
}

func protocolUsageParser(protocol relayprotocol.Protocol) func(string, *services.RequestLog) {
	switch protocol {
	case relayprotocol.OpenAIChat:
		return OpenAIChatParseTokenUsageFromResponse
	case relayprotocol.OpenAIResponses:
		return CodexParseTokenUsageFromResponse
	case relayprotocol.GeminiNative:
		return GeminiParseTokenUsageFromResponse
	default:
		return ClaudeCodeParseTokenUsageFromResponse
	}
}

type unsupportedMatrixSSESource struct{ err error }

func (s *unsupportedMatrixSSESource) ProcessLine(string) string { return "" }
func (s *unsupportedMatrixSSESource) Err() error                { return s.err }

type unsupportedMatrixSSETarget struct{ err error }

func (t *unsupportedMatrixSSETarget) ProcessPayload(string) string { return "" }
func (t *unsupportedMatrixSSETarget) Err() error                   { return t.err }

type chatMatrixSSESource struct{ err error }

func (s *chatMatrixSSESource) ProcessLine(line string) string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "event:") || isSSEControlLine(trimmed) {
		return ""
	}
	if !strings.HasPrefix(trimmed, "data:") {
		s.err = fmt.Errorf("OpenAI Chat 流包含无法识别的行: %s", truncateRelayError(trimmed))
		return ""
	}
	payload := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
	if payload != "[DONE]" {
		var chunk map[string]any
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			s.err = fmt.Errorf("OpenAI Chat 流事件不是合法 JSON: %w", err)
			return ""
		}
		if chatChunkContainsReasoning(chunk) {
			s.err = fmt.Errorf("上游 Chat 流返回 reasoning_content，跨协议转换无法保证语义完整")
			return ""
		}
	}
	return "data: " + payload + "\n\n"
}

func (s *chatMatrixSSESource) Err() error { return s.err }

func chatChunkContainsReasoning(chunk map[string]any) bool {
	choices, _ := chunk["choices"].([]any)
	if len(choices) == 0 {
		return false
	}
	choice, _ := choices[0].(map[string]any)
	delta, _ := choice["delta"].(map[string]any)
	_, exists := delta["reasoning_content"]
	return exists
}

func isSSEControlLine(line string) bool {
	return strings.HasPrefix(line, ":") || strings.HasPrefix(line, "id:") || strings.HasPrefix(line, "retry:")
}

type chatMatrixSSETarget struct{ err error }

func (t *chatMatrixSSETarget) ProcessPayload(payload string) string { return payload }
func (t *chatMatrixSSETarget) Err() error                           { return t.err }

type responsesMatrixSSETarget struct {
	converter *CodexChatSSEConverter
}

func (t *responsesMatrixSSETarget) ProcessPayload(payload string) string {
	return t.converter.ProcessPayload(payload)
}

func (t *responsesMatrixSSETarget) Err() error { return t.converter.Err() }

type matrixChatMetadata struct {
	id      string
	model   string
	created int64
}

func newMatrixChatMetadata(model string) matrixChatMetadata {
	return matrixChatMetadata{
		id:      "chatcmpl_" + uuid.NewString(),
		model:   model,
		created: time.Now().Unix(),
	}
}

func (m *matrixChatMetadata) apply(id, model string, created int64) {
	if id != "" {
		m.id = id
	}
	if model != "" {
		m.model = model
	}
	if created > 0 {
		m.created = created
	}
}

func (m matrixChatMetadata) chunk(delta map[string]any, finishReason any, usage map[string]any) string {
	chunk := map[string]any{
		"id": m.id, "object": "chat.completion.chunk", "created": m.created, "model": m.model,
		"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finishReason}},
	}
	if len(usage) > 0 {
		chunk["usage"] = usage
	}
	return string(encodeSSEData(chunk))
}

type anthropicMatrixSSESource struct {
	eventName          string
	metadata           matrixChatMetadata
	inputTokens        int64
	inputKnown         bool
	outputTokens       int64
	outputKnown        bool
	cacheRead          int64
	cacheReadKnown     bool
	cacheCreation      int64
	cacheCreationKnown bool
	stopReason         string
	toolIndexes        map[int]int
	nextToolIndex      int
	hasTools           bool
	started            bool
	stopped            bool
	err                error
}

func newAnthropicMatrixSSESource(model string) *anthropicMatrixSSESource {
	return &anthropicMatrixSSESource{
		metadata:    newMatrixChatMetadata(model),
		toolIndexes: make(map[int]int),
	}
}

func (s *anthropicMatrixSSESource) ProcessLine(line string) string {
	if s.stopped || s.err != nil {
		return ""
	}
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return ""
	}
	if isSSEControlLine(trimmed) {
		return ""
	}
	if strings.HasPrefix(trimmed, "event:") {
		s.eventName = strings.TrimSpace(strings.TrimPrefix(trimmed, "event:"))
		return ""
	}
	if !strings.HasPrefix(trimmed, "data:") {
		s.err = fmt.Errorf("Anthropic 流包含无法识别的行: %s", truncateRelayError(trimmed))
		return ""
	}
	payload := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
	var event map[string]any
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		s.err = fmt.Errorf("Anthropic 流事件不是合法 JSON: %w", err)
		return ""
	}
	eventType := stringFromMap(event, "type")
	if eventType == "" {
		eventType = s.eventName
	}
	s.eventName = ""
	switch eventType {
	case "ping":
		return ""
	case "error":
		s.err = fmt.Errorf("Anthropic 流返回错误: %s", truncateRelayError(payload))
		return ""
	case "message_start":
		message, _ := event["message"].(map[string]any)
		s.metadata.apply(stringFromMap(message, "id"), stringFromMap(message, "model"), 0)
		s.captureUsage(message["usage"])
		if s.started {
			return ""
		}
		s.started = true
		return s.metadata.chunk(map[string]any{"role": "assistant", "content": ""}, nil, nil)
	case "content_block_start":
		return s.startContentBlock(event)
	case "content_block_delta":
		return s.contentBlockDelta(event)
	case "content_block_stop":
		return ""
	case "message_delta":
		delta, _ := event["delta"].(map[string]any)
		s.stopReason = stringFromMap(delta, "stop_reason")
		s.captureUsage(event["usage"])
		return ""
	case "message_stop":
		return s.finish()
	default:
		s.err = fmt.Errorf("不支持的 Anthropic 流事件: %s", eventType)
		return ""
	}
}

func (s *anthropicMatrixSSESource) startContentBlock(event map[string]any) string {
	block, _ := event["content_block"].(map[string]any)
	blockType := stringFromMap(block, "type")
	switch blockType {
	case "text":
		if text := stringFromMap(block, "text"); text != "" {
			return s.metadata.chunk(map[string]any{"content": text}, nil, nil)
		}
		return ""
	case "tool_use":
		blockIndex := int(int64FromAny(event["index"]))
		toolIndex := s.nextToolIndex
		s.nextToolIndex++
		s.toolIndexes[blockIndex] = toolIndex
		s.hasTools = true
		tool := map[string]any{
			"index": toolIndex, "id": block["id"], "type": "function",
			"function": map[string]any{"name": block["name"], "arguments": ""},
		}
		return s.metadata.chunk(map[string]any{"tool_calls": []any{tool}}, nil, nil)
	case "thinking", "redacted_thinking":
		s.err = fmt.Errorf("上游 Anthropic 流返回 %s 内容块，跨协议转换无法保证语义完整", blockType)
		return ""
	default:
		s.err = fmt.Errorf("不支持的 Anthropic content block: %s", blockType)
		return ""
	}
}

func (s *anthropicMatrixSSESource) contentBlockDelta(event map[string]any) string {
	delta, _ := event["delta"].(map[string]any)
	switch stringFromMap(delta, "type") {
	case "text_delta":
		return s.metadata.chunk(map[string]any{"content": stringFromMap(delta, "text")}, nil, nil)
	case "input_json_delta":
		blockIndex := int(int64FromAny(event["index"]))
		toolIndex, exists := s.toolIndexes[blockIndex]
		if !exists {
			s.err = fmt.Errorf("Anthropic tool 参数增量缺少 content_block_start: index=%d", blockIndex)
			return ""
		}
		tool := map[string]any{
			"index":    toolIndex,
			"function": map[string]any{"arguments": stringFromMap(delta, "partial_json")},
		}
		return s.metadata.chunk(map[string]any{"tool_calls": []any{tool}}, nil, nil)
	case "thinking_delta", "signature_delta":
		s.err = fmt.Errorf("上游 Anthropic 流返回 thinking 增量，跨协议转换无法保证语义完整")
		return ""
	default:
		s.err = fmt.Errorf("不支持的 Anthropic content delta: %s", stringFromMap(delta, "type"))
		return ""
	}
}

func (s *anthropicMatrixSSESource) captureUsage(raw any) {
	usage, _ := raw.(map[string]any)
	if usage == nil {
		return
	}
	// Anthropic 的 input_tokens 已经是普通输入，缺少 cache_read 字段时
	// 可按 Anthropic 语义视为本次没有缓存读取；这与 OpenAI/Gemini 的
	// prompt_tokens 缺少缓存拆分时不能猜测不同。
	s.cacheReadKnown = true
	if value, exists := usage["input_tokens"]; exists {
		s.inputTokens = int64FromAny(value)
		s.inputKnown = true
	}
	if value, exists := usage["output_tokens"]; exists {
		s.outputTokens = int64FromAny(value)
		s.outputKnown = true
	}
	if value, exists := usage["cache_read_input_tokens"]; exists {
		s.cacheRead = int64FromAny(value)
		s.cacheReadKnown = true
	}
	if value, exists := usage["cache_creation_input_tokens"]; exists {
		s.cacheCreation = int64FromAny(value)
		s.cacheCreationKnown = true
	}
}

func (s *anthropicMatrixSSESource) finish() string {
	if s.stopped {
		return ""
	}
	s.stopped = true
	finishReason := "stop"
	if s.stopReason == "max_tokens" {
		finishReason = "length"
	} else if s.stopReason == "tool_use" || s.hasTools {
		finishReason = "tool_calls"
	}
	usage := map[string]any{}
	if s.inputKnown {
		promptTokens := s.inputTokens
		if s.cacheReadKnown {
			// OpenAI prompt_tokens 包含 cache read；Anthropic 的 input_tokens
			// 不包含它，转换时需要先恢复总 prompt，再由解析器拆回普通输入。
			promptTokens += s.cacheRead
		}
		usage["prompt_tokens"] = promptTokens
	}
	if s.outputKnown {
		usage["completion_tokens"] = s.outputTokens
	}
	if s.inputKnown && s.outputKnown {
		promptTokens := s.inputTokens
		if s.cacheReadKnown {
			promptTokens += s.cacheRead
		}
		usage["total_tokens"] = promptTokens + s.outputTokens
	}
	if s.cacheReadKnown {
		usage["prompt_tokens_details"] = map[string]any{"cached_tokens": s.cacheRead}
	}
	if s.cacheCreationKnown {
		usage["cache_creation_input_tokens"] = s.cacheCreation
	}
	return s.metadata.chunk(map[string]any{}, finishReason, usage) + "data: [DONE]\n\n"
}

func (s *anthropicMatrixSSESource) Err() error { return s.err }

type matrixResponseTool struct {
	index       int
	callID      string
	name        string
	started     bool
	arguments   strings.Builder
	emittedArgs int
}

type responsesMatrixSSESource struct {
	eventName string
	metadata  matrixChatMetadata
	tools     map[int]*matrixResponseTool
	nextTool  int
	started   bool
	stopped   bool
	err       error
}

func newResponsesMatrixSSESource(model string) *responsesMatrixSSESource {
	return &responsesMatrixSSESource{
		metadata: newMatrixChatMetadata(model),
		tools:    make(map[int]*matrixResponseTool),
	}
}

func (s *responsesMatrixSSESource) ProcessLine(line string) string {
	if s.stopped || s.err != nil {
		return ""
	}
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return ""
	}
	if isSSEControlLine(trimmed) {
		return ""
	}
	if strings.HasPrefix(trimmed, "event:") {
		s.eventName = strings.TrimSpace(strings.TrimPrefix(trimmed, "event:"))
		return ""
	}
	if !strings.HasPrefix(trimmed, "data:") {
		s.err = fmt.Errorf("Responses 流包含无法识别的行: %s", truncateRelayError(trimmed))
		return ""
	}
	payload := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
	if payload == "[DONE]" {
		return s.finish(nil, "completed")
	}
	var event map[string]any
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		s.err = fmt.Errorf("Responses 流事件不是合法 JSON: %w", err)
		return ""
	}
	eventType := stringFromMap(event, "type")
	if eventType == "" {
		eventType = s.eventName
	}
	s.eventName = ""
	if strings.Contains(eventType, "reasoning") {
		s.err = fmt.Errorf("上游 Responses 流返回 reasoning 事件，跨协议转换无法保证语义完整")
		return ""
	}
	switch eventType {
	case "response.created", "response.in_progress":
		response, _ := event["response"].(map[string]any)
		s.applyResponseMetadata(response)
		return ""
	case "response.output_item.added":
		return s.outputItemAdded(event)
	case "response.output_text.delta":
		return s.ensureStarted() + s.metadata.chunk(map[string]any{"content": stringFromMap(event, "delta")}, nil, nil)
	case "response.function_call_arguments.delta":
		return s.functionArgumentsDelta(event)
	case "response.output_item.done":
		return s.outputItemDone(event)
	case "response.completed":
		response, _ := event["response"].(map[string]any)
		return s.finish(response, "completed")
	case "response.incomplete":
		response, _ := event["response"].(map[string]any)
		return s.finish(response, "incomplete")
	case "response.failed", "response.error", "error":
		s.err = fmt.Errorf("Responses 流返回错误: %s", truncateRelayError(payload))
		return ""
	case "response.content_part.added", "response.content_part.done", "response.output_text.done", "response.function_call_arguments.done":
		return ""
	default:
		s.err = fmt.Errorf("不支持的 Responses 流事件: %s", eventType)
		return ""
	}
}

func (s *responsesMatrixSSESource) applyResponseMetadata(response map[string]any) {
	if response == nil {
		return
	}
	s.metadata.apply(
		stringFromMap(response, "id"),
		stringFromMap(response, "model"),
		int64FromAny(response["created_at"]),
	)
}

func (s *responsesMatrixSSESource) ensureStarted() string {
	if s.started {
		return ""
	}
	s.started = true
	return s.metadata.chunk(map[string]any{"role": "assistant", "content": ""}, nil, nil)
}

func (s *responsesMatrixSSESource) outputItemAdded(event map[string]any) string {
	item, _ := event["item"].(map[string]any)
	switch stringFromMap(item, "type") {
	case "message":
		return s.ensureStarted()
	case "function_call":
		outputIndex := int(int64FromAny(event["output_index"]))
		tool := s.responseTool(outputIndex)
		tool.callID = stringFromMap(item, "call_id")
		tool.name = stringFromMap(item, "name")
		return s.startResponseTool(tool)
	case "reasoning":
		s.err = fmt.Errorf("上游 Responses 流返回 reasoning 输出项，跨协议转换无法保证语义完整")
		return ""
	default:
		s.err = fmt.Errorf("不支持的 Responses output item: %s", stringFromMap(item, "type"))
		return ""
	}
}

func (s *responsesMatrixSSESource) functionArgumentsDelta(event map[string]any) string {
	outputIndex := int(int64FromAny(event["output_index"]))
	tool := s.responseTool(outputIndex)
	delta := stringFromMap(event, "delta")
	tool.arguments.WriteString(delta)
	return s.startResponseTool(tool) + s.emitResponseToolArguments(tool, delta)
}

func (s *responsesMatrixSSESource) outputItemDone(event map[string]any) string {
	item, _ := event["item"].(map[string]any)
	if stringFromMap(item, "type") != "function_call" {
		return ""
	}
	outputIndex := int(int64FromAny(event["output_index"]))
	tool := s.responseTool(outputIndex)
	if tool.callID == "" {
		tool.callID = stringFromMap(item, "call_id")
	}
	if tool.name == "" {
		tool.name = stringFromMap(item, "name")
	}
	fullArguments := stringFromMap(item, "arguments")
	var out strings.Builder
	out.WriteString(s.startResponseTool(tool))
	if tool.arguments.Len() == 0 && fullArguments != "" {
		tool.arguments.WriteString(fullArguments)
		out.WriteString(s.emitResponseToolArguments(tool, fullArguments))
	}
	return out.String()
}

func (s *responsesMatrixSSESource) responseTool(outputIndex int) *matrixResponseTool {
	if tool := s.tools[outputIndex]; tool != nil {
		return tool
	}
	tool := &matrixResponseTool{index: s.nextTool, callID: "call_" + uuid.NewString()}
	s.nextTool++
	s.tools[outputIndex] = tool
	return tool
}

func (s *responsesMatrixSSESource) startResponseTool(tool *matrixResponseTool) string {
	if tool.started {
		return ""
	}
	if tool.name == "" {
		return ""
	}
	tool.started = true
	call := map[string]any{
		"index": tool.index, "id": tool.callID, "type": "function",
		"function": map[string]any{"name": tool.name, "arguments": ""},
	}
	return s.ensureStarted() + s.metadata.chunk(map[string]any{"tool_calls": []any{call}}, nil, nil)
}

func (s *responsesMatrixSSESource) emitResponseToolArguments(tool *matrixResponseTool, delta string) string {
	if !tool.started || delta == "" {
		return ""
	}
	tool.emittedArgs += len(delta)
	call := map[string]any{
		"index":    tool.index,
		"function": map[string]any{"arguments": delta},
	}
	return s.metadata.chunk(map[string]any{"tool_calls": []any{call}}, nil, nil)
}

func (s *responsesMatrixSSESource) finish(response map[string]any, status string) string {
	if s.stopped {
		return ""
	}
	s.stopped = true
	s.applyResponseMetadata(response)
	usage := responsesUsageToChatUsage(nil)
	if response != nil {
		usage = responsesUsageToChatUsage(response["usage"])
		if responseStatus := stringFromMap(response, "status"); responseStatus != "" {
			status = responseStatus
		}
	}
	finishReason := "stop"
	if len(s.tools) > 0 {
		finishReason = "tool_calls"
	} else if status == "incomplete" {
		finishReason = "length"
	}
	return s.ensureStarted() + s.metadata.chunk(map[string]any{}, finishReason, usage) + "data: [DONE]\n\n"
}

func responsesUsageToChatUsage(raw any) map[string]any {
	usage, _ := raw.(map[string]any)
	inputTokens := int64FromAny(usage["input_tokens"])
	outputTokens := int64FromAny(usage["output_tokens"])
	inputDetails, _ := usage["input_tokens_details"].(map[string]any)
	outputDetails, _ := usage["output_tokens_details"].(map[string]any)
	result := map[string]any{
		"prompt_tokens": inputTokens, "completion_tokens": outputTokens,
		"total_tokens": inputTokens + outputTokens,
	}
	if _, exists := inputDetails["cached_tokens"]; exists {
		result["prompt_tokens_details"] = map[string]any{"cached_tokens": int64FromAny(inputDetails["cached_tokens"])}
	}
	if _, exists := outputDetails["reasoning_tokens"]; exists {
		result["completion_tokens_details"] = map[string]any{"reasoning_tokens": int64FromAny(outputDetails["reasoning_tokens"])}
	}
	return result
}

func (s *responsesMatrixSSESource) Err() error { return s.err }

type matrixAnthropicTool struct {
	blockIndex int
	id         string
	name       string
	arguments  strings.Builder
	emitted    int
	started    bool
}

type anthropicMatrixSSETarget struct {
	messageID      string
	model          string
	startedMessage bool
	textStarted    bool
	textBlockIndex int
	nextBlockIndex int
	tools          map[int]*matrixAnthropicTool
	toolOrder      []int
	finishReason   string
	inputTokens    int64
	inputKnown     bool
	cacheRead      int64
	cacheReadKnown bool
	outputTokens   int64
	outputKnown    bool
	stopped        bool
	err            error
}

func newAnthropicMatrixSSETarget(model string) *anthropicMatrixSSETarget {
	return &anthropicMatrixSSETarget{
		messageID: "msg_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:24],
		model:     model,
		tools:     make(map[int]*matrixAnthropicTool),
	}
}

func (t *anthropicMatrixSSETarget) ProcessPayload(payload string) string {
	if t.stopped || t.err != nil {
		return ""
	}
	var out strings.Builder
	for _, line := range strings.Split(strings.TrimSpace(payload), "\n") {
		converted := t.processLine(line)
		out.WriteString(converted)
		if t.err != nil {
			break
		}
	}
	return out.String()
}

func (t *anthropicMatrixSSETarget) processLine(line string) string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "event:") || isSSEControlLine(trimmed) {
		return ""
	}
	if !strings.HasPrefix(trimmed, "data:") {
		t.err = fmt.Errorf("Chat 中间流包含无法识别的行: %s", truncateRelayError(trimmed))
		return ""
	}
	payload := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
	if payload == "[DONE]" {
		return t.finish()
	}
	var chunk map[string]any
	if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
		t.err = fmt.Errorf("Chat 中间流事件不是合法 JSON: %w", err)
		return ""
	}
	if id := stringFromMap(chunk, "id"); id != "" && !t.startedMessage {
		t.messageID = id
	}
	if model := stringFromMap(chunk, "model"); model != "" {
		t.model = model
	}
	t.captureChatUsage(chunk["usage"])
	choices, _ := chunk["choices"].([]any)
	if len(choices) == 0 {
		return ""
	}
	choice, _ := choices[0].(map[string]any)
	delta, _ := choice["delta"].(map[string]any)
	if _, exists := delta["reasoning_content"]; exists {
		t.err = fmt.Errorf("Chat 中间流包含 reasoning_content，无法转换为有效 Anthropic thinking 块")
		return ""
	}
	var out strings.Builder
	if content, ok := delta["content"].(string); ok && content != "" {
		out.WriteString(t.writeTextDelta(content))
	}
	if calls, ok := delta["tool_calls"].([]any); ok {
		out.WriteString(t.writeToolDeltas(calls))
	}
	if finish := stringFromMap(choice, "finish_reason"); finish != "" {
		t.finishReason = finish
	}
	return out.String()
}

func (t *anthropicMatrixSSETarget) ensureMessageStart() string {
	if t.startedMessage {
		return ""
	}
	t.startedMessage = true
	message := map[string]any{
		"id": t.messageID, "type": "message", "role": "assistant", "model": t.model,
		"content": []any{}, "stop_reason": nil, "stop_sequence": nil,
		"usage": map[string]any{},
	}
	return string(encodeNamedSSE("message_start", map[string]any{"type": "message_start", "message": message}))
}

func (t *anthropicMatrixSSETarget) writeTextDelta(text string) string {
	var out strings.Builder
	out.WriteString(t.ensureMessageStart())
	if !t.textStarted {
		t.textBlockIndex = t.nextBlockIndex
		t.nextBlockIndex++
		t.textStarted = true
		out.Write(encodeNamedSSE("content_block_start", map[string]any{
			"type": "content_block_start", "index": t.textBlockIndex,
			"content_block": map[string]any{"type": "text", "text": ""},
		}))
	}
	out.Write(encodeNamedSSE("content_block_delta", map[string]any{
		"type": "content_block_delta", "index": t.textBlockIndex,
		"delta": map[string]any{"type": "text_delta", "text": text},
	}))
	return out.String()
}

func (t *anthropicMatrixSSETarget) writeToolDeltas(calls []any) string {
	var out strings.Builder
	for _, rawCall := range calls {
		call, _ := rawCall.(map[string]any)
		index := int(int64FromAny(call["index"]))
		tool := t.tools[index]
		if tool == nil {
			tool = &matrixAnthropicTool{blockIndex: t.nextBlockIndex, id: "call_" + uuid.NewString()}
			t.nextBlockIndex++
			t.tools[index] = tool
			t.toolOrder = append(t.toolOrder, index)
		}
		if id := stringFromMap(call, "id"); id != "" {
			tool.id = id
		}
		function, _ := call["function"].(map[string]any)
		if name := stringFromMap(function, "name"); name != "" {
			tool.name = name
		}
		if arguments := stringFromMap(function, "arguments"); arguments != "" {
			tool.arguments.WriteString(arguments)
		}
		out.WriteString(t.startAnthropicTool(tool))
		if tool.started && tool.arguments.Len() > tool.emitted {
			arguments := tool.arguments.String()[tool.emitted:]
			tool.emitted = tool.arguments.Len()
			out.Write(encodeNamedSSE("content_block_delta", map[string]any{
				"type": "content_block_delta", "index": tool.blockIndex,
				"delta": map[string]any{"type": "input_json_delta", "partial_json": arguments},
			}))
		}
	}
	return out.String()
}

func (t *anthropicMatrixSSETarget) startAnthropicTool(tool *matrixAnthropicTool) string {
	if tool.started || tool.name == "" {
		return ""
	}
	tool.started = true
	return t.ensureMessageStart() + string(encodeNamedSSE("content_block_start", map[string]any{
		"type": "content_block_start", "index": tool.blockIndex,
		"content_block": map[string]any{"type": "tool_use", "id": tool.id, "name": tool.name, "input": map[string]any{}},
	}))
}

func (t *anthropicMatrixSSETarget) captureChatUsage(raw any) {
	usage, _ := raw.(map[string]any)
	if usage == nil {
		return
	}
	inputRaw, inputExists := usage["prompt_tokens"]
	inputTokens := int64FromAny(inputRaw)
	inputDetails, _ := usage["prompt_tokens_details"].(map[string]any)
	if cachedRaw, cacheExists := inputDetails["cached_tokens"]; cacheExists {
		cachedTokens := int64FromAny(cachedRaw)
		if inputExists && inputTokens >= cachedTokens && inputTokens >= 0 && cachedTokens >= 0 {
			t.inputTokens = inputTokens - cachedTokens
			t.inputKnown = true
			t.cacheRead = cachedTokens
			t.cacheReadKnown = true
		} else {
			// 缓存拆分矛盾时不截断，也不把原始总输入当普通输入计价。
			t.inputKnown = false
			t.cacheReadKnown = false
		}
	} else {
		// OpenAI prompt_tokens 包含缓存；没有 cached_tokens 时不能直接
		// 当作 Anthropic 的普通 input_tokens。
		t.inputKnown = false
		t.cacheReadKnown = false
	}
	if value, exists := usage["completion_tokens"]; exists {
		t.outputTokens = int64FromAny(value)
		t.outputKnown = true
	}
}

func (t *anthropicMatrixSSETarget) finish() string {
	if t.stopped {
		return ""
	}
	t.stopped = true
	var out strings.Builder
	out.WriteString(t.ensureMessageStart())
	if !t.textStarted && len(t.toolOrder) == 0 {
		out.WriteString(t.writeTextDelta(""))
	}
	if t.textStarted {
		out.Write(encodeNamedSSE("content_block_stop", map[string]any{"type": "content_block_stop", "index": t.textBlockIndex}))
	}
	for _, index := range t.toolOrder {
		tool := t.tools[index]
		out.WriteString(t.startAnthropicTool(tool))
		if !tool.started {
			t.err = fmt.Errorf("Chat tool call 缺少 function.name")
			return ""
		}
		out.Write(encodeNamedSSE("content_block_stop", map[string]any{"type": "content_block_stop", "index": tool.blockIndex}))
	}
	stopReason := "end_turn"
	if t.finishReason == "length" {
		stopReason = "max_tokens"
	} else if t.finishReason == "tool_calls" || t.finishReason == "function_call" || len(t.toolOrder) > 0 {
		stopReason = "tool_use"
	}
	out.Write(encodeNamedSSE("message_delta", map[string]any{
		"type":  "message_delta",
		"delta": map[string]any{"stop_reason": stopReason, "stop_sequence": nil},
		"usage": func() map[string]any {
			usage := map[string]any{}
			if t.inputKnown {
				usage["input_tokens"] = t.inputTokens
			}
			if t.cacheReadKnown {
				usage["cache_read_input_tokens"] = t.cacheRead
			}
			if t.outputKnown {
				usage["output_tokens"] = t.outputTokens
			}
			return usage
		}(),
	}))
	out.Write(encodeNamedSSE("message_stop", map[string]any{"type": "message_stop"}))
	return out.String()
}

func (t *anthropicMatrixSSETarget) Err() error { return t.err }
