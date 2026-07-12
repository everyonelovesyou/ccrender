package parse

import (
	"bytes"
	"strings"
	"testing"
)

func TestSubagentCall(t *testing.T) {
	s := mustParse(t)
	got := eventsOfKind(s, KindSubagentCall)
	if len(got) != 1 {
		t.Fatalf("SubagentCall %d件", len(got))
	}
	sub := got[0].Subagent
	if sub.AgentID != "abc123" || sub.AgentType != "general-purpose" {
		t.Errorf("メタ情報: %+v", sub)
	}
	if sub.Prompt != "Please investigate the repository structure." {
		t.Errorf("Prompt = %q", sub.Prompt)
	}
	// 最終回答は本流 tool_result (agentId 行入り) ではなく subagents/ の最後の assistant テキスト
	if sub.Answer != "Investigation summary: the repo has 3 packages." {
		t.Errorf("Answer = %q", sub.Answer)
	}
}

func TestSubagentMissingFallsBackWithWarning(t *testing.T) {
	// subagents/ が無いセッション: 本流 tool_result で縮退し stderr に警告
	dir := t.TempDir()
	path := dir + "/no-sub.jsonl"
	lines := `{"type":"assistant","uuid":"a1","isSidechain":false,"cwd":"/p","sessionId":"s1","timestamp":"2026-07-12T00:00:01Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"tux","name":"Agent","input":{"description":"調査","prompt":"do it","subagent_type":"general-purpose"}}]}}
{"type":"user","uuid":"u1","isSidechain":false,"cwd":"/p","sessionId":"s1","timestamp":"2026-07-12T00:00:02Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tux","is_error":false,"content":[{"type":"text","text":"main answer"}]}]}}
`
	if err := writeFile(path, lines); err != nil {
		t.Fatal(err)
	}
	var warn bytes.Buffer
	s, err := ParseFile(path, &warn)
	if err != nil {
		t.Fatal(err)
	}
	got := eventsOfKind(s, KindSubagentCall)
	if len(got) != 1 || got[0].Subagent.Answer != "main answer" {
		t.Fatalf("縮退動作: %+v", got)
	}
	if !strings.Contains(warn.String(), "サブエージェント記録が見つかりません") {
		t.Errorf("警告が出ていない: %q", warn.String())
	}
}

func TestLoadSubagents(t *testing.T) {
	subs := loadSubagents("testdata/session_small/subagents")
	sub, ok := subs["tu3"]
	if !ok {
		t.Fatalf("toolUseId=tu3 が見つからない: %+v", subs)
	}
	if sub.AgentID != "abc123" || sub.Answer == "" {
		t.Errorf("loadSubagents: %+v", sub)
	}
	if got := loadSubagents("testdata/no-such-dir"); len(got) != 0 {
		t.Errorf("存在しないディレクトリ: %+v", got)
	}
}
