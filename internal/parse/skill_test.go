package parse

import (
	"encoding/json"
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
