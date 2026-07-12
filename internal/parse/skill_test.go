package parse

import (
	"encoding/json"
	"strings"
	"testing"
)

// mustRecord は JSONL 1行分の文字列を rawRecord へデコードする。
func mustRecord(t *testing.T, line string) rawRecord {
	t.Helper()
	var rec rawRecord
	if err := json.Unmarshal([]byte(line), &rec); err != nil {
		t.Fatalf("rawRecord のデコード失敗: %v", err)
	}
	return rec
}

func TestSkillExpansionPath(t *testing.T) {
	cases := []struct {
		name     string
		line     string
		wantPath string
		wantOK   bool
	}{
		{
			"文字列 content の展開エントリ",
			`{"type":"user","isMeta":true,"message":{"role":"user","content":"Base directory for this skill: /Users/example/.claude/skills/ohayou\n\n# 手順"}}`,
			"/Users/example/.claude/skills/ohayou", true,
		},
		{
			"ブロック配列 content の展開エントリ",
			`{"type":"user","isMeta":true,"message":{"role":"user","content":[{"type":"text","text":"Base directory for this skill: /Users/example/.claude/skills/ohayou\n本文"}]}}`,
			"/Users/example/.claude/skills/ohayou", true,
		},
		{
			"行末の \\r と余分な空白を除去する",
			`{"type":"user","isMeta":true,"message":{"role":"user","content":"Base directory for this skill: /Users/example/.claude/skills/ohayou \r\n本文"}}`,
			"/Users/example/.claude/skills/ohayou", true,
		},
		{
			"isMeta でなければ対象外",
			`{"type":"user","message":{"role":"user","content":"Base directory for this skill: /x"}}`,
			"", false,
		},
		{
			"接頭辞が一致しなければ対象外",
			`{"type":"user","isMeta":true,"message":{"role":"user","content":"ただの isMeta エントリ"}}`,
			"", false,
		},
		{
			"assistant レコードは対象外",
			`{"type":"assistant","isMeta":true,"message":{"role":"assistant","content":"Base directory for this skill: /x"}}`,
			"", false,
		},
	}
	for _, c := range cases {
		got, ok := skillExpansionPath(mustRecord(t, c.line))
		if ok != c.wantOK || got != c.wantPath {
			t.Errorf("%s: skillExpansionPath = (%q, %v), want (%q, %v)", c.name, got, ok, c.wantPath, c.wantOK)
		}
	}
}

func TestCollectSkillIndex(t *testing.T) {
	records := []rawRecord{
		// ユーザー呼び出しのコマンドエントリ (引数付き)
		mustRecord(t, `{"type":"user","uuid":"cmd1","message":{"role":"user","content":"<command-message>ohayou</command-message>\n<command-name>/ohayou</command-name>\n<command-args>今日も</command-args>"}}`),
		// 引数が空白のみのコマンドエントリ → 引数なし扱い
		mustRecord(t, `{"type":"user","uuid":"cmd2","message":{"role":"user","content":"<command-name>/clear</command-name>\n<command-args>   </command-args>"}}`),
		// エージェント発動の展開エントリ
		mustRecord(t, `{"type":"user","uuid":"sk1","isMeta":true,"sourceToolUseID":"tu9","message":{"role":"user","content":"Base directory for this skill: /Users/example/plug/skills/brainstorming\n本文"}}`),
		// ユーザー呼び出しの展開エントリ (sourceToolUseID なし) は expansions に入らない
		mustRecord(t, `{"type":"user","uuid":"sk2","isMeta":true,"parentUuid":"cmd1","message":{"role":"user","content":"Base directory for this skill: /Users/example/.claude/skills/ohayou\n本文"}}`),
		// 通常発話は commands に入らない
		mustRecord(t, `{"type":"user","uuid":"u1","message":{"role":"user","content":"こんにちは"}}`),
	}
	idx := collectSkillIndex(records)

	if got := idx.expansions["tu9"]; got != "/Users/example/plug/skills/brainstorming" {
		t.Errorf("expansions[tu9] = %q", got)
	}
	if len(idx.expansions) != 1 {
		t.Errorf("expansions の件数 = %d, want 1", len(idx.expansions))
	}
	if got := idx.commands["cmd1"]; got.name != "/ohayou" || got.args != "今日も" {
		t.Errorf("commands[cmd1] = %+v", got)
	}
	if got := idx.commands["cmd2"]; got.name != "/clear" || got.args != "" {
		t.Errorf("空白のみの args が引数なしになっていない: %+v", got)
	}
	if _, ok := idx.commands["u1"]; ok {
		t.Error("通常発話が commands に入っている")
	}
}

