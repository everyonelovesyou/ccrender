package parse

import (
	"encoding/json"
	"path"
	"regexp"
	"strings"
)

// スキル展開エントリの本文はこの行で始まる (設計書)。
const skillPathPrefix = "Base directory for this skill: "

// skillExpansionPath は rec がスキル展開エントリなら (スキルの絶対パス, true) を返す。
// 判定: type=user かつ isMeta かつ最初の text ブロックが skillPathPrefix で始まること。
func skillExpansionPath(rec rawRecord) (string, bool) {
	if rec.Type != "user" || !rec.IsMeta {
		return "", false
	}
	bs := rec.Message.blocks()
	if len(bs) == 0 || bs[0].Type != "text" || !strings.HasPrefix(bs[0].Text, skillPathPrefix) {
		return "", false
	}
	line := bs[0].Text[len(skillPathPrefix):]
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	return strings.TrimSpace(line), true
}

// commandEntry はスラッシュコマンドの user エントリから抽出したコマンド情報。
type commandEntry struct {
	name string // "/ohayou" のような先頭スラッシュ付きの名前
	args string // <command-args> のトリム結果。空白のみなら ""
}

// skillIndex はスキル展開の対応付けに使う事前索引。
type skillIndex struct {
	expansions map[string]string       // sourceToolUseID → スキルパス (エージェント発動)
	commands   map[string]commandEntry // コマンドエントリの uuid → コマンド (ユーザー呼び出し)
}

var (
	commandNameRE = regexp.MustCompile(`(?s)<command-name>(.*?)</command-name>`)
	commandArgsRE = regexp.MustCompile(`(?s)<command-args>(.*?)</command-args>`)
)

// collectSkillIndex は全レコードからスキル展開とコマンドエントリを索引化する。
func collectSkillIndex(records []rawRecord) skillIndex {
	idx := skillIndex{expansions: map[string]string{}, commands: map[string]commandEntry{}}
	for _, rec := range records {
		if p, ok := skillExpansionPath(rec); ok {
			if rec.SourceToolUseID != "" {
				idx.expansions[rec.SourceToolUseID] = p
			}
			continue
		}
		if rec.Type != "user" || rec.IsMeta || rec.UUID == "" {
			continue
		}
		for _, b := range rec.Message.blocks() {
			if b.Type != "text" {
				continue
			}
			m := commandNameRE.FindStringSubmatch(b.Text)
			if m == nil {
				continue
			}
			e := commandEntry{name: strings.TrimSpace(m[1])}
			if a := commandArgsRE.FindStringSubmatch(b.Text); a != nil {
				e.args = strings.TrimSpace(a[1])
			}
			idx.commands[rec.UUID] = e
			break
		}
	}
	return idx
}

// agentSkillInvocation はエージェント発動 (Skill tool_use) の SkillInvocation を組み立てる。
func agentSkillInvocation(b contentBlock, skillPath string) *SkillInvocation {
	var input struct {
		Skill string `json:"skill"`
	}
	_ = json.Unmarshal(b.Input, &input)
	name := input.Skill
	if name == "" {
		// 縮退: input.skill が空ならパスの basename で代替する
		name = path.Base(skillPath)
	}
	return &SkillInvocation{Name: name, Path: skillPath}
}
