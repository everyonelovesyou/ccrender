package parse

import (
	"os"
	"strings"
	"testing"
	"time"
)

const fixture = "testdata/session_small.jsonl"

func eventsOfKind(s *Session, k EventKind) []Event {
	var out []Event
	for _, e := range s.Events {
		if e.Kind == k {
			out = append(out, e)
		}
	}
	return out
}

func mustParse(t *testing.T) *Session {
	t.Helper()
	s, err := ParseFile(fixture, nil)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	return s
}

func TestSessionHeader(t *testing.T) {
	s := mustParse(t)
	if s.ID != "sess-0001" {
		t.Errorf("ID = %q", s.ID)
	}
	if s.ProjectPath != "/Users/example/proj" {
		t.Errorf("ProjectPath = %q", s.ProjectPath)
	}
	wantStart := time.Date(2026, 7, 12, 0, 0, 1, 0, time.UTC)
	if !s.StartedAt.Equal(wantStart) {
		t.Errorf("StartedAt = %v", s.StartedAt)
	}
	// 末尾は複数行 Bash ターンの tool_result (00:00:19Z)
	wantEnd := time.Date(2026, 7, 12, 0, 0, 19, 0, time.UTC)
	if !s.EndedAt.Equal(wantEnd) {
		t.Errorf("EndedAt = %v", s.EndedAt)
	}
	if s.Stats.SkippedLines != 1 {
		t.Errorf("SkippedLines = %d", s.Stats.SkippedLines)
	}
}

func TestUserMessages(t *testing.T) {
	s := mustParse(t)
	got := eventsOfKind(s, KindUserMessage)
	// 「こんにちは」に加え、テキストなし Edit ターンの直前にある実発話
	// 「次のファイルを直して」の2件。
	if len(got) != 2 {
		t.Fatalf("UserMessage %d件: %+v", len(got), got)
	}
	if got[0].Text != "こんにちは" {
		t.Errorf("ノイズ除去後 Text = %q", got[0].Text)
	}
	if got[1].Text != "次のファイルを直して" {
		t.Errorf("2件目の Text = %q", got[1].Text)
	}
}

func TestAssistantMessages(t *testing.T) {
	s := mustParse(t)
	got := eventsOfKind(s, KindAssistantMessage)
	// 本文付きの2件 (「確認します」「コミットします」) に加え、
	// system_note (compact) 直後のツール呼び出し、エージェント発動 skill_invocation の前、
	// テキストなし Edit ターンの前に空テキストの見出しが挿入される。
	if len(got) != 5 || got[0].Text != "確認します" {
		t.Fatalf("AssistantMessage: %+v", got)
	}
	if got[1].Text != "" || got[2].Text != "" || got[3].Text != "" {
		t.Errorf("挿入された見出しの Text は空であるべき: %+v", got[1:4])
	}
	if got[4].Text != "コミットします" {
		t.Errorf("末尾のテキスト付きメッセージ = %q", got[4].Text)
	}
}

func TestAllLinesBrokenFails(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/broken.jsonl"
	if err := writeFile(path, "{oops\n{also broken\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseFile(path, nil); err == nil {
		t.Fatal("全行破損でエラーにならなかった")
	}
}

func TestNoiseStrip(t *testing.T) {
	in := "本文\n<system-reminder>a</system-reminder>\n<command-name>/x</command-name>\n<local-command-stdout>y</local-command-stdout>"
	if got := stripNoise(in); got != "本文" {
		t.Errorf("stripNoise = %q", got)
	}
	if got := stripNoise("<system-reminder>only noise</system-reminder>"); got != "" {
		t.Errorf("全ノイズで空にならない: %q", got)
	}
	if !strings.Contains(stripNoise("<system-reminder>削除</system-reminder>残す"), "残す") {
		t.Error("ノイズ以外まで消えた")
	}
}

// task-notification はユーザー発話ではないため丸ごと除去する
func TestTaskNotificationDropped(t *testing.T) {
	if got := stripNoise("<task-notification>\n<task-id>x</task-id>\n<summary>done</summary>\n</task-notification>"); got != "" {
		t.Errorf("task-notification が残った: %q", got)
	}
	jsonl := `{"type":"user","sessionId":"s1","timestamp":"2026-07-18T00:00:00Z","message":{"role":"user","content":"<task-notification>\n<task-id>x</task-id>\n</task-notification>"}}` + "\n"
	s := parseString(t, jsonl)
	if got := eventsOfKind(s, KindUserMessage); len(got) != 0 {
		t.Errorf("task-notification がユーザー発言として描画される: %+v", got)
	}
}

// timestamp は UTC で記録されるため、表示用にローカル時刻へ変換する
func TestParseTimeLocal(t *testing.T) {
	orig := time.Local
	time.Local = time.FixedZone("JST", 9*3600)
	defer func() { time.Local = orig }()
	got := parseTime("2026-07-18T03:00:00Z")
	if got.Format("15:04") != "12:00" {
		t.Errorf("ローカル時刻 = %q, want 12:00", got.Format("15:04"))
	}
}

