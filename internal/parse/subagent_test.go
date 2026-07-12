package parse

import (
	"bytes"
	"os"
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

func TestSubagentDeniedIsPermissionDeny(t *testing.T) {
	// Agent tool_use が拒否された場合は SubagentCall ではなく PermissionDeny として扱う
	dir := t.TempDir()
	path := dir + "/denied-agent.jsonl"
	lines := `{"type":"assistant","uuid":"a1","isSidechain":false,"cwd":"/p","sessionId":"s1","timestamp":"2026-07-12T00:00:01Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"tux","name":"Agent","input":{"description":"調査","prompt":"do it","subagent_type":"general-purpose"}}]}}
{"type":"user","uuid":"u1","isSidechain":false,"cwd":"/p","sessionId":"s1","timestamp":"2026-07-12T00:00:02Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tux","is_error":true,"content":"The user doesn't want to proceed with this tool use. The tool use was rejected. To tell you how to proceed, the user said:\nこのエージェントは呼ばないで\n\nNote: The user's next message may contain a correction."}]}}
`
	if err := writeFile(path, lines); err != nil {
		t.Fatal(err)
	}
	var warn bytes.Buffer
	s, err := ParseFile(path, &warn)
	if err != nil {
		t.Fatal(err)
	}
	if got := eventsOfKind(s, KindSubagentCall); len(got) != 0 {
		t.Fatalf("SubagentCall として扱われてはいけない: %+v", got)
	}
	got := eventsOfKind(s, KindPermissionDeny)
	if len(got) != 1 {
		t.Fatalf("PermissionDeny %d件", len(got))
	}
	tc := got[0].Tool
	if tc == nil || tc.Name != "Agent" || tc.DenyReason != "このエージェントは呼ばないで" {
		t.Errorf("PermissionDeny: %+v", tc)
	}
	if strings.Contains(warn.String(), "サブエージェント記録が見つかりません") {
		t.Errorf("拒否された Agent に対して誤ったサブエージェント警告が出た: %q", warn.String())
	}
}

func TestSubagentAnswerErrorsWithoutAssistantText(t *testing.T) {
	cases := []struct {
		name  string
		lines string
	}{
		{"assistant レコードがない",
			`{"type":"user","uuid":"su1","isSidechain":true,"cwd":"/p","sessionId":"s1","timestamp":"2026-07-12T00:00:01Z","message":{"role":"user","content":"do it"}}
`},
		{"assistant はあるが text ブロックがない",
			`{"type":"user","uuid":"su1","isSidechain":true,"cwd":"/p","sessionId":"s1","timestamp":"2026-07-12T00:00:01Z","message":{"role":"user","content":"do it"}}
{"type":"assistant","uuid":"sa1","isSidechain":true,"cwd":"/p","sessionId":"s1","timestamp":"2026-07-12T00:00:02Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"stu1","name":"Bash","input":{"command":"ls"}}]}}
`},
	}
	for _, c := range cases {
		path := t.TempDir() + "/agent-x.jsonl"
		if err := writeFile(path, c.lines); err != nil {
			t.Fatal(err)
		}
		if _, err := subagentAnswer(path); err == nil {
			t.Errorf("%s: エラーになっていない", c.name)
		}
	}
}

func TestSubagentEmptyRecordFallsBackWithWarning(t *testing.T) {
	// subagents/ の記録はあるが assistant テキストが空: 本流 tool_result で縮退し警告
	dir := t.TempDir()
	path := dir + "/empty-sub.jsonl"
	lines := `{"type":"assistant","uuid":"a1","isSidechain":false,"cwd":"/p","sessionId":"s1","timestamp":"2026-07-12T00:00:01Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"tux","name":"Agent","input":{"description":"調査","prompt":"do it","subagent_type":"general-purpose"}}]}}
{"type":"user","uuid":"u1","isSidechain":false,"cwd":"/p","sessionId":"s1","timestamp":"2026-07-12T00:00:02Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tux","is_error":false,"content":[{"type":"text","text":"main answer"}]}]}}
`
	if err := writeFile(path, lines); err != nil {
		t.Fatal(err)
	}
	subDir := dir + "/empty-sub/subagents"
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(subDir+"/agent-xyz.meta.json", `{"agentType":"general-purpose","toolUseId":"tux"}`); err != nil {
		t.Fatal(err)
	}
	// assistant テキストが一件もないサブエージェント JSONL
	if err := writeFile(subDir+"/agent-xyz.jsonl", `{"type":"user","uuid":"su1","isSidechain":true,"cwd":"/p","sessionId":"s1","timestamp":"2026-07-12T00:00:01Z","message":{"role":"user","content":"do it"}}
`); err != nil {
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
