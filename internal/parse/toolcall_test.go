package parse

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestToolCallMatched(t *testing.T) {
	s := mustParse(t)
	got := eventsOfKind(s, KindToolCall)
	// fixture 中の ToolCall は tu1 (Bash, 結果あり)、tu4 (Read, 結果なし)、
	// tu6 (Edit, ルート配下パスを相対化)、tu7 (Bash, heredoc を含む複数行) の4件。
	// tu2 は PermissionDeny、tu3 は SubagentCall で、それぞれ後続タスックで扱う
	// (本タスク時点では tu2/tu3 も ToolCall として数えられるため、名前で特定して検証する)
	var bash, read, edit, multilineBash *ToolCall
	for i := range got {
		tc := got[i].Tool
		switch {
		case tc.Name == "Bash" && tc.Summary == "ls -la":
			bash = tc
		case tc.Name == "Bash":
			multilineBash = tc
		case tc.Name == "Read":
			read = tc
		case tc.Name == "Edit":
			edit = tc
		}
	}
	if bash == nil || read == nil || edit == nil || multilineBash == nil {
		t.Fatalf("Bash/Read/Edit/複数行 Bash の ToolCall が見つからない: %+v", got)
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
	if edit.Summary != "internal/render/render.go" {
		t.Errorf("ルート配下パスが相対化されていない: Edit Summary = %q", edit.Summary)
	}
	wantMultiline := "git commit -m \"$(cat <<'EOF'\nfeat: 変更\nEOF\n)\""
	if multilineBash.Summary != wantMultiline {
		t.Errorf("複数行 Bash Summary = %q, want %q", multilineBash.Summary, wantMultiline)
	}
}

func TestToolSummary(t *testing.T) {
	cases := []struct {
		name  string
		input string
		root  string
		want  string
	}{
		{name: "Bash", input: `{"command":"go test ./...","description":"テスト"}`, root: "", want: "go test ./..."},
		{name: "Edit", input: `{"file_path":"/a/b.go","old_string":"x"}`, root: "", want: "/a/b.go"},
		{name: "Read", input: `{"file_path":"/a/c.go"}`, root: "", want: "/a/c.go"},
		{name: "Agent", input: `{"description":"調査","prompt":"..."}`, root: "", want: "調査"},
		{name: "Grep", input: `{"pattern":"foo","path":"/src"}`, root: "", want: "foo"},
		{name: "UnknownTool", input: `{"other":"x"}`, root: "", want: ""},
		{name: "Edit", input: `{"file_path":"/proj/internal/a.go"}`, root: "/proj", want: "internal/a.go"},
		{name: "Read", input: `{"file_path":"/other/b.go"}`, root: "/proj", want: "/other/b.go"},
		{name: "Bash", input: `{"command":"cat /proj/internal/a.go"}`, root: "/proj", want: "cat /proj/internal/a.go"},
		{name: "Grep", input: `{"path":"/proj/internal"}`, root: "/proj", want: "internal"},
		{name: "Edit", input: `{"file_path":"/proj"}`, root: "/proj", want: "/proj"},
	}
	for _, c := range cases {
		if got := toolSummary(c.name, json.RawMessage(c.input), c.root); got != c.want {
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
