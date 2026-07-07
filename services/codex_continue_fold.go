package services

import (
	"net/http"

	"github.com/daodao97/xgo/xrequest"
)

func (prs *ProviderRelayService) foldCodexResponsesStream(
	w http.ResponseWriter,
	provider Provider,
	endpoint string,
	query map[string]string,
	headers map[string]string,
	baseBody map[string]any,
	firstResponse *xrequest.Response,
	config codexContinueConfig,
	state *codexFoldState,
) error {
	response := firstResponse
	for roundNo, continuations := 1, 0; ; roundNo++ {
		result, err := readCodexContinueRound(w, response, roundNo, state)
		if err != nil {
			writeCodexSSEEvent(w, syntheticCodexIncomplete(state, "upstream_error", roundNo, config.Step))
			writeCodexSSEDone(w)
			flushWriter(w)
			return nil
		}
		state.usage.add(result.usage)
		truncated := isCodexReasoningTruncated(result.reasoningTokens, config.Step)
		hasEncrypted := lastReasoningHasEncrypted(result.roundReasoning)
		canContinue := result.terminal != nil && truncated && hasEncrypted && continuations < config.MaxContinuations
		state.rounds = append(state.rounds, map[string]any{
			"round":                 roundNo,
			"reasoning_tokens":      result.reasoningTokens,
			"truncated":             truncated,
			"has_encrypted_content": hasEncrypted,
			"continued":             canContinue,
		})

		if canContinue {
			continuations++
			state.replayTail = append(state.replayTail, cloneJSONValue(result.roundReasoning).([]any)...)
			state.replayTail = append(state.replayTail, codexCommentaryMarker(config.Marker))
			nextBody, err := buildCodexContinuationPayload(baseBody, state.replayTail)
			if err != nil {
				writeCodexSSEEvent(w, syntheticCodexIncomplete(state, "payload_error", roundNo, config.Step))
				writeCodexSSEDone(w)
				flushWriter(w)
				return nil
			}
			response, err = prs.sendNativeCodexResponsesRequest(provider, endpoint, query, headers, nextBody)
			if err != nil || response == nil || response.StatusCode() < 200 || response.StatusCode() >= 300 || !isEventStream(response) {
				writeCodexSSEEvent(w, syntheticCodexIncomplete(state, "upstream_error", roundNo, config.Step))
				writeCodexSSEDone(w)
				flushWriter(w)
				return nil
			}
			continue
		}

		if result.terminal == nil {
			writeCodexSSEEvent(w, syntheticCodexIncomplete(state, "upstream_eof", roundNo, config.Step))
			writeCodexSSEDone(w)
			flushWriter(w)
			return nil
		}

		flushCodexBufferedItems(w, state, result.bufferedItems)
		writeCodexSSEEvent(w, reconstructCodexTerminal(state, result.terminal, stoppedReason(truncated, hasEncrypted, continuations, config), roundNo, config.Step))
		writeCodexSSEDone(w)
		flushWriter(w)
		return nil
	}
}

func reconstructCodexTerminal(state *codexFoldState, terminal map[string]any, reason string, roundNo int, step int64) map[string]any {
	terminalType := "response.incomplete"
	if terminal != nil && stringField(terminal, "type") != "" {
		terminalType = stringField(terminal, "type")
	}
	response := cloneJSONMap(state.baseResponse)
	if terminalResp := mapField(terminal, "response"); terminalResp != nil {
		for key, value := range terminalResp {
			if key == "status" || key == "incomplete_details" {
				response[key] = cloneJSONValue(value)
			}
		}
	}
	response["output"] = cloneJSONValue(state.finalOutput)
	if publicUsage := state.usage.publicUsage(); publicUsage != nil {
		response["usage"] = publicUsage
	}
	response["metadata"] = codexMetadata(response["metadata"], state, reason, roundNo, step)
	return map[string]any{"type": terminalType, "response": response, "sequence_number": state.nextSeq()}
}

func syntheticCodexIncomplete(state *codexFoldState, reason string, roundNo int, step int64) map[string]any {
	response := cloneJSONMap(state.baseResponse)
	response["status"] = "incomplete"
	response["incomplete_details"] = map[string]any{"reason": reason}
	response["output"] = cloneJSONValue(state.finalOutput)
	if publicUsage := state.usage.publicUsage(); publicUsage != nil {
		response["usage"] = publicUsage
	}
	response["metadata"] = codexMetadata(response["metadata"], state, reason, roundNo, step)
	return map[string]any{"type": "response.incomplete", "response": response, "sequence_number": state.nextSeq()}
}

func codexMetadata(existing any, state *codexFoldState, reason string, roundNo int, step int64) map[string]any {
	metadata, _ := existing.(map[string]any)
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadata["code_switch_codex_continue"] = map[string]any{
		"enabled":         true,
		"proxy_rounds":    roundNo,
		"stopped_reason":  reason,
		"truncation_step": step,
		"rounds":          cloneJSONValue(state.rounds),
	}
	return metadata
}

func stoppedReason(truncated bool, hasEncrypted bool, continuations int, config codexContinueConfig) string {
	if truncated && !hasEncrypted {
		return "no_encrypted_content"
	}
	if truncated && continuations >= config.MaxContinuations {
		return "max_continue"
	}
	return ""
}
