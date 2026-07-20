package parse

import "testing"

func TestSystemNoteCompactBoundary(t *testing.T) {
	s := mustParse(t)
	got := eventsOfKind(s, KindSystemNote)
	if len(got) != 1 {
		t.Fatalf("SystemNote %d件", len(got))
	}
	if got[0].Text != "コンテキスト圧縮 (compact)" {
		t.Errorf("Text = %q", got[0].Text)
	}
}

func TestStats(t *testing.T) {
	s := mustParse(t)
	want := Stats{
		UserMessages:      2, // 「こんにちは」+「次のファイルを直して」
		AssistantMessages: 2, // 「確認します」+「コミットします」(空見出しは含まない)
		ToolCalls:         4, // Bash (結果あり) + Read (結果なし) + Edit (相対化パス) + Bash (複数行)
		PermissionDenies:  1, // Edit
		SystemNotes:       1, // compact_boundary
		SubagentCalls:     1, // Agent
		SkillInvocations:  2, // ユーザー呼び出し (/ohayou) + エージェント発動 (Skill tool_use)
		SkippedLines:      1, // 壊れ行
	}
	if s.Stats != want {
		t.Errorf("Stats = %+v, want %+v", s.Stats, want)
	}
}
