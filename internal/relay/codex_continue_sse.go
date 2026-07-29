package relay

import (
	"encoding/json"
	"strings"
)

type codexSSEFrame struct {
	done  bool
	event map[string]any
	data  []byte
}

type codexSSEParser struct {
	buffer string
}

func (p *codexSSEParser) push(chunk []byte) []codexSSEFrame {
	p.buffer += string(chunk)
	frames := make([]codexSSEFrame, 0)
	for {
		index, sepLen := findSSEBlockEnd(p.buffer)
		if index < 0 {
			break
		}
		block := p.buffer[:index]
		p.buffer = p.buffer[index+sepLen:]
		if frame, ok := parseCodexSSEBlock(block); ok {
			frames = append(frames, frame)
		}
	}
	return frames
}

func (p *codexSSEParser) finish() []codexSSEFrame {
	if strings.TrimSpace(p.buffer) == "" {
		return nil
	}
	block := p.buffer
	p.buffer = ""
	frame, ok := parseCodexSSEBlock(block)
	if !ok {
		return nil
	}
	return []codexSSEFrame{frame}
}

func findSSEBlockEnd(buffer string) (int, int) {
	lf := strings.Index(buffer, "\n\n")
	crlf := strings.Index(buffer, "\r\n\r\n")
	if lf < 0 {
		if crlf < 0 {
			return -1, 0
		}
		return crlf, 4
	}
	if crlf >= 0 && crlf < lf {
		return crlf, 4
	}
	return lf, 2
}

func parseCodexSSEBlock(block string) (codexSSEFrame, bool) {
	lines := strings.Split(strings.ReplaceAll(block, "\r\n", "\n"), "\n")
	dataLines := make([]string, 0)
	for _, line := range lines {
		line = strings.TrimSuffix(line, "\r")
		if strings.HasPrefix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if len(dataLines) == 0 {
		return codexSSEFrame{}, false
	}
	payload := strings.Join(dataLines, "\n")
	if strings.TrimSpace(payload) == "[DONE]" {
		return codexSSEFrame{done: true}, true
	}
	var event map[string]any
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		return codexSSEFrame{}, false
	}
	return codexSSEFrame{event: event, data: []byte(payload)}, true
}
