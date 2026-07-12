package render

import (
	"bytes"
	"flag"
	"os"
	"testing"
	"time"

	"cctx/internal/parse"
)

var update = flag.Bool("update", false, "golden ファイルを更新する")

// fixtureSession はレンダリングテスト用の Session リテラル。parse には依存しない。
func fixtureSession() *parse.Session {
	t0 := time.Date(2026, 7, 12, 0, 0, 1, 0, time.UTC)
	at := func(sec int) time.Time { return t0.Add(time.Duration(sec) * time.Second) }
	return &parse.Session{
		ID:          "sess-0001",
		ProjectPath: "/Users/example/proj",
		StartedAt:   at(0),
		EndedAt:     at(8),
		Events: []parse.Event{
			{Kind: parse.KindUserMessage, Timestamp: at(0), Text: "こんにちは"},
			{Kind: parse.KindAssistantMessage, Timestamp: at(1), Text: "確認します"},
			{Kind: parse.KindToolCall, Timestamp: at(2), Tool: &parse.ToolCall{
				Name: "Bash", Summary: "seq 1 25", Input: "{\n  \"command\": \"seq 1 25\"\n}",
				HasResult: true,
				Result:    "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n13\n14\n15\n16\n17\n18\n19\n20\n21\n22\n23\n24\n25",
			}},
			{Kind: parse.KindPermissionDeny, Timestamp: at(3), Tool: &parse.ToolCall{
				Name: "Edit", Summary: "/tmp/x.txt", IsError: true, HasResult: true,
				DenyReason: "こっちは触らないで",
			}},
			{Kind: parse.KindSubagentCall, Timestamp: at(4), Subagent: &parse.Subagent{
				AgentID: "abc123", AgentType: "general-purpose",
				Prompt: "Please investigate.", Answer: "Investigation summary.",
			}},
			{Kind: parse.KindSystemNote, Timestamp: at(5), Text: "コンテキスト圧縮 (compact)"},
			{Kind: parse.KindToolCall, Timestamp: at(6), Tool: &parse.ToolCall{
				Name: "Read", Summary: "/tmp/y.txt", Input: "{}", HasResult: false,
			}},
		},
		Stats: parse.Stats{
			UserMessages: 1, AssistantMessages: 1, ToolCalls: 2,
			PermissionDenies: 1, SystemNotes: 1, SubagentCalls: 1, SkippedLines: 1,
		},
	}
}

func TestMarkdownGolden(t *testing.T) {
	var buf bytes.Buffer
	if err := Markdown(&buf, fixtureSession(), ""); err != nil {
		t.Fatal(err)
	}
	compareGolden(t, "testdata/golden.md", buf.Bytes())
}

func TestMarkdownEmptySession(t *testing.T) {
	s := &parse.Session{ID: "empty", ProjectPath: "/p"}
	var buf bytes.Buffer
	if err := Markdown(&buf, s, ""); err != nil {
		t.Fatalf("空セッションでエラー: %v", err)
	}
}

func TestMarkdownToolOnlySession(t *testing.T) {
	s := fixtureSession()
	var events []parse.Event
	for _, e := range s.Events {
		if e.Kind == parse.KindToolCall {
			events = append(events, e)
		}
	}
	s.Events = events
	var buf bytes.Buffer
	if err := Markdown(&buf, s, ""); err != nil {
		t.Fatalf("ツールのみセッションでエラー: %v", err)
	}
}

func TestMarkdownBadOverrideTemplate(t *testing.T) {
	dir := t.TempDir()
	bad := dir + "/bad.tmpl"
	if err := os.WriteFile(bad, []byte("{{.Unclosed"), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := Markdown(&buf, fixtureSession(), bad); err == nil {
		t.Fatal("構文エラーのテンプレートが通った")
	}
	if err := Markdown(&buf, fixtureSession(), dir+"/no-such.tmpl"); err == nil {
		t.Fatal("存在しないテンプレートが通った")
	}
}

func compareGolden(t *testing.T, path string, got []byte) {
	t.Helper()
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden がありません。go test ./internal/render/ -update で生成してください: %v", err)
	}
	if !bytes.Equal(want, got) {
		t.Errorf("golden と不一致。差分確認後 -update で更新:\n--- got ---\n%s", got)
	}
}
