package render

import (
	"bytes"
	"flag"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/everyonelovesyou/ccrender/internal/parse"
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
		EndedAt:     at(11),
		Models:      []string{"claude-fable-5", "claude-opus-4-8"},
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
				Diff:       "- old line\n+ new line",
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
			{Kind: parse.KindSkillInvocation, Timestamp: at(7), Skill: &parse.SkillInvocation{
				Name: "ohayou", Path: "/Users/example/.claude/skills/ohayou",
				ByUser: true, Command: "/ohayou 今日も",
			}},
			{Kind: parse.KindSkillInvocation, Timestamp: at(8), Skill: &parse.SkillInvocation{
				Name: "superpowers:brainstorming",
				Path: "/Users/example/plug/skills/brainstorming",
			}},
			// テキストなしで Edit だけ呼ぶターン: 空テキストの見出しが挿入され、
			// ルート配下のパスは相対化されて表示される。
			{Kind: parse.KindAssistantMessage, Timestamp: at(9)},
			{Kind: parse.KindToolCall, Timestamp: at(9), Tool: &parse.ToolCall{
				Name: "Edit", Summary: "internal/render/render.go",
				Input:     "{\n  \"file_path\": \"internal/render/render.go\"\n}",
				Diff:      "- foo\n+ bar",
				HasResult: true, Result: "ok",
			}},
			// heredoc を含む複数行 Bash: フェンス全文 (md) / 全文 copy-src (html) で表示される。
			{Kind: parse.KindAssistantMessage, Timestamp: at(10), Text: "コミットします"},
			{Kind: parse.KindToolCall, Timestamp: at(11), Tool: &parse.ToolCall{
				Name:      "Bash",
				Summary:   "git commit -m \"$(cat <<'EOF'\nfeat: 変更\nEOF\n)\"",
				Input:     "{\n  \"command\": \"git commit -m \\\"$(cat <<'EOF'\\nfeat: 変更\\nEOF\\n)\\\"\"\n}",
				HasResult: true, Result: "[main abc1234] feat: 変更",
			}},
		},
		Stats: parse.Stats{
			UserMessages: 1, AssistantMessages: 2, ToolCalls: 4,
			PermissionDenies: 1, SystemNotes: 1, SubagentCalls: 1,
			SkillInvocations: 2, SkippedLines: 1,
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

// Read の内容は意図的に非表示のため、「(結果なし)」の但し書きも出さない
func TestMarkdownReadShowsNoResultNote(t *testing.T) {
	toolEvent := func(name string) parse.Event {
		return parse.Event{Kind: parse.KindToolCall, Tool: &parse.ToolCall{Name: name, Summary: "/a.go"}}
	}
	s := &parse.Session{ID: "s", ProjectPath: "/p", Events: []parse.Event{toolEvent("Read")}}
	var buf bytes.Buffer
	if err := Markdown(&buf, s, ""); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "(結果なし)") {
		t.Errorf("Read に「(結果なし)」が表示される:\n%s", buf.String())
	}
	s.Events = []parse.Event{toolEvent("Grep")}
	buf.Reset()
	if err := Markdown(&buf, s, ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "(結果なし)") {
		t.Errorf("Read 以外の「(結果なし)」まで消えた:\n%s", buf.String())
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

func TestMarkdownEmptyAssistantHeading(t *testing.T) {
	// テキストなしでツールだけ呼んだターンの assistant_message は見出し行のみで、余分な空行が続かない
	s := &parse.Session{
		ID: "s1", ProjectPath: "/proj",
		Events: []parse.Event{
			{Kind: parse.KindAssistantMessage, Timestamp: time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC)},
			{Kind: parse.KindToolCall, Timestamp: time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC), Tool: &parse.ToolCall{Name: "Edit", Summary: "a.go"}},
		},
	}
	var buf bytes.Buffer
	if err := Markdown(&buf, s, ""); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "## 🤖 Assistant (10:00)") {
		t.Error("見出しが描画されていない")
	}
	if strings.Contains(out, "## 🤖 Assistant (10:00)\n\n\n") {
		t.Error("空見出しの直後に余分な (2つ以上の) 空行が続いている")
	}
	// 見出し直後は他イベント間と同じ1行の空行のみを挟んで次のツール行に接続する
	if !strings.Contains(out, "## 🤖 Assistant (10:00)\n\n🔧 **Edit**") {
		t.Errorf("見出し直後の空行が乱れている:\n%s", out)
	}
}

// 複数行の Bash コマンドはフェンスで全文表示される
func TestMarkdownMultilineCommand(t *testing.T) {
	cmd := "git commit -m \"$(cat <<'EOF'\nfeat: 変更\nEOF\n)\""
	s := &parse.Session{
		ID: "s1", ProjectPath: "/proj",
		Events: []parse.Event{
			{Kind: parse.KindToolCall, Tool: &parse.ToolCall{Name: "Bash", Summary: cmd, HasResult: true, Result: "ok"}},
		},
	}
	out := renderMarkdown(t, s)
	if !strings.Contains(out, "feat: 変更") {
		t.Error("複数行コマンドの2行目以降が出力されていない")
	}
}

func TestMarkdownPermissionDenyShowsDiff(t *testing.T) {
	// 「何を拒否されたか」が一番見たい情報なので、拒否された Edit でも diff を描画する
	s := &parse.Session{ID: "x", Events: []parse.Event{
		{Kind: parse.KindPermissionDeny, Tool: &parse.ToolCall{
			Name:       "Edit",
			Summary:    "/tmp/x.txt",
			Diff:       "- 消される行\n+ 足される行",
			IsError:    true,
			HasResult:  true,
			DenyReason: "こっちは触らないで",
		}},
	}}
	out := renderMarkdown(t, s)
	if !strings.Contains(out, "```diff") {
		t.Errorf("拒否ブロックに diff フェンスがない:\n%s", out)
	}
	if !strings.Contains(out, "- 消される行") || !strings.Contains(out, "+ 足される行") {
		t.Errorf("diff の中身が描画されていない:\n%s", out)
	}
}

func TestMarkdownPermissionDenyWithoutDiff(t *testing.T) {
	// Edit 以外の拒否では Diff が空なので、空のフェンスを出さない
	s := &parse.Session{ID: "x", Events: []parse.Event{
		{Kind: parse.KindPermissionDeny, Tool: &parse.ToolCall{
			Name: "Bash", Summary: "rm -rf /", IsError: true, HasResult: true,
		}},
	}}
	if out := renderMarkdown(t, s); strings.Contains(out, "```diff") {
		t.Errorf("Diff が空なのに diff フェンスが描画されている:\n%s", out)
	}
}

// renderMarkdown はデフォルトテンプレートで s を markdown に整形して返す (テスト用)。
func renderMarkdown(t *testing.T, s *parse.Session) string {
	t.Helper()
	var buf bytes.Buffer
	if err := Markdown(&buf, s, ""); err != nil {
		t.Fatal(err)
	}
	return buf.String()
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
