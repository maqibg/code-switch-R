package services

type codexFoldState struct {
	seq                   int64
	downstreamOutputIndex int
	baseResponse          map[string]any
	finalOutput           []any
	replayTail            []any
	rounds                []any
	usage                 codexFoldUsage
	requestLog            *ReqeustLog
}

type codexFoldUsage struct {
	saw                 bool
	firstInputTokens    int64
	firstCachedTokens   int64
	totalReasoning      int64
	finalOutputTokens   int64
	finalReasoningToken int64
}

func (u *codexFoldUsage) add(usage map[string]any) {
	if usage == nil {
		return
	}
	if !u.saw {
		u.firstInputTokens = nestedInt64(usage, "input_tokens")
		u.firstCachedTokens = nestedInt64(usage, "input_tokens_details", "cached_tokens")
	}
	reasoning := nestedInt64(usage, "output_tokens_details", "reasoning_tokens")
	u.totalReasoning += reasoning
	u.finalOutputTokens = nestedInt64(usage, "output_tokens")
	u.finalReasoningToken = reasoning
	u.saw = true
}

func (u codexFoldUsage) publicUsage() map[string]any {
	if !u.saw {
		return nil
	}
	visibleOutput := u.finalOutputTokens - u.finalReasoningToken
	if visibleOutput < 0 {
		visibleOutput = 0
	}
	output := u.totalReasoning + visibleOutput
	usage := map[string]any{
		"input_tokens":  u.firstInputTokens,
		"output_tokens": output,
		"total_tokens":  u.firstInputTokens + output,
		"output_tokens_details": map[string]any{
			"reasoning_tokens": u.totalReasoning,
		},
	}
	if u.firstCachedTokens > 0 {
		usage["input_tokens_details"] = map[string]any{"cached_tokens": u.firstCachedTokens}
	}
	return usage
}

func (u codexFoldUsage) applyToLog(log *ReqeustLog) {
	if log == nil {
		return
	}
	public := u.publicUsage()
	if public == nil {
		return
	}
	log.InputTokens = int(nestedInt64(public, "input_tokens"))
	log.OutputTokens = int(nestedInt64(public, "output_tokens"))
	log.CacheReadTokens = int(nestedInt64(public, "input_tokens_details", "cached_tokens"))
	log.ReasoningTokens = int(nestedInt64(public, "output_tokens_details", "reasoning_tokens"))
}

type codexBufferedItem struct {
	upstreamIndex any
	events        []codexBufferedEvent
	item          any
}

type codexBufferedEvent struct {
	eventType string
	data      []byte
}

type codexRoundResult struct {
	terminal        map[string]any
	usage           map[string]any
	reasoningTokens int64
	roundReasoning  []any
	bufferedItems   []codexBufferedItem
}
