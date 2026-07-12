# スキル発動エントリの特別描画 実装計画

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** スキル展開エントリを user 発言として垂れ流さず、発動元 (ユーザー呼び出し / エージェント発動) に応じた「🔧 Skill(名前) パス」チップとして md / html に描画する。

**Architecture:** parse 層に `skill_invocation` イベント種別を追加し、展開エントリの判定・対応付け (エージェント発動は `sourceToolUseID`、ユーザー呼び出しは `parentUuid` 索引) を閉じ込める。render 層には `fileURL` テンプレート関数を追加し、html の `file://` リンクを安全に生成する。テンプレートと README を更新する。

**Tech Stack:** Go (標準ライブラリのみ)。`html/template` / `text/template`、golden テスト方式。

**Spec:** `docs/superpowers/specs/2026-07-12-skill-invocation-rendering-design.md` (レビュー反映済み版)

## Global Constraints

- 依存追加なし (標準ライブラリのみ)
- 生ログ固有の判定は `internal/parse` に閉じる (render はイベントモデルだけを見る)
- 展開本文 (スキル手順書) はどの形式の出力にも一切現れない (折りたたみにも残さない)
- 「直近のコマンドを覚えておく」近接方式は使わない。対応付けは `parentUuid` / `sourceToolUseID` のみ
- 縮退時は無言のフォールバックではなく設計書どおりの縮退 (basename 代替) を行う。設計にない縮退が必要になったら実装を止めて報告する
- コミットは各タスクの実装エージェントが行う (ユーザーの明示指示による代行)。全テストが通ったことを確認してからコミットすること
- コミットメッセージは既存ログに合わせ `feat:` / `test:` / `docs:` + 日本語1行とし、末尾に `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>` を付ける
- テスト実行はリポジトリルートで `go test ./...`

---

### Task 1: parse — データモデルとスキル展開エントリの検出基盤

`skill_invocation` のモデル定義、`rawRecord` のフィールド追加、展開エントリ判定 (`skillExpansionPath`) とコマンド / 展開の事前索引 (`collectSkillIndex`) を作る。このタスクではまだイベント生成フローには組み込まない (Task 2, 3 で行う)。

**Files:**
- Modify: `internal/parse/model.go`
- Modify: `internal/parse/record.go:9-18` (rawRecord)
- Create: `internal/parse/skill.go`
- Test: `internal/parse/skill_test.go`

**Interfaces:**
- Consumes: 既存の `rawRecord`, `contentBlock`, `(*rawMessage).blocks()`
- Produces:
  - `const KindSkillInvocation EventKind = "skill_invocation"`
  - `type SkillInvocation struct { Name, Path string; ByUser bool; Command string }` / `Event.Skill *SkillInvocation` / `Stats.SkillInvocations int`
  - `func skillExpansionPath(rec rawRecord) (string, bool)` — 展開エントリなら (パス, true)
  - `type commandEntry struct { name, args string }` — name は `/ohayou` 形式 (先頭 `/` 付き)、args はトリム済み (空白のみなら "")
  - `type skillIndex struct { expansions map[string]string; commands map[string]commandEntry }` — expansions は sourceToolUseID → パス、commands はコマンドエントリの uuid → commandEntry
  - `func collectSkillIndex(records []rawRecord) skillIndex`
  - `rawRecord` に `IsMeta bool` / `SourceToolUseID string` / `ParentUUID string` フィールド

- [ ] **Step 1: 失敗するテストを書く**

`internal/parse/skill_test.go` を新規作成:

```go
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
```

- [ ] **Step 2: テストが失敗することを確認する**

Run: `go test ./internal/parse/ -run 'TestSkillExpansionPath|TestCollectSkillIndex' -v`
Expected: コンパイルエラー (`undefined: skillExpansionPath` など) で FAIL

- [ ] **Step 3: モデルと rawRecord を拡張する**

`internal/parse/model.go` — 定数ブロックに追加:

```go
	KindSubagentCall     EventKind = "subagent_call"
	KindSkillInvocation  EventKind = "skill_invocation"
```

`Stats` に追加 (SubagentCalls の下):

```go
	SubagentCalls     int
	SkillInvocations  int
```

`Event` に追加 (Subagent の下):

