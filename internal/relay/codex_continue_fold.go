package relay

import (
	"codeswitch/services"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/daodao97/xgo/xrequest"
	"github.com/gin-gonic/gin"
)

type codexRoundLog struct {
	state           *codexFoldState
	traceID         string
	roundNo         int
	reasoningTokens int64
	truncated       bool
	hasEncrypted    bool
	canContinue     bool
}

type codexContinuationRoundInput struct {
	baseBody map[string]any
	state    *codexFoldState
	result   codexRoundResult
	config   codexContinueConfig
}

type codexIncompleteOutput struct {
	w       http.ResponseWriter
	state   *codexFoldState
	traceID string
	reason  string
	roundNo int
	step    int64
	err     error
}

func (prs *ProviderRelayService) foldCodexResponsesStream(
	c *gin.Context,
	w http.ResponseWriter,
	execution *relayForwardExecution,
	endpoint string,
	query map[string]string,
	headers map[string]string,
	baseBody map[string]any,
	firstResponse *xrequest.Response,
	config codexContinueConfig,
	state *codexFoldState,
	traceID string,
	firstAttemptStarted time.Time,
) error {
	provider := execution.Provider
	response := firstResponse
	attemptStarted := firstAttemptStarted
	for roundNo, continuations := 1, 0; ; roundNo++ {
		result, err := readCodexContinueRound(w, response, roundNo, state)
		if err != nil {
			attemptLog := codexContinueRoundRequestLog(provider, execution.Model, response, result.usage)
			prs.recordCodexContinueAttempt(c, provider, execution, attemptLog, attemptStarted, false, err)
			completeCodexContinueWithIncomplete(codexIncompleteOutput{
				w: w, state: state, traceID: traceID, reason: "upstream_error", roundNo: roundNo, step: config.Step, err: err,
			})
			return nil
		}
		state.usage.add(result.usage)
		attemptLog := codexContinueRoundRequestLog(provider, execution.Model, response, result.usage)
		attemptSuccess := result.terminal != nil
		var attemptErr error
		if !attemptSuccess {
			attemptErr = fmt.Errorf("upstream stream ended without terminal event")
		}
		prs.recordCodexContinueAttempt(c, provider, execution, attemptLog, attemptStarted, attemptSuccess, attemptErr)
		truncated := isCodexReasoningTruncated(result.reasoningTokens, config.Step)
		hasEncrypted := lastReasoningHasEncrypted(result.roundReasoning)
		canContinue := result.terminal != nil && truncated && hasEncrypted && continuations < config.MaxContinuations
		recordCodexContinueRound(codexRoundLog{
			state:           state,
			traceID:         traceID,
			roundNo:         roundNo,
			reasoningTokens: result.reasoningTokens,
			truncated:       truncated,
			hasEncrypted:    hasEncrypted,
			canContinue:     canContinue,
		})

		if canContinue {
			continuations++
			nextBody, err := prepareCodexContinuationRound(codexContinuationRoundInput{
				baseBody: baseBody,
				state:    state,
				result:   result,
				config:   config,
			})
			if err != nil {
				completeCodexContinueWithIncomplete(codexIncompleteOutput{
					w: w, state: state, traceID: traceID, reason: "payload_error", roundNo: roundNo, step: config.Step, err: err,
				})
				return nil
			}
			logCodexContinue("INFO", traceID, "发起续写请求 | next_round=%d | provider=%s | replay_items=%d", roundNo+1, provider.Name, len(state.replayTail))
			attemptStarted = time.Now()
			response, err = prs.sendNativeCodexResponsesRequest(c, provider, endpoint, query, headers, nextBody)
			if err != nil || response == nil || response.StatusCode() < 200 || response.StatusCode() >= 300 || !isEventStream(response) {
				attemptErr := err
				if attemptErr == nil {
					switch {
					case response == nil:
						attemptErr = fmt.Errorf("empty response")
					case response.StatusCode() < 200 || response.StatusCode() >= 300:
						attemptErr = fmt.Errorf("upstream status %d", response.StatusCode())
					default:
						attemptErr = fmt.Errorf("upstream response is not event-stream")
					}
				}
				attemptLog := codexContinueRoundRequestLog(provider, execution.Model, response, nil)
				prs.recordCodexContinueAttempt(c, provider, execution, attemptLog, attemptStarted, false, attemptErr)
				logCodexContinuationUpstreamFailure(traceID, roundNo, response, err)
				writeCodexSSEEvent(w, syntheticCodexIncomplete(state, "upstream_error", roundNo, config.Step))
				writeCodexSSEDone(w)
				flushWriter(w)
				return nil
			}
			continue
		}

		if result.terminal == nil {
			completeCodexContinueWithIncomplete(codexIncompleteOutput{
				w: w, state: state, traceID: traceID, reason: "upstream_eof", roundNo: roundNo, step: config.Step,
			})
			return nil
		}

		flushCodexBufferedItems(w, state, result.bufferedItems)
		reason := stoppedReason(truncated, hasEncrypted, continuations, config)
		logCodexContinueFinished(traceID, roundNo, continuations, reason)
		writeCodexSSEEvent(w, reconstructCodexTerminal(state, result.terminal, reason, roundNo, config.Step))
		writeCodexSSEDone(w)
		flushWriter(w)
		return nil
	}
}

