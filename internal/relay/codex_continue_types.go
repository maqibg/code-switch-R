package relay

import "codeswitch/services"

type codexFoldState struct {
	seq                   int64
	downstreamOutputIndex int
	baseResponse          map[string]any
	finalOutput           []any
	replayTail            []any
	rounds                []any
	usage                 codexFoldUsage
	requestLog            *services.RequestLog
}

type codexFoldUsage struct {
	saw                 bool
	firstInputTokens    int64
	firstInputKnown     bool
	firstCachedTokens   int64
	firstCachedKnown    bool
	totalReasoning      int64
	finalOutputTokens   int64
	finalOutputKnown    bool
	finalReasoningToken int64
	finalReasoningKnown bool
}

func (u *codexFoldUsage) add(usage map[string]any) {
	if usage == nil {
		return
	}
	if !u.saw {
		u.firstInputTokens = nestedInt64(usage, "input_tokens")
		u.firstInputKnown = nestedExists(usage, "input_tokens")
		u.firstCachedTokens = nestedInt64(usage, "input_tokens_details", "cached_tokens")
		u.firstCachedKnown = nestedExists(usage, "input_tokens_details", "cached_tokens")
	}
	reasoning := nestedInt64(usage, "output_tokens_details", "reasoning_tokens")
	u.totalReasoning += reasoning
	u.finalOutputTokens = nestedInt64(usage, "output_tokens")
	u.finalOutputKnown = nestedExists(usage, "output_tokens")
	u.finalReasoningToken = reasoning
	u.finalReasoningKnown = nestedExists(usage, "output_tokens_details", "reasoning_tokens")
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
	usage := map[string]any{}
	if u.firstInputKnown {
		usage["input_tokens"] = u.firstInputTokens
	}
	if u.finalOutputKnown {
		usage["output_tokens"] = output
	}
	if u.firstInputKnown && u.finalOutputKnown {
		usage["total_tokens"] = u.firstInputTokens + output
	}
	if u.finalReasoningKnown {
		usage["output_tokens_details"] = map[string]any{"reasoning_tokens": u.totalReasoning}
	}
	if u.firstCachedKnown {
		usage["input_tokens_details"] = map[string]any{"cached_tokens": u.firstCachedTokens}
	}
	return usage
}

func (u codexFoldUsage) applyToLog(log *services.RequestLog) {
	if log == nil {
		return
	}
	public := u.publicUsage()
	if public == nil {
		return
	}
	rawInput := nestedInt64(public, "input_tokens")
	rawInputKnown := nestedExists(public, "input_tokens")
	cached := nestedInt64(public, "input_tokens_details", "cached_tokens")
	cachedKnown := nestedExists(public, "input_tokens_details", "cached_tokens")
	if rawInputKnown && cachedKnown && rawInput >= cached && rawInput >= 0 && cached >= 0 {
		log.InputTokens = int(rawInput - cached)
		log.UsageKnownMask |= services.UsageFieldInput
		log.CacheReadTokens = int(cached)
		log.UsageKnownMask |= services.UsageFieldCacheRead
	} else if cachedKnown {
		log.CacheReadTokens = int(cached)
		log.UsageKnownMask |= services.UsageFieldCacheRead
	}
	if nestedExists(public, "output_tokens") {
		log.OutputTokens = int(nestedInt64(public, "output_tokens"))
		log.UsageKnownMask |= services.UsageFieldOutput
	}
	if nestedExists(public, "output_tokens_details", "reasoning_tokens") {
		log.ReasoningTokens = int(nestedInt64(public, "output_tokens_details", "reasoning_tokens"))
		log.UsageKnownMask |= services.UsageFieldReasoning
	}
	log.UsageStatus = services.UsageStatusPartial
	finalizeUsageStatus(log)
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
