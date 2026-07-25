package parse

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"
)

func TestToolEventSetsWriteDiff(t *testing.T) {
	newEvent := func(name, input string) Event {
		return toolEvent(
			contentBlock{Type: "tool_use", ID: "tu-x", Name: name, Input: json.RawMessage(input)},
			time.Time{}, "", map[string]contentBlock{}, map[string]Subagent{}, skillIndex{}, io.Discard,
		)
	}
	write := newEvent("Write", `{"file_path":"/tmp/a.txt","content":"foo\nbar"}`)
	if write.Tool.Diff != "+ foo\n+ bar" {
		t.Errorf("Write の Diff が設定されていない: %q", write.Tool.Diff)
	}
	// Write と入力の形が違う NotebookEdit / MultiEdit は対象外のまま
	notebook := newEvent("NotebookEdit", `{"file_path":"/tmp/a.ipynb","new_source":"foo"}`)
	if notebook.Tool.Diff != "" {
		t.Errorf("対象外のツールに Diff が設定されている: %q", notebook.Tool.Diff)
	}
}

func TestToolEventSetsReadRange(t *testing.T) {
	newEvent := func(name, input string) Event {
		return toolEvent(
			contentBlock{Type: "tool_use", ID: "tu-x", Name: name, Input: json.RawMessage(input)},
			time.Time{}, "/proj", map[string]contentBlock{}, map[string]Subagent{}, skillIndex{}, io.Discard,
		)
	}
	read := newEvent("Read", `{"file_path":"/proj/a.go","offset":17,"limit":30}`)
	if read.Tool.Range != "L 17〜46" {
		t.Errorf("Read の Range が設定されていない: %q", read.Tool.Range)
	}
	// Range は Summary と独立している (パスのコピーを濁さない)
	if read.Tool.Summary != "a.go" {
		t.Errorf("Summary に範囲が混ざっている: %q", read.Tool.Summary)
	}
	whole := newEvent("Read", `{"file_path":"/proj/a.go"}`)
	if whole.Tool.Range != "" {
		t.Errorf("範囲指定のない Read に Range が設定されている: %q", whole.Tool.Range)
	}
}

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
	if edit.Diff != "- foo\n+ bar" {
		t.Errorf("Edit の Diff が設定されていない: %q", edit.Diff)
	}
	if bash.Diff != "" {
		t.Errorf("Edit 以外のツールに Diff が設定されている: %q", bash.Diff)
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
		// 読み取り範囲は Summary ではなく Range が持つ (パスのコピーを濁さないため)
		{name: "Read", input: `{"file_path":"/a/c.go","offset":17,"limit":30}`, root: "", want: "/a/c.go"},
	}
	for _, c := range cases {
		if got := toolSummary(c.name, json.RawMessage(c.input), c.root); got != c.want {
			t.Errorf("toolSummary(%s) = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestToolRange(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{name: "Read", input: `{"file_path":"/a/c.go","offset":17,"limit":30}`, want: "L 17〜46"},
		{name: "Read", input: `{"file_path":"/a/c.go","limit":30}`, want: "L 1〜30"},
		{name: "Read", input: `{"file_path":"/a/c.go","offset":105}`, want: "L 105〜"},
		{name: "Read", input: `{"file_path":"/a/c.go","offset":1,"limit":30}`, want: "L 1〜30"},
		// 範囲として意味をなさない値は付記しない
		{name: "Read", input: `{"file_path":"/a/c.go"}`, want: ""},
		{name: "Read", input: `{"file_path":"/a/c.go","offset":0,"limit":0}`, want: ""},
		{name: "Read", input: `{"file_path":"/a/c.go","offset":-3}`, want: ""},
		{name: "Read", input: `壊れた JSON`, want: ""},
		// 範囲を持つのは Read だけ (他ツールの同名キーには反応しない)
		{name: "Edit", input: `{"file_path":"/a/c.go","offset":17,"limit":30}`, want: ""},
	}
	for _, c := range cases {
		if got := toolRange(c.name, json.RawMessage(c.input)); got != c.want {
			t.Errorf("toolRange(%s, %s) = %q, want %q", c.name, c.input, got, c.want)
		}
	}
}

// 結果を描画するかは render 層の判断 (ShowsResult)。parse は成功・失敗どちらも保持する
func TestReadResultRetained(t *testing.T) {
	jsonl := `{"type":"assistant","sessionId":"s1","timestamp":"2026-07-18T00:00:00Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"r1","name":"Read","input":{"file_path":"/a.go"}},{"type":"tool_use","id":"r2","name":"Read","input":{"file_path":"/b.go"}}]}}` + "\n" +
		`{"type":"user","sessionId":"s1","timestamp":"2026-07-18T00:00:01Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"r1","content":"1\tpackage main"},{"type":"tool_result","tool_use_id":"r2","is_error":true,"content":"File does not exist."}]}}` + "\n"
	s := parseString(t, jsonl)
	got := eventsOfKind(s, KindToolCall)
	if len(got) != 2 {
		t.Fatalf("ToolCall %d件: %+v", len(got), got)
	}
	if !got[0].Tool.HasResult || got[0].Tool.Result != "1\tpackage main" {
		t.Errorf("Read 成功結果が parse 層で捨てられている: %+v", got[0].Tool)
	}
	if !got[1].Tool.HasResult || !got[1].Tool.IsError {
		t.Errorf("Read エラー結果は残すべき: %+v", got[1].Tool)
	}
}

func TestFormatInput(t *testing.T) {
	got := formatInput(json.RawMessage(`{"command":"ls","description":"一覧"}`))
	if !strings.Contains(got, "\n") || !strings.Contains(got, `"command": "ls"`) {
		t.Errorf("整形されていない: %q", got)
	}
}