func codexContinueRoundRequestLog(provider services.Provider, model string, response *xrequest.Response, usage map[string]any) services.RequestLog {
	logEntry := services.RequestLog{Platform: "codex", Provider: provider.Name, Model: model, IsStream: true}
	if response != nil {
		logEntry.HttpCode = response.StatusCode()
	}
	if usage != nil {
		encoded, _ := json.Marshal(map[string]any{"usage": usage})
		CodexParseTokenUsageFromResponse(string(encoded), &logEntry)
	}
	return logEntry
}

func recordCodexContinueRound(entry codexRoundLog) {
	logCodexContinue(
		"INFO",
		entry.traceID,
		"round=%d | reasoning_tokens=%d | truncated=%t | encrypted=%t | continue=%t",
		entry.roundNo,
		entry.reasoningTokens,
		entry.truncated,
		entry.hasEncrypted,
		entry.canContinue,
	)
	entry.state.rounds = append(entry.state.rounds, map[string]any{
		"round":                 entry.roundNo,
		"reasoning_tokens":      entry.reasoningTokens,
		"truncated":             entry.truncated,
		"has_encrypted_content": entry.hasEncrypted,
		"continued":             entry.canContinue,
	})
}

func prepareCodexContinuationRound(input codexContinuationRoundInput) ([]byte, error) {
	input.state.replayTail = append(input.state.replayTail, input.result.roundReasoning...)
	input.state.replayTail = append(input.state.replayTail, codexCommentaryMarker(input.config.Marker))
	return buildCodexContinuationPayload(input.baseBody, input.state.replayTail)
}

func completeCodexContinueWithIncomplete(output codexIncompleteOutput) {
	logCodexContinueIncomplete(output)
	writeCodexSSEEvent(output.w, syntheticCodexIncomplete(output.state, output.reason, output.roundNo, output.step))
	writeCodexSSEDone(output.w)
	flushWriter(output.w)
}

func logCodexContinueIncomplete(output codexIncompleteOutput) {
	level := "WARN"
	if output.reason == "payload_error" {
		level = "ERROR"
	}
	if output.err != nil {
		logCodexContinue(level, output.traceID, "自动续写中止 | round=%d | reason=%s | error=%s", output.roundNo, output.reason, codexContinueErrorSummary(output.err))
		return
	}
	logCodexContinue(level, output.traceID, "自动续写中止 | round=%d | reason=%s", output.roundNo, output.reason)
}

func logCodexContinuationUpstreamFailure(traceID string, roundNo int, response *xrequest.Response, err error) {
	if response != nil {
		logCodexContinue("WARN", traceID, "自动续写中止 | round=%d | reason=upstream_error | http=%d | event_stream=%t", roundNo, response.StatusCode(), isEventStream(response))
		return
	}
	if err != nil {
		logCodexContinue("WARN", traceID, "自动续写中止 | round=%d | reason=upstream_error | error=%s", roundNo, codexContinueErrorSummary(err))
		return
	}
	logCodexContinue("WARN", traceID, "自动续写中止 | round=%d | reason=upstream_error | error=empty response", roundNo)
}

func logCodexContinueFinished(traceID string, rounds int, continuations int, reason string) {
	level := "INFO"
	displayReason := reason
	if displayReason == "" {
		displayReason = "completed"
	}
	if displayReason != "completed" {
		level = "WARN"
	}
	logCodexContinue(level, traceID, "自动续写结束 | rounds=%d | continuations=%d | reason=%s", rounds, continuations, displayReason)
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
