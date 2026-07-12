package parse

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestToolCallMatched(t *testing.T) {
	s := mustParse(t)
	got := eventsOfKind(s, KindToolCall)
	// fixture 中の ToolCall は tu1 (Bash, 結果あり) と tu4 (Read, 結果なし) の2件。
	// tu2 は PermissionDeny、tu3 は SubagentCall で、それぞれ後続タスックで扱う
	// (本タスク時点では tu2/tu3 も ToolCall として数えられるため、名前で特定して検証する)
	var bash, read *ToolCall
	for i := range got {
		switch got[i].Tool.Name {
		case "Bash":
			bash = got[i].Tool
		case "Read":
			read = got[i].Tool
		}
	}
	if bash == nil || read == nil {
		t.Fatalf("Bash/Read の ToolCall が見つからない: %+v", got)
	}
	if !bash.HasResult || bash.Result != "file1\nfile2\nfile3" || bash.IsError {
		t.Errorf("Bash: %+v", bash)
	}
	if bash.Summary != "ls -la" {
		t.Errorf("Bash Summary = %q", bash.Summary)
	}
	if read.HasResult || read.Result != "" {
		t.Errorf("結果なし tool_use: %+v", read)
	}
	if read.Summary != "/tmp/y.txt" {
		t.Errorf("Read Summary = %q", read.Summary)
	}
}

func TestToolSummary(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"Bash", `{"command":"go test ./...","description":"テスト"}`, "go test ./..."},
		{"Edit", `{"file_path":"/a/b.go","old_string":"x"}`, "/a/b.go"},
		{"Read", `{"file_path":"/a/c.go"}`, "/a/c.go"},
		{"Agent", `{"description":"調査","prompt":"..."}`, "調査"},
		{"Grep", `{"pattern":"foo","path":"/src"}`, "foo"},
		{"UnknownTool", `{"other":"x"}`, ""},
	}
	for _, c := range cases {
		if got := toolSummary(c.name, json.RawMessage(c.input)); got != c.want {
			t.Errorf("toolSummary(%s) = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestFormatInput(t *testing.T) {
	got := formatInput(json.RawMessage(`{"command":"ls","description":"一覧"}`))
	if !strings.Contains(got, "\n") || !strings.Contains(got, `"command": "ls"`) {
		t.Errorf("整形されていない: %q", got)
	}
}