```go
	Subagent  *Subagent // Kind が SubagentCall のとき非 nil
	Skill     *SkillInvocation // Kind が SkillInvocation のとき非 nil
```

`Subagent` 型定義の下に追加:

```go
type SkillInvocation struct {
	Name    string // スキル名。エージェント発動は input.skill、ユーザー呼び出しは <command-name> の先頭 "/" を除いた形
	Path    string // "Base directory for this skill:" の絶対パス
	ByUser  bool   // true ならユーザー呼び出し (スラッシュコマンド)
	Command string // ByUser のとき「/name 引数」の再現文字列。エージェント発動では空
}
```

`internal/parse/record.go` — `rawRecord` にフィールド追加:

```go
type rawRecord struct {
	Type            string      `json:"type"`
	Subtype         string      `json:"subtype"`
	UUID            string      `json:"uuid"`
	ParentUUID      string      `json:"parentUuid"`
	IsSidechain     bool        `json:"isSidechain"`
	IsMeta          bool        `json:"isMeta"`
	SourceToolUseID string      `json:"sourceToolUseID"`
	CWD             string      `json:"cwd"`
	SessionID       string      `json:"sessionId"`
	Timestamp       string      `json:"timestamp"`
	Message         *rawMessage `json:"message"`
}
```

- [ ] **Step 4: skill.go を実装する**

`internal/parse/skill.go` を新規作成:

```go
package parse

import (
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
```

- [ ] **Step 5: テストが通ることを確認する**

Run: `go test ./internal/parse/ -v`
Expected: 新規2テスト PASS、既存テストも全 PASS

- [ ] **Step 6: Commit**

```bash
git add internal/parse/model.go internal/parse/record.go internal/parse/skill.go internal/parse/skill_test.go
git commit -m "$(cat <<'EOF'
feat: スキル展開エントリの検出基盤とデータモデルを追加

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 2: parse — エージェント発動を skill_invocation イベントにする

`Skill` の tool_use に対応する展開があれば `tool_call` の代わりに `skill_invocation` を出す。展開エントリ自体はどの発動元でも `user_message` に漏らさない。`Stats.SkillInvocations` の計上もここで入れる。

**Files:**
- Modify: `internal/parse/parse.go` (ParseFile / buildEvents / toolEvent / computeStats)
- Modify: `internal/parse/skill.go`
- Test: `internal/parse/skill_test.go`

**Interfaces:**
- Consumes: Task 1 の `skillIndex` / `collectSkillIndex` / `skillExpansionPath` / `SkillInvocation` / `KindSkillInvocation`
- Produces:
  - `buildEvents(rec rawRecord, ts time.Time, results map[string]contentBlock, subs map[string]Subagent, idx skillIndex, warn io.Writer) []Event` — 引数に idx が増える
  - `toolEvent(b contentBlock, ts time.Time, results map[string]contentBlock, subs map[string]Subagent, idx skillIndex, warn io.Writer) Event` — 同上
  - `func agentSkillInvocation(b contentBlock, skillPath string) *SkillInvocation` (skill.go)
  - buildEvents はユーザー呼び出し (sourceToolUseID なし) の展開エントリを**まだ描画しない** (Task 3 で追加)。エージェント発動の展開エントリは常に非表示

- [ ] **Step 1: 失敗するテストを書く**

`internal/parse/skill_test.go` に追記。JSONL をテスト内で組み立てて `t.TempDir()` に書き、ParseFile で通す (既存 `writeFile` ヘルパーは parse_test.go にある):

```go
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
	agentSkillUse = `{"type":"assistant","uuid":"a1","isSidechain":false,"cwd":"/p","sessionId":"s1","timestamp":"2026-07-12T00:00:01Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"tu1","name":"Skill","input":{"skill":"superpowers:brainstorming"}}]}}`
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
```

- [ ] **Step 2: テストが失敗することを確認する**

Run: `go test ./internal/parse/ -run 'Skill' -v`
Expected: `TestAgentSkillInvocation` 等が FAIL (skill_invocation が出ない / 展開本文が user_message に漏れる)

- [ ] **Step 3: parse.go と skill.go を実装する**

`internal/parse/skill.go` に追記:

```go
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
```

import に `"encoding/json"` と `"path"` を追加。

`internal/parse/parse.go` を変更:

ParseFile (`results := collectToolResults(records)` の直後):

```go
	results := collectToolResults(records)
	idx := collectSkillIndex(records)
