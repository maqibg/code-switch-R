package relay

import (
	"bufio"
	"fmt"
	"io"
	"net/http"

	"github.com/daodao97/xgo/xrequest"
)

func readCodexContinueRound(w http.ResponseWriter, resp *xrequest.Response, roundNo int, state *codexFoldState) (codexRoundResult, error) {
	defer resp.RawResponse.Body.Close()
	reader := bufio.NewReader(resp.RawResponse.Body)
	parser := &codexSSEParser{}
	result := codexRoundResult{}
	itemKind := make(map[string]string)
	outputMap := make(map[string]int)

	for {
		chunk, err := reader.ReadBytes('\n')
		if len(chunk) > 0 {
			handleCodexFrames(w, parser.push(chunk), roundNo, state, &result, itemKind, outputMap)
		}
		if err != nil {
			if err != io.EOF {
				return result, err
			}
			handleCodexFrames(w, parser.finish(), roundNo, state, &result, itemKind, outputMap)
			break
		}
	}
	result.usage = terminalUsage(result.terminal)
	result.reasoningTokens = nestedInt64(result.usage, "output_tokens_details", "reasoning_tokens")
	return result, nil
}

func handleCodexFrames(w http.ResponseWriter, frames []codexSSEFrame, roundNo int, state *codexFoldState, result *codexRoundResult, itemKind map[string]string, outputMap map[string]int) {
	for _, frame := range frames {
		if frame.done || frame.event == nil {
			continue
		}
		handleCodexEvent(w, frame.event, frame.data, roundNo, state, result, itemKind, outputMap)
	}
}

func handleCodexEvent(w http.ResponseWriter, event map[string]any, raw []byte, roundNo int, state *codexFoldState, result *codexRoundResult, itemKind map[string]string, outputMap map[string]int) {
	eventType := stringField(event, "type")
	if eventType == "response.created" || eventType == "response.in_progress" {
		if roundNo == 1 {
			if eventType == "response.created" {
				state.baseResponse = mapField(event, "response")
			}
			writeCodexSequencedEvent(w, event, state)
		}
		return
	}
	if isCodexTerminalEvent(eventType) {
		result.terminal = event
		return
	}
	if eventType == "response.output_item.added" {
		handleCodexOutputItemAdded(w, event, raw, state, result, itemKind, outputMap)
		return
	}
	upstreamIndex, ok := event["output_index"]
	if !ok {
		writeCodexSequencedEvent(w, event, state)
		return
	}
	key := fmt.Sprint(upstreamIndex)
	switch itemKind[key] {
	case "reasoning":
		if index, exists := outputMap[key]; exists {
			setOutputIndex(event, index)
		}
		if eventType == "response.output_item.done" {
			if item, exists := event["item"]; exists {
				result.roundReasoning = append(result.roundReasoning, item)
				state.finalOutput = append(state.finalOutput, item)
			}
		}
		writeCodexSequencedEvent(w, event, state)
	case "buffered":
		for i := range result.bufferedItems {
			if fmt.Sprint(result.bufferedItems[i].upstreamIndex) == key {
				if eventType == "response.output_item.done" {
					if item, exists := event["item"]; exists {
						result.bufferedItems[i].item = item
					}
				}
				result.bufferedItems[i].events = append(result.bufferedItems[i].events, codexBufferedEvent{
					eventType: eventType,
					data:      append([]byte(nil), raw...),
				})
				break
			}
		}
	default:
		writeCodexSequencedEvent(w, event, state)
	}
}

func handleCodexOutputItemAdded(w http.ResponseWriter, event map[string]any, raw []byte, state *codexFoldState, result *codexRoundResult, itemKind map[string]string, outputMap map[string]int) {
	upstreamIndex := event["output_index"]
	key := fmt.Sprint(upstreamIndex)
	if outputItemType(event) == "reasoning" {
		itemKind[key] = "reasoning"
		outputMap[key] = state.downstreamOutputIndex
		setOutputIndex(event, state.downstreamOutputIndex)
		state.downstreamOutputIndex++
		writeCodexSequencedEvent(w, event, state)
		return
	}
	itemKind[key] = "buffered"
	result.bufferedItems = append(result.bufferedItems, codexBufferedItem{
		upstreamIndex: upstreamIndex,
		events: []codexBufferedEvent{{
			eventType: stringField(event, "type"),
			data:      append([]byte(nil), raw...),
		}},
		item: event["item"],
	})
}

func flushCodexBufferedItems(w http.ResponseWriter, state *codexFoldState, items []codexBufferedItem) {
	for _, item := range items {
		finalItem := item.item
		for _, event := range item.events {
			writeCodexSequencedRawEvent(w, event.data, event.eventType, state.downstreamOutputIndex, state)
		}
		state.downstreamOutputIndex++
		state.finalOutput = append(state.finalOutput, finalItem)
	}
}