// parseLines は JSONL 行群を一時ファイルに書いて ParseFile する。
func parseLines(t *testing.T, lines ...string) *Session {
	t.Helper()
	path := t.TempDir() + "/s.jsonl"
	if err := writeFile(path, strings.Join(lines, "\n")+"\n"); err != nil {
		t.Fatal(err)
	}
	s, err := ParseFile(path, nil)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	return s
}

const (
	agentSkillUse       = `{"type":"assistant","uuid":"a1","isSidechain":false,"cwd":"/p","sessionId":"s1","timestamp":"2026-07-12T00:00:01Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"tu1","name":"Skill","input":{"skill":"superpowers:brainstorming"}}]}}`
	agentSkillExpansion = `{"type":"user","uuid":"sk1","parentUuid":"a1","sourceToolUseID":"tu1","isMeta":true,"isSidechain":false,"cwd":"/p","sessionId":"s1","timestamp":"2026-07-12T00:00:02Z","message":{"role":"user","content":[{"type":"text","text":"Base directory for this skill: /Users/example/plug/skills/brainstorming\n\n# 手順\n\nこの本文は出力に現れてはならない"}]}}`
)

func TestAgentSkillInvocation(t *testing.T) {
	s := parseLines(t, agentSkillUse, agentSkillExpansion)

	invs := eventsOfKind(s, KindSkillInvocation)
	if len(invs) != 1 {
		t.Fatalf("skill_invocation %d件: %+v", len(invs), s.Events)
	}
	inv := invs[0].Skill
	if inv.Name != "superpowers:brainstorming" || inv.Path != "/Users/example/plug/skills/brainstorming" {
		t.Errorf("SkillInvocation = %+v", inv)
	}
	if inv.ByUser || inv.Command != "" {
		t.Errorf("エージェント発動なのに ByUser/Command が設定されている: %+v", inv)
	}
	// tool_call と二重計上しない
	if len(eventsOfKind(s, KindToolCall)) != 0 {
		t.Error("Skill tool_use が tool_call としても出ている")
	}
	if s.Stats.SkillInvocations != 1 || s.Stats.ToolCalls != 0 {
		t.Errorf("Stats = %+v", s.Stats)
	}
	// 展開本文が user_message として漏れない
	for _, e := range eventsOfKind(s, KindUserMessage) {
		if strings.Contains(e.Text, "現れてはならない") {
			t.Error("展開本文が user_message に漏れている")
		}
	}
}

func TestAgentSkillEmptyNameFallsBackToBasename(t *testing.T) {
	use := `{"type":"assistant","uuid":"a1","isSidechain":false,"cwd":"/p","sessionId":"s1","timestamp":"2026-07-12T00:00:01Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"tu1","name":"Skill","input":{}}]}}`
	s := parseLines(t, use, agentSkillExpansion)
	invs := eventsOfKind(s, KindSkillInvocation)
	if len(invs) != 1 || invs[0].Skill.Name != "brainstorming" {
		t.Fatalf("basename 縮退が働いていない: %+v", invs)
	}
}

func TestSkillUseWithoutExpansionStaysToolCall(t *testing.T) {
	// 対応する展開が無い Skill tool_use は従来どおり tool_call
	s := parseLines(t, agentSkillUse)
	if len(eventsOfKind(s, KindSkillInvocation)) != 0 {
		t.Error("展開が無いのに skill_invocation になった")
	}
	if len(eventsOfKind(s, KindToolCall)) != 1 {
		t.Error("tool_call が維持されていない")
	}
}