```

buildEvents 呼び出し:

```go
		s.Events = append(s.Events, buildEvents(rec, ts, results, subs, idx, warn)...)
```

buildEvents のシグネチャと user ケース冒頭:

```go
// buildEvents は1レコードをイベント列へ変換する。
func buildEvents(rec rawRecord, ts time.Time, results map[string]contentBlock, subs map[string]Subagent, idx skillIndex, warn io.Writer) []Event {
	var events []Event
	switch rec.Type {
	case "user":
		if _, ok := skillExpansionPath(rec); ok {
			// スキル展開エントリは user_message として出力しない。
			// エージェント発動 (sourceToolUseID あり) は Skill tool_use 側で描画する
			// (対応する tool_use が無ければそのまま非表示)。
			return nil
		}
		for _, b := range rec.Message.blocks() {
```

(user ケースのそれ以外と assistant / system ケースは変更なし。tool_use の分岐は `toolEvent(b, ts, results, subs, idx, warn)` に引数を追加)

toolEvent — permission_deny の早期 return の後、`b.Name == "Agent"` 分岐の前に挿入:

```go
	if kind == KindPermissionDeny {
		// 拒否された Agent tool_use は SubagentCall ではなく PermissionDeny として扱う
		// (サブエージェントは実際には起動されていないため)
		return Event{Kind: kind, Timestamp: ts, Tool: tc}
	}
	if b.Name == "Skill" {
		if p, ok := idx.expansions[b.ID]; ok {
			return Event{Kind: KindSkillInvocation, Timestamp: ts, Skill: agentSkillInvocation(b, p)}
		}
	}
	if b.Name == "Agent" {
```

(toolEvent のシグネチャにも `idx skillIndex` を追加する)

computeStats に追加:

```go
		case KindSubagentCall:
			st.SubagentCalls++
		case KindSkillInvocation:
			st.SkillInvocations++
```

- [ ] **Step 4: テストが通ることを確認する**

Run: `go test ./...`
Expected: 全 PASS (既存テスト含む)

- [ ] **Step 5: Commit**

```bash
git add internal/parse/parse.go internal/parse/skill.go internal/parse/skill_test.go
git commit -m "$(cat <<'EOF'
feat: エージェント発動の Skill tool_use を skill_invocation イベントにする

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 3: parse — ユーザー呼び出しを skill_invocation イベントにする

sourceToolUseID を持たない展開エントリを、`parentUuid` でコマンドエントリと突き合わせて `skill_invocation` (ByUser=true) にする。対応が見つからなければ basename 縮退。

**Files:**
- Modify: `internal/parse/parse.go` (buildEvents の user ケース)
- Modify: `internal/parse/skill.go`
- Test: `internal/parse/skill_test.go`

**Interfaces:**
- Consumes: Task 1 の `skillIndex.commands` / `commandEntry`、Task 2 の buildEvents 構造
- Produces: `func userSkillEvent(rec rawRecord, skillPath string, ts time.Time, idx skillIndex) Event` (skill.go)

- [ ] **Step 1: 失敗するテストを書く**

`internal/parse/skill_test.go` に追記:

```go
const (
	userCommand = `{"type":"user","uuid":"cmd1","isSidechain":false,"cwd":"/p","sessionId":"s1","timestamp":"2026-07-12T00:00:01Z","message":{"role":"user","content":"<command-message>ohayou</command-message>\n<command-name>/ohayou</command-name>"}}`
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
```

- [ ] **Step 2: テストが失敗することを確認する**

Run: `go test ./internal/parse/ -run 'UserSkill|Consecutive|Intervening' -v`
Expected: FAIL (ユーザー呼び出しの展開は Task 2 時点では常に非表示のため skill_invocation が0件)

- [ ] **Step 3: userSkillEvent を実装する**

`internal/parse/skill.go` に追記 (import に `"time"` を追加):

```go
// userSkillEvent はユーザー呼び出しの展開エントリを skill_invocation イベントへ変換する。
// parentUuid でコマンドエントリを引き、見つからなければパスの basename で縮退する。
func userSkillEvent(rec rawRecord, skillPath string, ts time.Time, idx skillIndex) Event {
	inv := &SkillInvocation{Path: skillPath, ByUser: true}
	if cmd, ok := idx.commands[rec.ParentUUID]; ok && rec.ParentUUID != "" {
		inv.Name = strings.TrimPrefix(cmd.name, "/")
		inv.Command = cmd.name
		if cmd.args != "" {
			inv.Command += " " + cmd.args
		}
	} else {
		base := path.Base(skillPath)
		inv.Name = base
		inv.Command = "/" + base
	}
	return Event{Kind: KindSkillInvocation, Timestamp: ts, Skill: inv}
}
```

`internal/parse/parse.go` の buildEvents user ケースを更新:

```go
	case "user":
		if p, ok := skillExpansionPath(rec); ok {
			if rec.SourceToolUseID != "" {
				// エージェント発動: Skill tool_use 側で描画する (対応が無ければ非表示)
				return nil
			}
			return []Event{userSkillEvent(rec, p, ts, idx)}
		}
```

- [ ] **Step 4: テストが通ることを確認する**

Run: `go test ./...`
Expected: 全 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/parse/parse.go internal/parse/skill.go internal/parse/skill_test.go
git commit -m "$(cat <<'EOF'
feat: スラッシュコマンドのスキル発動を parentUuid で対応付けて描画する

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 4: render — fileURL テンプレート関数

`file://` URL を安全に生成する `fileURL` を render 層に追加する。html/template の安全機構 (`template.URL`) を意図的に迂回するため、file スキーム + 絶対パス専用とする。

**Files:**
- Modify: `internal/render/funcs.go`
- Test: `internal/render/funcs_test.go`

**Interfaces:**
- Consumes: なし (独立)
- Produces: `func FileURL(path string) template.URL` (html/template の URL 型)、`Funcs()` に `"fileURL": FileURL` を登録

- [ ] **Step 1: 失敗するテストを書く**

`internal/render/funcs_test.go` に追記:

```go
func TestFileURL(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"通常の絶対パス", "/Users/example/.claude/skills/ohayou", "file:///Users/example/.claude/skills/ohayou"},
		{"空白と # を %エスケープする", "/Users/example/my skills/foo#bar", "file:///Users/example/my%20skills/foo%23bar"},
		{"? を %エスケープする", "/Users/example/q?x", "file:///Users/example/q%3Fx"},
		{"相対パスは空文字列 (テキスト表示へ縮退)", "skills/ohayou", ""},
		{"空文字列も空", "", ""},
	}
	for _, c := range cases {
		if got := string(FileURL(c.in)); got != c.want {
			t.Errorf("%s: FileURL(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}
```

- [ ] **Step 2: テストが失敗することを確認する**

Run: `go test ./internal/render/ -run TestFileURL -v`
Expected: コンパイルエラー (`undefined: FileURL`) で FAIL

- [ ] **Step 3: FileURL を実装する**

`internal/render/funcs.go` — import を更新し関数を追加:

```go
import (
	"fmt"
	htmltemplate "html/template"
	"net/url"
	"strings"
	"text/template"
)
```

```go
// FileURL は絶対パスから file:// URL を生成する (空白・#・? 等を %エスケープ)。
// template.URL を返すのは html/template の安全機構の意図的な迂回なので、
// file スキーム + 絶対パス専用とし、汎用のキャスト関数にはしない。
// 絶対パスでなければ空文字列を返し、テンプレート側でテキスト表示へ縮退させる。
func FileURL(path string) htmltemplate.URL {
	if !strings.HasPrefix(path, "/") {
		return ""
	}
	u := url.URL{Scheme: "file", Path: path}
	return htmltemplate.URL(u.String())
}
```

`Funcs()` に追加:

```go
	return template.FuncMap{
		"truncateLines": TruncateLines,
		"firstLine":     FirstLine,
		"fileURL":       FileURL,
	}
```

- [ ] **Step 4: テストが通ることを確認する**

Run: `go test ./internal/render/ -v`
Expected: 全 PASS (markdown 側も同じ Funcs() を使うが、md テンプレートでは fileURL を呼ばないため影響なし)

- [ ] **Step 5: Commit**

```bash
git add internal/render/funcs.go internal/render/funcs_test.go
git commit -m "$(cat <<'EOF'
feat: file:// リンクを安全に生成する fileURL テンプレート関数を追加

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 5: md テンプレート — スキルチップと統計行

md 出力に skill_invocation の描画と統計行の「スキル N」を追加する。fixtureSession にイベントを足して golden を更新する。

**Files:**
- Modify: `templates/default.md.tmpl`
- Modify: `internal/render/markdown_test.go` (fixtureSession)
- Test: `internal/render/testdata/golden.md` (golden 更新)

**Interfaces:**
- Consumes: Task 1 の `parse.KindSkillInvocation` / `parse.SkillInvocation` / `Event.Skill` / `Stats.SkillInvocations`
- Produces: fixtureSession に skill_invocation イベント2件 (ByUser / エージェント発動) と `Stats.SkillInvocations: 2` が入る。Task 6 (html) も同じ fixtureSession を使う

- [ ] **Step 1: fixtureSession にスキルイベントを追加する (失敗するテストにする)**

`internal/render/markdown_test.go` の fixtureSession の Events 末尾 (`{Kind: parse.KindToolCall, ... Name: "Read" ...}` の後) に追加:

```go
			{Kind: parse.KindSkillInvocation, Timestamp: at(7), Skill: &parse.SkillInvocation{
				Name: "ohayou", Path: "/Users/example/.claude/skills/ohayou",
				ByUser: true, Command: "/ohayou 今日も",
			}},
			{Kind: parse.KindSkillInvocation, Timestamp: at(8), Skill: &parse.SkillInvocation{
				Name: "superpowers:brainstorming",
				Path: "/Users/example/plug/skills/brainstorming",
			}},
```

Stats を更新:

```go
		Stats: parse.Stats{
			UserMessages: 1, AssistantMessages: 1, ToolCalls: 2,
			PermissionDenies: 1, SystemNotes: 1, SubagentCalls: 1,
			SkillInvocations: 2, SkippedLines: 1,
		},
```

- [ ] **Step 2: テストが失敗することを確認する**

Run: `go test ./internal/render/ -run TestMarkdownGolden -v`
Expected: golden と不一致で FAIL (skill イベントが未対応なので何も描画されず、統計行にも出ない)

- [ ] **Step 3: md テンプレートを更新する**

`templates/default.md.tmpl` の統計行 (5行目) を更新:

```
- イベント: user {{.Stats.UserMessages}} / assistant {{.Stats.AssistantMessages}} / ツール {{.Stats.ToolCalls}} / 権限拒否 {{.Stats.PermissionDenies}} / サブエージェント {{.Stats.SubagentCalls}} / スキル {{.Stats.SkillInvocations}}
```

イベント分岐の `subagent_call` ブロックの後 (`{{else if eq .Kind "system_note"}}` の前) に追加:

```
{{else if eq .Kind "skill_invocation"}}
{{- if .Skill.ByUser}}
## 👤 User ({{.Timestamp.Format "15:04"}})

{{.Skill.Command}}

🔧 Skill({{.Skill.Name}}) `{{.Skill.Path}}`
{{else}}
🔧 Skill({{.Skill.Name}}) `{{.Skill.Path}}`
{{end}}
```

- [ ] **Step 4: golden を更新し、内容を目視確認する**

Run: `go test ./internal/render/ -update && git diff internal/render/testdata/golden.md`
Expected: golden.md の差分に以下が現れる:
- 統計行末尾に `/ スキル 2`
- `## 👤 User (00:00)` + `/ohayou 今日も` + ``🔧 Skill(ohayou) `/Users/example/.claude/skills/ohayou` ``
- 単独行の ``🔧 Skill(superpowers:brainstorming) `/Users/example/plug/skills/brainstorming` ``

- [ ] **Step 5: 全テストが通ることを確認する**

Run: `go test ./...`
Expected: 全 PASS (golden.html は Task 6 で更新するため、この時点で TestHTMLGolden が FAIL したら Step 4 と同じ `-update` で html 側 golden も更新されている — html テンプレート未対応のため skill イベントは何も描画されないだけで、エラーにはならない)

- [ ] **Step 6: Commit**

```bash
git add templates/default.md.tmpl internal/render/markdown_test.go internal/render/testdata/golden.md internal/render/testdata/golden.html
git commit -m "$(cat <<'EOF'
feat: md 出力にスキル発動チップと統計を追加

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 6: html テンプレート — .skill チップと file:// リンク

html 出力に `.skill` チップ (fileURL リンク付き)、ユーザーバブルへの埋め込み、統計行を追加する。

**Files:**
- Modify: `templates/default.html.tmpl`
- Test: `internal/render/html_test.go`
- Test: `internal/render/testdata/golden.html` (golden 更新)

**Interfaces:**
- Consumes: Task 4 の `fileURL` 関数、Task 5 で拡張済みの fixtureSession
- Produces: なし (最終消費者)

- [ ] **Step 1: 失敗するテストを書く**

`internal/render/html_test.go` に追記:

```go
func TestHTMLSkillChip(t *testing.T) {
	var buf bytes.Buffer
	if err := HTML(&buf, fixtureSession(), ""); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	// file:// リンクが期待どおり生成される (#ZgotmplZ でない)
	if strings.Contains(out, "ZgotmplZ") {
		t.Error("href が #ZgotmplZ に置換されている")
	}
	if !strings.Contains(out, `href="file:///Users/example/.claude/skills/ohayou"`) {
		t.Error("ユーザー呼び出しの file:// リンクがない")
	}
	if !strings.Contains(out, `href="file:///Users/example/plug/skills/brainstorming"`) {
		t.Error("エージェント発動の file:// リンクがない")
	}
	// ユーザー呼び出しはコマンド再現がユーザーバブルに入る
	if !strings.Contains(out, "/ohayou 今日も") {
		t.Error("コマンド再現がない")
	}
	if !strings.Contains(out, `class="skill"`) {
		t.Error(".skill チップがない")
	}
}

func TestHTMLSkillPathEscaping(t *testing.T) {
	s := &parse.Session{ID: "x", Events: []parse.Event{
		{Kind: parse.KindSkillInvocation, Skill: &parse.SkillInvocation{
			Name: "odd", Path: "/Users/example/my skills/foo#bar",
		}},
	}}
	var buf bytes.Buffer
	if err := HTML(&buf, s, ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `href="file:///Users/example/my%20skills/foo%23bar"`) {
		t.Errorf("空白・# が %%エスケープされていない:\n%s", buf.String())
	}
}

func TestHTMLSkillRelativePathDegradesToText(t *testing.T) {
	s := &parse.Session{ID: "x", Events: []parse.Event{
		{Kind: parse.KindSkillInvocation, Skill: &parse.SkillInvocation{
			Name: "rel", Path: "skills/rel",
		}},
	}}
	var buf bytes.Buffer
	if err := HTML(&buf, s, ""); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Contains(out, `href="skills/rel"`) || strings.Contains(out, `href=""`) {
		t.Error("相対パスがリンクになっている (または空 href が出ている)")
	}
	if !strings.Contains(out, "<code>skills/rel</code>") {
		t.Error("相対パスがテキスト表示へ縮退していない")
	}
}
```

- [ ] **Step 2: テストが失敗することを確認する**

Run: `go test ./internal/render/ -run TestHTMLSkill -v`
Expected: FAIL (skill イベントが描画されない)

- [ ] **Step 3: html テンプレートを更新する**

`templates/default.html.tmpl` の `<style>` に追加 (`.sub .body` の行の後):

```css
  .skill { font-size: 0.9rem; margin: 0.5rem 0 0.5rem 1rem; color: #5f6368; }
  .msg .skill { margin: 0.5rem 0 0; }
```

header の統計行を更新:

```html
    <dt>イベント</dt><dd>user {{.Stats.UserMessages}} / assistant {{.Stats.AssistantMessages}} / ツール {{.Stats.ToolCalls}} / 🚫 {{.Stats.PermissionDenies}} / 🤝 {{.Stats.SubagentCalls}} / スキル {{.Stats.SkillInvocations}}</dd>
```

イベント分岐の `subagent_call` ブロックの後 (`{{- else if eq .Kind "system_note"}}` の前) に追加:

```html
{{- else if eq .Kind "skill_invocation"}}
{{- $p := .Skill.Path}}
{{- if .Skill.ByUser}}
<div class="msg user"><span class="ts">👤 {{.Timestamp.Format "15:04"}}</span>
{{.Skill.Command}}
<div class="skill">🔧 Skill(<b>{{.Skill.Name}}</b>) {{with fileURL $p}}<a href="{{.}}">{{$p}}</a>{{else}}<code>{{$p}}</code>{{end}}</div>
</div>
{{- else}}
<div class="skill">🔧 Skill(<b>{{.Skill.Name}}</b>) {{with fileURL $p}}<a href="{{.}}">{{$p}}</a>{{else}}<code>{{$p}}</code>{{end}}</div>
{{- end}}
```

- [ ] **Step 4: golden を更新し、テストが通ることを確認する**

Run: `go test ./internal/render/ -update && go test ./...`
Expected: 全 PASS。`git diff internal/render/testdata/golden.html` で `.skill` チップと `file://` リンクの差分を目視確認する

- [ ] **Step 5: ブラウザ確認用の実出力を1つ作る (手動確認)**

Run: `go run ./cmd/cctx --stdout <実セッションの JSONL があれば指定> | head -50` の代わりに、golden.html をブラウザで開いて `.skill` チップの見た目と file:// リンクを確認する:

```bash
open internal/render/testdata/golden.html
```

Expected: 🔧 チップが tool 行と同等の控えめなトーンで表示され、パスがリンクになっている (file:// リンクはブラウザ制約で開けない場合もあるが、href の形が正しければよい)

- [ ] **Step 6: Commit**

```bash
git add templates/default.html.tmpl internal/render/html_test.go internal/render/testdata/golden.html
git commit -m "$(cat <<'EOF'
feat: html 出力にスキルチップと file:// リンクを追加

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 7: E2E — fixture 拡張と通しの確認

`session_small.jsonl` に両発動元のスキル展開行を加え、md / html の通し出力を確認する。fixture 末尾に行を足すため、`EndedAt` を見る既存テストの期待値も更新する。

**Files:**
- Modify: `internal/parse/testdata/session_small.jsonl`
- Modify: `internal/parse/parse_test.go:39-47` (TestSessionHeader の EndedAt)
- Test: `cmd/cctx/main_test.go` (TestRunEndToEnd)

**Interfaces:**
- Consumes: Task 1〜6 のすべて
- Produces: なし (最終確認)

- [ ] **Step 1: fixture に4行追加する**

`internal/parse/testdata/session_small.jsonl` の末尾 (isSidechain 行の後) に追加:

```
{"type":"user","uuid":"cmd1","isSidechain":false,"cwd":"/Users/example/proj","sessionId":"sess-0001","timestamp":"2026-07-12T00:00:11Z","message":{"role":"user","content":"<command-message>ohayou</command-message>\n<command-name>/ohayou</command-name>\n<command-args>今日も</command-args>"}}
{"type":"user","uuid":"sk1","parentUuid":"cmd1","isMeta":true,"isSidechain":false,"cwd":"/Users/example/proj","sessionId":"sess-0001","timestamp":"2026-07-12T00:00:12Z","message":{"role":"user","content":"Base directory for this skill: /Users/example/.claude/skills/ohayou\n\n# ohayou の手順\n\nこの展開本文は出力に現れてはならない"}}
{"type":"assistant","uuid":"a5","isSidechain":false,"cwd":"/Users/example/proj","sessionId":"sess-0001","timestamp":"2026-07-12T00:00:13Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"tu5","name":"Skill","input":{"skill":"superpowers:brainstorming"}}]}}
{"type":"user","uuid":"sk2","parentUuid":"a5","sourceToolUseID":"tu5","isMeta":true,"isSidechain":false,"cwd":"/Users/example/proj","sessionId":"sess-0001","timestamp":"2026-07-12T00:00:14Z","message":{"role":"user","content":[{"type":"text","text":"Base directory for this skill: /Users/example/plug/skills/brainstorming\n\nエージェント発動の展開本文も出力に現れてはならない"}]}}
```

- [ ] **Step 2: 既存テストの期待値を更新する**

`internal/parse/parse_test.go` の TestSessionHeader:

```go
	// 末尾はエージェント発動のスキル展開エントリ (00:00:14Z)
	wantEnd := time.Date(2026, 7, 12, 0, 0, 14, 0, time.UTC)
```

(コメントも上記のとおり差し替える)

- [ ] **Step 3: E2E アサートを追加する**

`cmd/cctx/main_test.go` の TestRunEndToEnd — md のアサートループを更新:

```go
	for _, want := range []string{
		"こんにちは", "確認します", "🚫", "こっちは触らないで", "サブエージェント", "(結果なし)",
		"/ohayou 今日も", "Skill(ohayou)", "Skill(superpowers:brainstorming)",
	} {
		if !bytes.Contains(md, []byte(want)) {
			t.Errorf("md に %q がない", want)
		}
	}
	if bytes.Contains(md, []byte("現れてはならない")) {
		t.Error("スキル展開本文が md に漏れている")
	}
```

html 側にもアサートを追加 (`os.Stat` の確認を読み込みへ変更):

```go
	html, err := os.ReadFile(filepath.Join(outDir, "sess-0001.html"))
	if err != nil {
		t.Fatalf("html が書き出されていない: %v", err)
	}
	if !bytes.Contains(html, []byte(`href="file:///Users/example/.claude/skills/ohayou"`)) {
		t.Error("html にスキルの file:// リンクがない")
	}
	if bytes.Contains(html, []byte("現れてはならない")) {
		t.Error("スキル展開本文が html に漏れている")
	}
```

- [ ] **Step 4: 全テストが通ることを確認する**

Run: `go test ./...`
Expected: 全 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/parse/testdata/session_small.jsonl internal/parse/parse_test.go cmd/cctx/main_test.go
git commit -m "$(cat <<'EOF'
test: E2E フィクスチャにスキル発動の両経路を追加

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 8: README — 外部テンプレート利用者向けドキュメント更新

公開テンプレートAPIの追加分 (イベント種別・SkillInvocation・Stats・fileURL) を README に反映する。

**Files:**
- Modify: `README.md:54-140` (テンプレート変数一覧)

**Interfaces:**
- Consumes: Task 1〜6 で確定した公開名 (`skill_invocation`, `Event.Skill`, `SkillInvocation`, `Stats.SkillInvocations`, `fileURL`)
- Produces: なし

- [ ] **Step 1: README を更新する**

1. `### Stats` の表、`SubagentCalls` 行の下に追加:

```markdown
| `SkillInvocations` | `int` | スキル発動の件数 |
```

2. `### Event` の表、`Kind` の説明を7種に更新:

```markdown
| `Kind` | `string` | イベント種別。`user_message` / `assistant_message` / `tool_call` / `permission_deny` / `system_note` / `subagent_call` / `skill_invocation` の7種 |
```

同じ表の `Subagent` 行の下に追加:

```markdown
| `Skill` | `*SkillInvocation` | `Kind` が `skill_invocation` のとき非 nil |
```

3. `### Subagent` の表の後に新セクションを追加:

```markdown
### SkillInvocation

| フィールド | 型 | 説明 |
| --- | --- | --- |
| `Name` | `string` | スキル名。エージェント発動は Skill ツールの `input.skill`、ユーザー呼び出しはコマンド名の先頭 `/` を除いた形 |
| `Path` | `string` | スキル本体のディレクトリ絶対パス |
| `ByUser` | `bool` | true ならユーザーのスラッシュコマンド呼び出し、false ならエージェントによる Skill ツール発動 |
| `Command` | `string` | `ByUser` のとき「/name 引数」の再現文字列。エージェント発動では空 |

デフォルトテンプレートでの表示例 (md):

```
🔧 Skill(superpowers:brainstorming) `/Users/.../skills/brainstorming`
```

ユーザー呼び出しは 👤 User の発言としてコマンド再現 (`/ohayou 今日も` など) と上記チップを表示します。スキル展開の本文 (手順書) はどの形式でも出力しません。
```

4. `### テンプレート関数` の表に追加:

```markdown
| `fileURL` | `fileURL path` | 絶対パスから `file://` URL を生成する (HTML 用。空白や `#` を %エスケープ)。絶対パスでない場合は空文字列を返す |
```

- [ ] **Step 2: 表示を確認する**

Run: `go test ./...`
Expected: 全 PASS (README 変更はテストに影響しないことの確認)

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "$(cat <<'EOF'
docs: テンプレート変数一覧に skill_invocation と fileURL を追記

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
EOF
)"
```
