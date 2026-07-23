package services

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

func TestClassifyConsoleLogLevelUsesMessageLevelBeforePipe(t *testing.T) {
	tests := []struct {
		name         string
		defaultLevel string
		message      string
		want         string
	}{
		{name: "wails info on stderr", defaultLevel: "ERROR", message: "Jul 23 17:11:41.213 INF Build Info", want: "INFO"},
		{name: "wails warning", defaultLevel: "ERROR", message: "Jul 23 17:11:41.213 WRN retrying request", want: "WARN"},
		{name: "wails error", defaultLevel: "INFO", message: "Jul 23 17:11:41.213 ERR Bound method returned an error", want: "ERROR"},
		{name: "application warning", defaultLevel: "INFO", message: "⚠️  [claude] 没有启用的 provider", want: "WARN"},
		{name: "plain stderr", defaultLevel: "ERROR", message: "plain stderr failure", want: "ERROR"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := classifyConsoleLogLevel(test.defaultLevel, test.message); got != test.want {
				t.Fatalf("classifyConsoleLogLevel(%q, %q) = %q, want %q", test.defaultLevel, test.message, got, test.want)
			}
		})
	}
}

func TestConsoleReadPipePreservesOutputAndStoresCompleteLines(t *testing.T) {
	service := &ConsoleService{logs: make([]ConsoleLog, 0, 4), maxLogs: 100}
	input := strings.Join([]string{
		"Jul 23 17:11:41.213 INF Build Info: Wails=v3",
		"Jul 23 17:11:41.216 INF [AssetFileServerFS] Handling request url=/ file=.",
		"Jul 23 17:11:42.794 ERR Bound method returned an error",
		"plain stderr tail",
	}, "\r\n")
	var output bytes.Buffer

	service.readPipe(strings.NewReader(input), "ERROR", &output)

	if output.String() != input {
		t.Fatalf("mirrored output changed: got=%q want=%q", output.String(), input)
	}
	logs := service.GetLogs()
	gotLevels := make([]string, 0, len(logs))
	gotMessages := make([]string, 0, len(logs))
	for _, entry := range logs {
		gotLevels = append(gotLevels, entry.Level)
		gotMessages = append(gotMessages, entry.Message)
	}
	wantLevels := []string{"INFO", "ERROR", "ERROR"}
	wantMessages := []string{
		"Jul 23 17:11:41.213 INF Build Info: Wails=v3",
		"Jul 23 17:11:42.794 ERR Bound method returned an error",
		"plain stderr tail",
	}
	if !reflect.DeepEqual(gotLevels, wantLevels) || !reflect.DeepEqual(gotMessages, wantMessages) {
		t.Fatalf("stored logs = levels %#v messages %#v, want levels %#v messages %#v", gotLevels, gotMessages, wantLevels, wantMessages)
	}
}