func TestDeniedSkillUseStaysPermissionDeny(t *testing.T) {
	deny := `{"type":"user","uuid":"u1","isSidechain":false,"cwd":"/p","sessionId":"s1","timestamp":"2026-07-12T00:00:02Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu1","is_error":true,"content":"The user doesn't want to proceed with this tool use. The tool use was rejected. To tell you how to proceed, the user said:\nやめて"}]}}`
	s := parseLines(t, agentSkillUse, deny)
	if len(eventsOfKind(s, KindPermissionDeny)) != 1 {
		t.Fatal("拒否された Skill tool_use が permission_deny になっていない")
	}
	if len(eventsOfKind(s, KindSkillInvocation)) != 0 {
		t.Error("拒否なのに skill_invocation が出ている")
	}
}

const (
	userCommand   = `{"type":"user","uuid":"cmd1","isSidechain":false,"cwd":"/p","sessionId":"s1","timestamp":"2026-07-12T00:00:01Z","message":{"role":"user","content":"<command-message>ohayou</command-message>\n<command-name>/ohayou</command-name>"}}`
	userExpansion = `{"type":"user","uuid":"sk2","parentUuid":"cmd1","isMeta":true,"isSidechain":false,"cwd":"/p","sessionId":"s1","timestamp":"2026-07-12T00:00:02Z","message":{"role":"user","content":"Base directory for this skill: /Users/example/.claude/skills/ohayou\n\n# 手順\n\nこの本文は出力に現れてはならない"}}`
)

func TestUserSkillInvocation(t *testing.T) {
	s := parseLines(t, userCommand, userExpansion)
	invs := eventsOfKind(s, KindSkillInvocation)
	if len(invs) != 1 {
		t.Fatalf("skill_invocation %d件: %+v", len(invs), s.Events)
	}
	inv := invs[0].Skill
	if !inv.ByUser || inv.Name != "ohayou" || inv.Command != "/ohayou" {
		t.Errorf("SkillInvocation = %+v", inv)
	}
	if inv.Path != "/Users/example/.claude/skills/ohayou" {
		t.Errorf("Path = %q", inv.Path)
	}
	// イベント時刻は展開エントリの timestamp (00:00:02)
	if invs[0].Timestamp.Second() != 2 {
		t.Errorf("Timestamp = %v, want 展開エントリの時刻", invs[0].Timestamp)
	}
	// user_message にも数えない (コマンドエントリはノイズ除去で空になる)
	if s.Stats.UserMessages != 0 || s.Stats.SkillInvocations != 1 {
		t.Errorf("Stats = %+v", s.Stats)
	}
}

func TestUserSkillInvocationWithArgs(t *testing.T) {
	cmd := `{"type":"user","uuid":"cmd1","isSidechain":false,"cwd":"/p","sessionId":"s1","timestamp":"2026-07-12T00:00:01Z","message":{"role":"user","content":"<command-name>/ohayou</command-name>\n<command-args>今日も よろしく</command-args>"}}`
	s := parseLines(t, cmd, userExpansion)
	invs := eventsOfKind(s, KindSkillInvocation)
	if len(invs) != 1 || invs[0].Skill.Command != "/ohayou 今日も よろしく" {
		t.Fatalf("引数付き Command 再現が違う: %+v", invs)
	}
}

func TestUserSkillBasenameFallback(t *testing.T) {
	// parentUuid の先にコマンドエントリが無い孤立展開は basename 縮退
	orphan := `{"type":"user","uuid":"sk3","parentUuid":"nonexistent","isMeta":true,"isSidechain":false,"cwd":"/p","sessionId":"s1","timestamp":"2026-07-12T00:00:05Z","message":{"role":"user","content":"Base directory for this skill: /Users/example/.claude/skills/oyasumi\n本文"}}`
	s := parseLines(t, orphan)
	invs := eventsOfKind(s, KindSkillInvocation)
	if len(invs) != 1 {
		t.Fatalf("skill_invocation %d件", len(invs))
	}
	inv := invs[0].Skill
	if !inv.ByUser || inv.Name != "oyasumi" || inv.Command != "/oyasumi" {
		t.Errorf("basename 縮退 = %+v", inv)
	}
}

