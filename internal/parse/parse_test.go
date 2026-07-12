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
	// 末尾はエージェント発動のスキル展開エントリ (00:00:14Z)
	wantEnd := time.Date(2026, 7, 12, 0, 0, 14, 0, time.UTC)
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
	if len(got) != 1 {
		t.Fatalf("UserMessage %d件: %+v", len(got), got)
	}
	if got[0].Text != "こんにちは" {
		t.Errorf("ノイズ除去後 Text = %q", got[0].Text)
	}
}

func TestAssistantMessages(t *testing.T) {
	s := mustParse(t)
	got := eventsOfKind(s, KindAssistantMessage)
	if len(got) != 1 || got[0].Text != "確認します" {
		t.Fatalf("AssistantMessage: %+v", got)
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

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}