// ユーザーのシェル実行 (! prefix) はタグを剥がして整形する
func TestBashTagsFormatted(t *testing.T) {
	if got := stripNoise("<bash-input>ls -la</bash-input>"); got != "$ ls -la" {
		t.Errorf("bash-input = %q, want %q", got, "$ ls -la")
	}
	if got := stripNoise("<bash-stdout>file1\nfile2</bash-stdout><bash-stderr></bash-stderr>"); got != "file1\nfile2" {
		t.Errorf("bash-stdout = %q, want %q", got, "file1\nfile2")
	}
	if got := stripNoise("<bash-stdout></bash-stdout><bash-stderr>oops</bash-stderr>"); got != "oops" {
		t.Errorf("bash-stderr = %q, want %q", got, "oops")
	}
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

// parseString は jsonl 文字列を一時ファイルに書いて ParseFile を呼ぶ。
func parseString(t *testing.T, jsonl string) *Session {
	t.Helper()
	dir := t.TempDir()
	path := dir + "/session.jsonl"
	if err := writeFile(path, jsonl); err != nil {
		t.Fatal(err)
	}
	s, err := ParseFile(path, nil)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	return s
}

// テキストなしでツールだけ呼んだターンにも assistant_message (空テキスト) が挿入される
func TestHeadingInsertedForToolOnlyTurn(t *testing.T) {
	jsonl := `{"type":"user","sessionId":"s1","timestamp":"2026-07-15T10:00:00Z","cwd":"/proj","message":{"role":"user","content":"直して"}}
{"type":"assistant","sessionId":"s1","timestamp":"2026-07-15T10:00:05Z","message":{"role":"assistant","model":"claude-fable-5","content":[{"type":"tool_use","id":"t1","name":"Edit","input":{"file_path":"/proj/a.go"}}]}}
{"type":"user","sessionId":"s1","timestamp":"2026-07-15T10:00:06Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}}
`
	s := parseString(t, jsonl)
	kinds := eventKinds(s.Events)
	want := []string{"user_message", "assistant_message", "tool_call"}
	if !equalStrings(kinds, want) {
		t.Fatalf("kinds = %v, want %v", kinds, want)
	}
	if s.Events[1].Text != "" {
		t.Errorf("挿入された見出しの Text = %q, want empty", s.Events[1].Text)
	}
	if s.Stats.AssistantMessages != 0 {
		t.Errorf("空見出しは AssistantMessages に数えない: got %d", s.Stats.AssistantMessages)
	}
}

// テキスト付きターンでは見出しが重複しない
func TestNoDuplicateHeadingWhenTextPresent(t *testing.T) {
	jsonl := `{"type":"user","sessionId":"s1","timestamp":"2026-07-15T10:00:00Z","cwd":"/proj","message":{"role":"user","content":"直して"}}
{"type":"assistant","sessionId":"s1","timestamp":"2026-07-15T10:00:05Z","message":{"role":"assistant","model":"claude-fable-5","content":[{"type":"text","text":"直します"}]}}
{"type":"assistant","sessionId":"s1","timestamp":"2026-07-15T10:00:06Z","message":{"role":"assistant","model":"claude-fable-5","content":[{"type":"tool_use","id":"t1","name":"Edit","input":{"file_path":"/proj/a.go"}}]}}
{"type":"user","sessionId":"s1","timestamp":"2026-07-15T10:00:07Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}}
`
	s := parseString(t, jsonl)
	kinds := eventKinds(s.Events)
	want := []string{"user_message", "assistant_message", "tool_call"}
	if !equalStrings(kinds, want) {
		t.Fatalf("kinds = %v, want %v", kinds, want)
	}
}

func eventKinds(events []Event) []string {
	var ks []string
	for _, e := range events {
		ks = append(ks, e.Kind)
	}
	return ks
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSessionModels(t *testing.T) {
	jsonl := `{"type":"user","sessionId":"s1","timestamp":"2026-07-15T10:00:00Z","cwd":"/proj","message":{"role":"user","content":"こんにちは"}}
{"type":"assistant","sessionId":"s1","timestamp":"2026-07-15T10:00:05Z","message":{"role":"assistant","model":"claude-fable-5","content":[{"type":"text","text":"はい"}]}}
{"type":"assistant","sessionId":"s1","timestamp":"2026-07-15T10:00:10Z","message":{"role":"assistant","model":"claude-fable-5","content":[{"type":"text","text":"続き"}]}}
{"type":"assistant","sessionId":"s1","timestamp":"2026-07-15T10:01:00Z","message":{"role":"assistant","model":"claude-opus-4-8","content":[{"type":"text","text":"切替後"}]}}
`
	s := parseString(t, jsonl)
	want := []string{"claude-fable-5", "claude-opus-4-8"}
	if len(s.Models) != len(want) {
		t.Fatalf("Models = %v, want %v", s.Models, want)
	}
	for i := range want {
		if s.Models[i] != want[i] {
			t.Errorf("Models[%d] = %q, want %q", i, s.Models[i], want[i])
		}
	}
}