func TestUserSkillNotBoundToUnrelatedCommand(t *testing.T) {
	// コマンドの後に通常発話を挟んだ孤立展開 (parentUuid 不一致) は、
	// 古いコマンドと結び付かず basename 縮退になる
	talk := `{"type":"user","uuid":"u1","isSidechain":false,"cwd":"/p","sessionId":"s1","timestamp":"2026-07-12T00:00:03Z","message":{"role":"user","content":"別の話"}}`
	orphan := `{"type":"user","uuid":"sk4","parentUuid":"u1","isMeta":true,"isSidechain":false,"cwd":"/p","sessionId":"s1","timestamp":"2026-07-12T00:00:04Z","message":{"role":"user","content":"Base directory for this skill: /Users/example/.claude/skills/oyasumi\n本文"}}`
	s := parseLines(t, userCommand, talk, orphan)
	invs := eventsOfKind(s, KindSkillInvocation)
	if len(invs) != 1 {
		t.Fatalf("skill_invocation %d件", len(invs))
	}
	if invs[0].Skill.Name != "oyasumi" {
		t.Errorf("古いコマンド /ohayou と結び付いている: %+v", invs[0].Skill)
	}
}

func TestTwoConsecutiveUserSkillInvocations(t *testing.T) {
	// 連続する2件のユーザー呼び出しが、それぞれ正しいコマンドと結び付く
	cmd2 := `{"type":"user","uuid":"cmd2","isSidechain":false,"cwd":"/p","sessionId":"s1","timestamp":"2026-07-12T00:00:03Z","message":{"role":"user","content":"<command-name>/oyasumi</command-name>"}}`
	exp2 := `{"type":"user","uuid":"sk5","parentUuid":"cmd2","isMeta":true,"isSidechain":false,"cwd":"/p","sessionId":"s1","timestamp":"2026-07-12T00:00:04Z","message":{"role":"user","content":"Base directory for this skill: /Users/example/.claude/skills/oyasumi\n本文"}}`
	s := parseLines(t, userCommand, userExpansion, cmd2, exp2)
	invs := eventsOfKind(s, KindSkillInvocation)
	if len(invs) != 2 {
		t.Fatalf("skill_invocation %d件", len(invs))
	}
	if invs[0].Skill.Name != "ohayou" || invs[1].Skill.Name != "oyasumi" {
		t.Errorf("対応付けが崩れている: %+v, %+v", invs[0].Skill, invs[1].Skill)
	}
}

func TestCommandAndExpansionWithInterveningRecord(t *testing.T) {
	// コマンドと展開の間に補助レコード (assistant 発話) が挟まっても parentUuid で結び付く
	mid := `{"type":"assistant","uuid":"a9","isSidechain":false,"cwd":"/p","sessionId":"s1","timestamp":"2026-07-12T00:00:01.5Z","message":{"role":"assistant","content":[{"type":"text","text":"承知しました"}]}}`
	s := parseLines(t, userCommand, mid, userExpansion)
	invs := eventsOfKind(s, KindSkillInvocation)
	if len(invs) != 1 || invs[0].Skill.Name != "ohayou" {
		t.Fatalf("補助レコードを挟むと結び付かない: %+v", invs)
	}
}

func TestOrphanExpansionWithSourceToolUseIDIsHidden(t *testing.T) {
	// sourceToolUseID を持つのに対応する Skill tool_use が無い展開は描画しない
	s := parseLines(t,
		`{"type":"user","uuid":"u0","isSidechain":false,"cwd":"/p","sessionId":"s1","timestamp":"2026-07-12T00:00:01Z","message":{"role":"user","content":"通常発話"}}`,
		agentSkillExpansion)
	if len(eventsOfKind(s, KindSkillInvocation)) != 0 {
		t.Error("孤立した展開エントリが描画されている")
	}
	for _, e := range eventsOfKind(s, KindUserMessage) {
		if strings.Contains(e.Text, "現れてはならない") {
			t.Error("孤立展開の本文が user_message に漏れている")
		}
	}
}
