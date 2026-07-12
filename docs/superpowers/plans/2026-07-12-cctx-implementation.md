# cctx 実装計画

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Claude Code のセッショントランスクリプト (JSONL) をAI向け markdown と人間向け HTML に整形する CLI `cctx` を作る。

**Architecture:** 単一パス構成。JSONL を読み正規化 `Session` モデルを構築し (internal/parse)、テンプレート (text/template / html/template) でレンダリングする (internal/render)。入力解決は internal/locate、`--translate` の `claude -p` 呼び出しは internal/translate に分離。

**Tech Stack:** Go (標準ライブラリのみ)。テンプレートは go:embed で同梱。

**Spec:** `docs/superpowers/specs/2026-07-12-cctx-design.md` (本計画の唯一の仕様源。齟齬があれば設計書が正)

## Global Constraints

- 依存は Go 標準ライブラリのみ。`go.mod` に require を一切追加しない
- モジュール名は `cctx` (ファイルシステムのパスに依存させない)
- 入力パス (`~/.claude/projects` 等) をハードコードしない。デフォルト値は `os.UserHomeDir()` から組み立て、フラグで上書き可能にする
- 失敗時はフォールバックせず報告する。唯一の例外は subagents/ ファイル欠損時の明示的な縮退 (stderr 警告 + 本流 tool_result 使用)
- ユーザー向けメッセージ (エラー・警告) は日本語。丸括弧は半角 `(...)`
- テスト fixture は実ログから転記せず、個人情報を含まない捏造データで作成する
- 各タスクの Interfaces 節に書かれた型・関数名は後続タスクが依存する。勝手に改名しない

---

### Task 1: モジュール初期化とレコードデコード層

**Files:**
- Create: `go.mod`
- Create: `internal/parse/model.go`
- Create: `internal/parse/record.go`
- Test: `internal/parse/record_test.go`

**Interfaces:**
- Consumes: なし (最初のタスク)
- Produces:
  - `parse.Session / Event / ToolCall / Subagent / Stats / EventKind` (model.go の定義そのまま)
  - `rawRecord` / `rawMessage` / `contentBlock` (parse パッケージ内部)
  - `(*rawMessage).blocks() []contentBlock` — content の文字列形/ブロック配列形を吸収
  - `(contentBlock).resultText() string` — tool_result の content (文字列形/配列形) をテキスト化
  - `(contentBlock).resultString() (string, bool)` — content が**文字列形のときだけ** ok=true (拒否検出用)

- [ ] **Step 1: モジュール初期化**

```bash
cd ~/Personal/develop/cctx
go mod init cctx
```

- [ ] **Step 2: データモデルを書く**

`internal/parse/model.go`:

```go
// Package parse は Claude Code のトランスクリプト JSONL を正規化 Session モデルへ変換する。
// 生ログのスキーマ解釈はこのパッケージに閉じる。
package parse

import "time"

// EventKind はイベント種別。テンプレートから eq で比較できるよう文字列の型エイリアスにする。
type EventKind = string

const (
	KindUserMessage      EventKind = "user_message"
	KindAssistantMessage EventKind = "assistant_message"
	KindToolCall         EventKind = "tool_call"
	KindPermissionDeny   EventKind = "permission_deny"
	KindSystemNote       EventKind = "system_note"
	KindSubagentCall     EventKind = "subagent_call"
)

type Session struct {
	ID          string
	ProjectPath string // レコードの cwd フィールドから取得
	StartedAt   time.Time
	EndedAt     time.Time
	Events      []Event
	Stats       Stats
}

type Stats struct {
	UserMessages      int
	AssistantMessages int
	ToolCalls         int
	PermissionDenies  int
	SystemNotes       int
	SubagentCalls     int
	SkippedLines      int // パースできなかった行数
}

type Event struct {
	Kind      EventKind
	Timestamp time.Time
	Text      string    // UserMessage / AssistantMessage / SystemNote の本文
	Tool      *ToolCall // Kind が ToolCall / PermissionDeny のとき非 nil
	Subagent  *Subagent // Kind が SubagentCall のとき非 nil
}

type ToolCall struct {
	Name       string
	Summary    string // ツールごとの要約 (Bash ならコマンド、Edit ならパス)
	Input      string // 全パラメータの整形 JSON
	Result     string
	HasResult  bool // 対応する tool_result が見つかったか
	IsError    bool
	DenyReason string // 権限拒否時にユーザーが添えたメッセージ (無ければ空)
}

type Subagent struct {
	AgentID   string
	AgentType string
	Prompt    string // 依頼プロンプト全文
	Answer    string // 最終回答全文
}
```

- [ ] **Step 3: 失敗するテストを書く**

`internal/parse/record_test.go`:

```go
package parse

import "testing"

func TestBlocksStringContent(t *testing.T) {
	m := &rawMessage{Role: "user", Content: []byte(`"こんにちは"`)}
	got := m.blocks()
	if len(got) != 1 || got[0].Type != "text" || got[0].Text != "こんにちは" {
		t.Fatalf("blocks() = %+v", got)
	}
}

func TestBlocksArrayContent(t *testing.T) {
	m := &rawMessage{Role: "assistant", Content: []byte(
		`[{"type":"text","text":"a"},{"type":"tool_use","id":"tu1","name":"Bash","input":{"command":"ls"}}]`)}
	got := m.blocks()
	if len(got) != 2 || got[0].Text != "a" || got[1].Name != "Bash" || got[1].ID != "tu1" {
		t.Fatalf("blocks() = %+v", got)
	}
}

func TestBlocksNil(t *testing.T) {
	var m *rawMessage
	if got := m.blocks(); got != nil {
		t.Fatalf("nil message: %+v", got)
	}
}

func TestResultTextString(t *testing.T) {
	b := contentBlock{Content: []byte(`"line1\nline2"`)}
	if got := b.resultText(); got != "line1\nline2" {
		t.Fatalf("resultText() = %q", got)
	}
}

func TestResultTextArray(t *testing.T) {
	b := contentBlock{Content: []byte(`[{"type":"text","text":"p1"},{"type":"text","text":"p2"}]`)}
	if got := b.resultText(); got != "p1\np2" {
		t.Fatalf("resultText() = %q", got)
	}
}

func TestResultStringOnlyString(t *testing.T) {
	if _, ok := (contentBlock{Content: []byte(`[{"type":"text","text":"x"}]`)}).resultString(); ok {
		t.Fatal("配列形で ok=true になった")
	}
	s, ok := (contentBlock{Content: []byte(`"deny text"`)}).resultString()
	if !ok || s != "deny text" {
		t.Fatalf("resultString() = %q, %v", s, ok)
	}
}
```

- [ ] **Step 4: テストが失敗することを確認**

Run: `go test ./internal/parse/ -v`
Expected: FAIL (rawMessage 未定義のコンパイルエラー)

- [ ] **Step 5: レコードデコード層を実装**

`internal/parse/record.go`:

```go
package parse

import (
	"encoding/json"
	"strings"
)

// rawRecord はトランスクリプト JSONL の1行。出力に使わないフィールドは持たない。
type rawRecord struct {
	Type        string      `json:"type"`
	Subtype     string      `json:"subtype"`
	UUID        string      `json:"uuid"`
	IsSidechain bool        `json:"isSidechain"`
	CWD         string      `json:"cwd"`
	SessionID   string      `json:"sessionId"`
	Timestamp   string      `json:"timestamp"`
	Message     *rawMessage `json:"message"`
}

type rawMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// contentBlock は message.content の1ブロック。text / tool_use / tool_result を兼ねる。
type contentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	IsError   bool            `json:"is_error"`
	Content   json.RawMessage `json:"content"`
}

// blocks は content の2形態 (文字列 / ブロック配列) を吸収して返す。
func (m *rawMessage) blocks() []contentBlock {
	if m == nil || len(m.Content) == 0 {
		return nil
	}
	var s string
	if err := json.Unmarshal(m.Content, &s); err == nil {
		return []contentBlock{{Type: "text", Text: s}}
	}
	var bs []contentBlock
	if err := json.Unmarshal(m.Content, &bs); err == nil {
		return bs
	}
	return nil
}

// resultText は tool_result の content をテキスト化する (文字列形はそのまま、配列形は text を連結)。
func (b contentBlock) resultText() string {
	if len(b.Content) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(b.Content, &s); err == nil {
		return s
	}
	var bs []contentBlock
	if err := json.Unmarshal(b.Content, &bs); err == nil {
		var parts []string
		for _, x := range bs {
			if x.Type == "text" {
				parts = append(parts, x.Text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

// resultString は content が文字列形のときだけ中身を返す。権限拒否の判定は文字列形に限る (設計書)。
func (b contentBlock) resultString() (string, bool) {
	var s string
	if err := json.Unmarshal(b.Content, &s); err != nil {
		return "", false
	}
	return s, true
}
```

- [ ] **Step 6: テストが通ることを確認**

Run: `go test ./internal/parse/ -v`
Expected: PASS (6件)

- [ ] **Step 7: コミット**

```bash
git add go.mod internal/parse/
git commit -m "feat: parse のデータモデルとレコードデコード層を追加"
```

---

### Task 2: JSONL 読み込みと発話イベント抽出

**Files:**
- Create: `internal/parse/parse.go`
- Create: `internal/parse/noise.go`
- Create: `internal/parse/testdata/session_small.jsonl`
- Create: `internal/parse/testdata/session_small/subagents/agent-abc123.meta.json`
- Create: `internal/parse/testdata/session_small/subagents/agent-abc123.jsonl`
- Test: `internal/parse/parse_test.go`

**Interfaces:**
- Consumes: Task 1 の rawRecord / blocks()
- Produces:
  - `parse.ParseFile(path string, warn io.Writer) (*Session, error)` — warn は縮退警告の出力先 (nil 可)。全行パース失敗ならエラー
  - fixture `testdata/session_small.jsonl` (後続タスクのテストが共用する。**行の追加・変更は後続タスクのテストを壊すため禁止**)
  - テストヘルパー `eventsOfKind(s *Session, k EventKind) []Event`

- [ ] **Step 1: fixture を作成する**

`internal/parse/testdata/session_small.jsonl` (捏造データ。1行1レコード、以下の11行をそのまま):

```json
{"type":"user","uuid":"u1","isSidechain":false,"cwd":"/Users/example/proj","sessionId":"sess-0001","timestamp":"2026-07-12T00:00:01Z","message":{"role":"user","content":"こんにちは\n<system-reminder>これはノイズ</system-reminder>"}}
{"type":"assistant","uuid":"a1","isSidechain":false,"cwd":"/Users/example/proj","sessionId":"sess-0001","timestamp":"2026-07-12T00:00:02Z","message":{"role":"assistant","content":[{"type":"text","text":"確認します"},{"type":"tool_use","id":"tu1","name":"Bash","input":{"command":"ls -la","description":"ファイル一覧"}}]}}
{"type":"user","uuid":"u2","isSidechain":false,"cwd":"/Users/example/proj","sessionId":"sess-0001","timestamp":"2026-07-12T00:00:03Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu1","is_error":false,"content":"file1\nfile2\nfile3"}]}}
{"type":"assistant","uuid":"a2","isSidechain":false,"cwd":"/Users/example/proj","sessionId":"sess-0001","timestamp":"2026-07-12T00:00:04Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"tu2","name":"Edit","input":{"file_path":"/tmp/x.txt","old_string":"a","new_string":"b"}}]}}
{"type":"user","uuid":"u3","isSidechain":false,"cwd":"/Users/example/proj","sessionId":"sess-0001","timestamp":"2026-07-12T00:00:05Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu2","is_error":true,"content":"The user doesn't want to proceed with this tool use. The tool use was rejected. To tell you how to proceed, the user said:\nこっちは触らないで\n\nNote: The user's next message may contain a correction."}]}}
{"type":"assistant","uuid":"a3","isSidechain":false,"cwd":"/Users/example/proj","sessionId":"sess-0001","timestamp":"2026-07-12T00:00:06Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"tu3","name":"Agent","input":{"description":"リポジトリ調査","prompt":"Please investigate the repository structure.","subagent_type":"general-purpose"}}]}}
{"type":"user","uuid":"u4","isSidechain":false,"cwd":"/Users/example/proj","sessionId":"sess-0001","timestamp":"2026-07-12T00:00:07Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu3","is_error":null,"content":[{"type":"text","text":"Investigation done.\n\nagentId: abc123 (use SendMessage to continue)"}]}]}}
{"type":"system","subtype":"compact_boundary","uuid":"s1","isSidechain":false,"cwd":"/Users/example/proj","sessionId":"sess-0001","timestamp":"2026-07-12T00:00:08Z","content":"Conversation compacted"}
{"type":"assistant","uuid":"a4","isSidechain":false,"cwd":"/Users/example/proj","sessionId":"sess-0001","timestamp":"2026-07-12T00:00:09Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"tu4","name":"Read","input":{"file_path":"/tmp/y.txt"}}]}}
{oops this line is broken
{"type":"user","uuid":"sc1","isSidechain":true,"cwd":"/Users/example/proj","sessionId":"sess-0001","timestamp":"2026-07-12T00:00:10Z","message":{"role":"user","content":"サイドチェーンの発話 (本流では無視される)"}}
```

`internal/parse/testdata/session_small/subagents/agent-abc123.meta.json`:

```json
{"agentType":"general-purpose","description":"リポジトリ調査","toolUseId":"tu3","spawnDepth":1}
```

`internal/parse/testdata/session_small/subagents/agent-abc123.jsonl`:

```json
{"type":"user","uuid":"sa-u1","isSidechain":true,"agentId":"abc123","cwd":"/Users/example/proj","sessionId":"sess-0001","timestamp":"2026-07-12T00:00:06Z","message":{"role":"user","content":"Please investigate the repository structure."}}
{"type":"assistant","uuid":"sa-a1","isSidechain":true,"agentId":"abc123","cwd":"/Users/example/proj","sessionId":"sess-0001","timestamp":"2026-07-12T00:00:07Z","message":{"role":"assistant","content":[{"type":"text","text":"Investigation summary: the repo has 3 packages."}]}}
```

- [ ] **Step 2: 失敗するテストを書く**

`internal/parse/parse_test.go`:

```go
package parse

import (
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
	// 末尾レコードは isSidechain (無視) なので、直前の a4 (00:00:09Z) が EndedAt
	wantEnd := time.Date(2026, 7, 12, 0, 0, 9, 0, time.UTC)
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
```

テスト補助 (parse_test.go 末尾に):

```go
func writeFile(path, content string) error {
	return osWriteFile(path, []byte(content), 0o644)
}
```

※ `osWriteFile` は `os.WriteFile` を import して使う (`import "os"` して `os.WriteFile` を直接呼ぶ形に書き換えてよい)。

- [ ] **Step 3: テストが失敗することを確認**

Run: `go test ./internal/parse/ -v`
Expected: FAIL (ParseFile / stripNoise 未定義)

- [ ] **Step 4: ノイズ除去と ParseFile を実装**

`internal/parse/noise.go`:

```go
package parse

import (
	"regexp"
	"strings"
)

// ユーザー発話から除去するノイズ。system-reminder ブロックとスキル/コマンド展開のタグ。
var noiseRE = regexp.MustCompile(`(?s)` +
	`<system-reminder>.*?</system-reminder>` +
	`|<command-name>.*?</command-name>` +
	`|<command-message>.*?</command-message>` +
	`|<command-args>.*?</command-args>` +
	`|<local-command-stdout>.*?</local-command-stdout>` +
	`|<local-command-caveat>.*?</local-command-caveat>`)

func stripNoise(s string) string {
	return strings.TrimSpace(noiseRE.ReplaceAllString(s, ""))
}
```

`internal/parse/parse.go` (このタスクでは発話イベントのみ。tool_use / system は後続タスクで拡張):

```go
package parse

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"
)

// ParseFile はトランスクリプト JSONL を読み、正規化 Session を返す。
// warn には縮退警告を書く (nil なら破棄)。全行パース失敗はエラー。
func ParseFile(path string, warn io.Writer) (*Session, error) {
	if warn == nil {
		warn = io.Discard
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("トランスクリプトを開けません: %w", err)
	}
	defer f.Close()

	records, skipped, err := readRecords(f)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("有効な行がありません (%s、スキップ %d行)", path, skipped)
	}

	s := &Session{}
	for _, rec := range records {
		if s.ID == "" {
			s.ID = rec.SessionID
		}
		if s.ProjectPath == "" {
			s.ProjectPath = rec.CWD
		}
		ts := parseTime(rec.Timestamp)
		if !ts.IsZero() {
			if s.StartedAt.IsZero() {
				s.StartedAt = ts
			}
			s.EndedAt = ts
		}
		s.Events = append(s.Events, buildEvents(rec, ts)...)
	}
	s.Stats = computeStats(s.Events, skipped)
	return s, nil
}

// readRecords は全行をデコードする。壊れた行と isSidechain=true はスキップして数える。
func readRecords(f io.Reader) ([]rawRecord, int, error) {
	r := bufio.NewReaderSize(f, 1<<20)
	var records []rawRecord
	skipped := 0
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 1 {
			var rec rawRecord
			if json.Unmarshal(line, &rec) != nil {
				skipped++
			} else if !rec.IsSidechain {
				records = append(records, rec)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, 0, fmt.Errorf("読み込みエラー: %w", err)
		}
	}
	return records, skipped, nil
}

func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// buildEvents は1レコードをイベント列へ変換する (このタスクでは発話のみ)。
func buildEvents(rec rawRecord, ts time.Time) []Event {
	var events []Event
	switch rec.Type {
	case "user":
		for _, b := range rec.Message.blocks() {
			if b.Type != "text" {
				continue
			}
			if text := stripNoise(b.Text); text != "" {
				events = append(events, Event{Kind: KindUserMessage, Timestamp: ts, Text: text})
			}
		}
	case "assistant":
		for _, b := range rec.Message.blocks() {
			if b.Type != "text" {
				continue
			}
			if text := stripNoise(b.Text); text != "" {
				events = append(events, Event{Kind: KindAssistantMessage, Timestamp: ts, Text: text})
			}
		}
	}
	return events
}

func computeStats(events []Event, skipped int) Stats {
	st := Stats{SkippedLines: skipped}
	for _, e := range events {
		switch e.Kind {
		case KindUserMessage:
			st.UserMessages++
		case KindAssistantMessage:
			st.AssistantMessages++
		case KindToolCall:
			st.ToolCalls++
		case KindPermissionDeny:
			st.PermissionDenies++
		case KindSystemNote:
			st.SystemNotes++
		case KindSubagentCall:
			st.SubagentCalls++
		}
	}
	return st
}
```

- [ ] **Step 5: テストが通ることを確認**

Run: `go test ./internal/parse/ -v`
Expected: PASS (Task 1 の6件 + 本タスク5件)

- [ ] **Step 6: コミット**

```bash
git add internal/parse/
git commit -m "feat: JSONL 読み込みと発話イベント抽出を追加"
```

---

### Task 3: ToolCall 統合 (tool_use ↔ tool_result)

**Files:**
- Create: `internal/parse/summary.go`
- Modify: `internal/parse/parse.go` (buildEvents を tool_use 対応に拡張)
- Test: `internal/parse/toolcall_test.go`

**Interfaces:**
- Consumes: Task 2 の ParseFile / fixture / eventsOfKind
- Produces:
  - `toolSummary(name string, input json.RawMessage) string`
  - `formatInput(input json.RawMessage) string` — 整形 JSON
  - buildEvents のシグネチャ変更: `buildEvents(rec rawRecord, ts time.Time, results map[string]contentBlock) []Event`
  - `collectToolResults(records []rawRecord) map[string]contentBlock` — tool_use_id → tool_result ブロック

- [ ] **Step 1: 失敗するテストを書く**

`internal/parse/toolcall_test.go`:

```go
package parse

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestToolCallMatched(t *testing.T) {
	s := mustParse(t)
	got := eventsOfKind(s, KindToolCall)
	// fixture 中の ToolCall は tu1 (Bash, 結果あり) と tu4 (Read, 結果なし) の2件。
	// tu2 は PermissionDeny、tu3 は SubagentCall で、それぞれ後続タスックで扱う
	// (本タスク時点では tu2/tu3 も ToolCall として数えられるため、名前で特定して検証する)
	var bash, read *ToolCall
	for i := range got {
		switch got[i].Tool.Name {
		case "Bash":
			bash = got[i].Tool
		case "Read":
			read = got[i].Tool
		}
	}
	if bash == nil || read == nil {
		t.Fatalf("Bash/Read の ToolCall が見つからない: %+v", got)
	}
	if !bash.HasResult || bash.Result != "file1\nfile2\nfile3" || bash.IsError {
		t.Errorf("Bash: %+v", bash)
	}
	if bash.Summary != "ls -la" {
		t.Errorf("Bash Summary = %q", bash.Summary)
	}
	if read.HasResult || read.Result != "" {
		t.Errorf("結果なし tool_use: %+v", read)
	}
	if read.Summary != "/tmp/y.txt" {
		t.Errorf("Read Summary = %q", read.Summary)
	}
}

func TestToolSummary(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"Bash", `{"command":"go test ./...","description":"テスト"}`, "go test ./..."},
		{"Edit", `{"file_path":"/a/b.go","old_string":"x"}`, "/a/b.go"},
		{"Read", `{"file_path":"/a/c.go"}`, "/a/c.go"},
		{"Agent", `{"description":"調査","prompt":"..."}`, "調査"},
		{"Grep", `{"pattern":"foo","path":"/src"}`, "foo"},
		{"UnknownTool", `{"other":"x"}`, ""},
	}
	for _, c := range cases {
		if got := toolSummary(c.name, json.RawMessage(c.input)); got != c.want {
			t.Errorf("toolSummary(%s) = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestFormatInput(t *testing.T) {
	got := formatInput(json.RawMessage(`{"command":"ls","description":"一覧"}`))
	if !strings.Contains(got, "\n") || !strings.Contains(got, `"command": "ls"`) {
		t.Errorf("整形されていない: %q", got)
	}
}
```

- [ ] **Step 2: テストが失敗することを確認**

Run: `go test ./internal/parse/ -run 'TestTool|TestFormat' -v`
Expected: FAIL (toolSummary / formatInput 未定義)

- [ ] **Step 3: summary と突き合わせを実装**

`internal/parse/summary.go`:

```go
package parse

import "encoding/json"

// toolSummary はツール呼び出しの1行要約を返す。ツール固有の主要パラメータを優先し、
// 未知のツールは汎用キーの探索にフォールバックする (どれも無ければ空)。
func toolSummary(name string, input json.RawMessage) string {
	var m map[string]any
	_ = json.Unmarshal(input, &m)
	str := func(k string) string {
		s, _ := m[k].(string)
		return s
	}
	switch name {
	case "Bash":
		if s := str("command"); s != "" {
			return s
		}
	case "Read", "Edit", "Write", "NotebookEdit":
		if s := str("file_path"); s != "" {
			return s
		}
	case "Agent":
		if s := str("description"); s != "" {
			return s
		}
	}
	for _, k := range []string{"description", "command", "file_path", "path", "pattern", "query", "skill", "url"} {
		if s := str(k); s != "" {
			return s
		}
	}
	return ""
}

// formatInput は input JSON をインデント付きで整形する。壊れていれば原文のまま返す。
func formatInput(input json.RawMessage) string {
	var v any
	if err := json.Unmarshal(input, &v); err != nil {
		return string(input)
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return string(input)
	}
	return string(b)
}
```

`internal/parse/parse.go` の変更。ParseFile のループ前に突き合わせマップを作り、buildEvents へ渡す:

```go
	// (ParseFile 内、records 取得後に追加)
	results := collectToolResults(records)
```

```go
	// (ループ内の呼び出しを変更)
	s.Events = append(s.Events, buildEvents(rec, ts, results)...)
```

追加する関数と buildEvents の拡張:

```go
// collectToolResults は user レコード中の tool_result を tool_use_id で索引化する。
func collectToolResults(records []rawRecord) map[string]contentBlock {
	m := map[string]contentBlock{}
	for _, rec := range records {
		if rec.Type != "user" {
			continue
		}
		for _, b := range rec.Message.blocks() {
			if b.Type == "tool_result" && b.ToolUseID != "" {
				m[b.ToolUseID] = b
			}
		}
	}
	return m
}

func buildEvents(rec rawRecord, ts time.Time, results map[string]contentBlock) []Event {
	var events []Event
	switch rec.Type {
	case "user":
		for _, b := range rec.Message.blocks() {
			if b.Type != "text" {
				continue // tool_result は tool_use 側で統合済み
			}
			if text := stripNoise(b.Text); text != "" {
				events = append(events, Event{Kind: KindUserMessage, Timestamp: ts, Text: text})
			}
		}
	case "assistant":
		for _, b := range rec.Message.blocks() {
			switch b.Type {
			case "text":
				if text := stripNoise(b.Text); text != "" {
					events = append(events, Event{Kind: KindAssistantMessage, Timestamp: ts, Text: text})
				}
			case "tool_use":
				events = append(events, toolEvent(b, ts, results))
			}
		}
	}
	return events
}

// toolEvent は tool_use ブロックを ToolCall イベントへ変換する。
func toolEvent(b contentBlock, ts time.Time, results map[string]contentBlock) Event {
	tc := &ToolCall{
		Name:    b.Name,
		Summary: toolSummary(b.Name, b.Input),
		Input:   formatInput(b.Input),
	}
	if res, ok := results[b.ID]; ok {
		tc.HasResult = true
		tc.Result = res.resultText()
		tc.IsError = res.IsError
	}
	return Event{Kind: KindToolCall, Timestamp: ts, Tool: tc}
}
```

- [ ] **Step 4: テストが通ることを確認**

Run: `go test ./internal/parse/ -v`
Expected: PASS (全件)

- [ ] **Step 5: コミット**

```bash
git add internal/parse/
git commit -m "feat: tool_use と tool_result の統合を追加"
```

---

### Task 4: PermissionDeny 検出

**Files:**
- Create: `internal/parse/deny.go`
- Modify: `internal/parse/parse.go` (toolEvent に拒否判定を追加)
- Test: `internal/parse/deny_test.go`

**Interfaces:**
- Consumes: Task 3 の toolEvent / resultString
- Produces: `denyReason(content string) (reason string, ok bool)`

- [ ] **Step 1: 失敗するテストを書く**

`internal/parse/deny_test.go`:

```go
package parse

import "testing"

func TestDenyReason(t *testing.T) {
	cases := []struct {
		name    string
		content string
		wantMsg string
		wantOK  bool
	}{
		{
			"メッセージ付き + Note 除去",
			"The user doesn't want to proceed with this tool use. The tool use was rejected. To tell you how to proceed, the user said:\nこっちは触らないで\n\nNote: The user's next message may contain a correction.",
			"こっちは触らないで", true,
		},
		{
			"メッセージなし",
			"The user doesn't want to proceed with this tool use. STOP what you are doing.",
			"", true,
		},
		{
			"本文が Note: を含んでも過剰に切らない",
			"The user doesn't want to proceed with this tool use. the user said:\n手順は\n\nNote: を参照して",
			"手順は\n\nNote: を参照して", true,
		},
		{
			"自動拒否は対象外",
			"Permission to use Bash with command rm has been denied.",
			"", false,
		},
		{
			"通常のツールエラーは対象外",
			"exit status 1: command not found",
			"", false,
		},
	}
	for _, c := range cases {
		msg, ok := denyReason(c.content)
		if ok != c.wantOK || msg != c.wantMsg {
			t.Errorf("%s: denyReason = (%q, %v), want (%q, %v)", c.name, msg, ok, c.wantMsg, c.wantOK)
		}
	}
}

func TestPermissionDenyEvent(t *testing.T) {
	s := mustParse(t)
	got := eventsOfKind(s, KindPermissionDeny)
	if len(got) != 1 {
		t.Fatalf("PermissionDeny %d件", len(got))
	}
	tc := got[0].Tool
	if tc.Name != "Edit" || tc.DenyReason != "こっちは触らないで" || !tc.IsError {
		t.Errorf("PermissionDeny: %+v", tc)
	}
}

func TestDenyRequiresIsError(t *testing.T) {
	// is_error=false で文言に前方一致する行 (引用) は検出しない
	b := contentBlock{IsError: false, Content: []byte(`"The user doesn't want to proceed with this tool use."`)}
	ev := toolEvent(contentBlock{Type: "tool_use", ID: "x", Name: "Bash"}, timeZero(), map[string]contentBlock{"x": b})
	if ev.Kind != KindToolCall {
		t.Errorf("引用行を拒否として誤検出: %v", ev.Kind)
	}
}

func TestDenyArrayContentNotDetected(t *testing.T) {
	// content が配列形の拒否文言はクラッシュせず非該当
	b := contentBlock{IsError: true, Content: []byte(`[{"type":"text","text":"The user doesn't want to proceed with this tool use."}]`)}
	ev := toolEvent(contentBlock{Type: "tool_use", ID: "x", Name: "Bash"}, timeZero(), map[string]contentBlock{"x": b})
	if ev.Kind != KindToolCall {
		t.Errorf("配列形を拒否として検出: %v", ev.Kind)
	}
}
```

テスト補助 (deny_test.go 末尾に):

```go
import "time" // ファイル先頭の import に追加

func timeZero() time.Time { return time.Time{} }
```

- [ ] **Step 2: テストが失敗することを確認**

Run: `go test ./internal/parse/ -run TestDeny -v`
Expected: FAIL (denyReason 未定義)

- [ ] **Step 3: 拒否検出を実装**

`internal/parse/deny.go`:

```go
package parse

import "strings"

// 権限拒否の判定仕様は設計書「権限拒否の検出仕様」節に従う (ccmetrics 実データ調査由来)。
// 前方一致にする理由: 部分一致は拒否文言を引用した行を誤検出する。
const (
	denyPrefix     = "The user doesn't want to proceed with this tool use."
	denySaidMarker = "the user said:\n"
	// アンカーを "\n\nNote:" だけにするとユーザー本文を過剰に切るため、定型文の長い接頭辞で切る
	denyNotePrefix = "\n\nNote: The user's next message"
)

// denyReason は tool_result の content (文字列形) が権限拒否かを判定し、添付メッセージを返す。
func denyReason(content string) (string, bool) {
	if !strings.HasPrefix(content, denyPrefix) {
		return "", false
	}
	msg := ""
	if i := strings.Index(content, denySaidMarker); i >= 0 {
		msg = content[i+len(denySaidMarker):]
		if j := strings.Index(msg, denyNotePrefix); j >= 0 {
			msg = msg[:j]
		}
	}
	return strings.TrimSpace(msg), true
}
```

`internal/parse/parse.go` の toolEvent に拒否判定を追加 (関数全体を置き換え):

```go
func toolEvent(b contentBlock, ts time.Time, results map[string]contentBlock) Event {
	tc := &ToolCall{
		Name:    b.Name,
		Summary: toolSummary(b.Name, b.Input),
		Input:   formatInput(b.Input),
	}
	kind := KindToolCall
	if res, ok := results[b.ID]; ok {
		tc.HasResult = true
		tc.Result = res.resultText()
		tc.IsError = res.IsError
		if res.IsError {
			if s, isStr := res.resultString(); isStr {
				if reason, deny := denyReason(s); deny {
					kind = KindPermissionDeny
					tc.DenyReason = reason
				}
			}
		}
	}
	return Event{Kind: kind, Timestamp: ts, Tool: tc}
}
```

- [ ] **Step 4: テストが通ることを確認**

Run: `go test ./internal/parse/ -v`
Expected: PASS (全件。TestToolCallMatched は名前で特定しているため影響なし)

- [ ] **Step 5: コミット**

```bash
git add internal/parse/
git commit -m "feat: 権限拒否 (PermissionDeny) の検出を追加"
```

---

### Task 5: SubagentCall (subagents/ 紐付け)

**Files:**
- Create: `internal/parse/subagent.go`
- Modify: `internal/parse/parse.go` (Agent tool_use の分岐を追加)
- Test: `internal/parse/subagent_test.go`

**Interfaces:**
- Consumes: Task 3 の toolEvent、Task 2 の fixture (subagents/ ディレクトリ含む)
- Produces:
  - `loadSubagents(dir string) map[string]Subagent` — key は meta.json の toolUseId。Answer 解決済み
  - toolEvent のシグネチャ変更: `toolEvent(b contentBlock, ts time.Time, results map[string]contentBlock, subs map[string]Subagent, warn io.Writer) Event`

- [ ] **Step 1: 失敗するテストを書く**

`internal/parse/subagent_test.go`:

```go
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
```

- [ ] **Step 2: テストが失敗することを確認**

Run: `go test ./internal/parse/ -run TestSubagent -v` および `-run TestLoadSubagents`
Expected: FAIL (loadSubagents 未定義、SubagentCall 0件)

- [ ] **Step 3: サブエージェント解決を実装**

`internal/parse/subagent.go`:

```go
package parse

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type subagentMeta struct {
	AgentType string `json:"agentType"`
	ToolUseID string `json:"toolUseId"`
}

// loadSubagents は subagents/ 配下の *.meta.json を走査し、toolUseId で索引化して返す。
// meta.json の toolUseId が本流の Agent tool_use の id と1対1対応する (設計書・実地調査)。
// Answer は agent-<id>.jsonl の最後の assistant レコードの text ブロック連結。
func loadSubagents(dir string) map[string]Subagent {
	m := map[string]Subagent{}
	metas, err := filepath.Glob(filepath.Join(dir, "*.meta.json"))
	if err != nil {
		return m
	}
	for _, mp := range metas {
		b, err := os.ReadFile(mp)
		if err != nil {
			continue
		}
		var meta subagentMeta
		if json.Unmarshal(b, &meta) != nil || meta.ToolUseID == "" {
			continue
		}
		jsonlPath := strings.TrimSuffix(mp, ".meta.json") + ".jsonl"
		answer, err := subagentAnswer(jsonlPath)
		if err != nil {
			continue // 回答が読めない記録は未解決扱い (呼び出し側で縮退警告)
		}
		id := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(mp), "agent-"), ".meta.json")
		m[meta.ToolUseID] = Subagent{AgentID: id, AgentType: meta.AgentType, Answer: answer}
	}
	return m
}

// subagentAnswer はサブエージェント JSONL の最後の assistant レコードのテキストを返す。
// 本流側の tool_result はハーネスが agentId 行と <usage> タグを付加するため使わない (設計書)。
func subagentAnswer(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	records, _, err := readRecords(f) // isSidechain=true をスキップしない読み方が必要 → 下記注意
	if err != nil {
		return "", err
	}
	answer := ""
	for _, rec := range records {
		if rec.Type != "assistant" {
			continue
		}
		var parts []string
		for _, b := range rec.Message.blocks() {
			if b.Type == "text" && b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
		if len(parts) > 0 {
			answer = strings.Join(parts, "\n\n")
		}
	}
	return answer, nil
}
```

**注意:** `readRecords` は Task 2 で isSidechain=true を除外しているが、サブエージェント JSONL は全レコードが isSidechain=true。`readRecords` にフラグを追加する:

```go
// readRecords の変更 (parse.go)
func readRecords(f io.Reader, keepSidechain bool) ([]rawRecord, int, error) {
	// ... 既存実装の条件を変更:
	// } else if keepSidechain || !rec.IsSidechain {
}
```

呼び出し側: ParseFile 内は `readRecords(f, false)`、subagentAnswer 内は `readRecords(f, true)`。

`internal/parse/parse.go` の変更。ParseFile で subagents/ を読み込み toolEvent へ渡す:

```go
	// (ParseFile 内、collectToolResults の直後に追加)
	subs := loadSubagents(strings.TrimSuffix(path, ".jsonl") + "/subagents")
```

buildEvents / toolEvent に subs と warn を引き回す (シグネチャ変更):

```go
func buildEvents(rec rawRecord, ts time.Time, results map[string]contentBlock, subs map[string]Subagent, warn io.Writer) []Event
// tool_use ブロックの分岐:
//   events = append(events, toolEvent(b, ts, results, subs, warn))

func toolEvent(b contentBlock, ts time.Time, results map[string]contentBlock, subs map[string]Subagent, warn io.Writer) Event {
	if b.Name == "Agent" {
		return subagentEvent(b, ts, results, subs, warn)
	}
	// ... 既存の ToolCall / PermissionDeny 処理はそのまま
}

// subagentEvent は Agent tool_use を SubagentCall イベントへ変換する。
func subagentEvent(b contentBlock, ts time.Time, results map[string]contentBlock, subs map[string]Subagent, warn io.Writer) Event {
	var input struct {
		Prompt      string `json:"prompt"`
		SubagentType string `json:"subagent_type"`
	}
	_ = json.Unmarshal(b.Input, &input)
	sub, found := subs[b.ID]
	sub.Prompt = input.Prompt
	if sub.AgentType == "" {
		sub.AgentType = input.SubagentType
	}
	if !found {
		// 明示的な縮退: 本流 tool_result の text で代替し、警告を出す (無言のフォールバックはしない)
		fmt.Fprintf(warn, "cctx: 警告: サブエージェント記録が見つかりません (tool_use %s)。本流の tool_result で代替します\n", b.ID)
		if res, ok := results[b.ID]; ok {
			sub.Answer = res.resultText()
		}
	}
	return Event{Kind: KindSubagentCall, Timestamp: ts, Subagent: &sub}
}
```

parse.go の import に `strings` を追加する。

- [ ] **Step 4: テストが通ることを確認**

Run: `go test ./internal/parse/ -v`
Expected: PASS (全件)。TestToolCallMatched は Bash/Read のみ名前で拾うため、tu3 が SubagentCall に移っても通る

- [ ] **Step 5: コミット**

```bash
git add internal/parse/
git commit -m "feat: サブエージェント呼び出しの紐付けと埋め込みを追加"
```

---

### Task 6: SystemNote と Stats

**Files:**
- Modify: `internal/parse/parse.go` (buildEvents に system 分岐を追加)
- Test: `internal/parse/system_test.go`

**Interfaces:**
- Consumes: Task 2〜5 の buildEvents / fixture
- Produces: system レコード (subtype=compact_boundary) → SystemNote イベント

- [ ] **Step 1: 失敗するテストを書く**

`internal/parse/system_test.go`:

```go
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
		UserMessages:      1,
		AssistantMessages: 1,
		ToolCalls:         2, // Bash (結果あり) + Read (結果なし)
		PermissionDenies:  1, // Edit
		SystemNotes:       1, // compact_boundary
		SubagentCalls:     1, // Agent
		SkippedLines:      1, // 壊れ行
	}
	if s.Stats != want {
		t.Errorf("Stats = %+v, want %+v", s.Stats, want)
	}
}
```

- [ ] **Step 2: テストが失敗することを確認**

Run: `go test ./internal/parse/ -run 'TestSystemNote|TestStats' -v`
Expected: FAIL (SystemNote 0件、Stats.SystemNotes 不一致)

- [ ] **Step 3: system 分岐を実装**

`internal/parse/parse.go` の buildEvents の switch に追加:

```go
	case "system":
		// 節目情報は最小限 (設計書)。現状は compact 境界のみ
		if rec.Subtype == "compact_boundary" {
			events = append(events, Event{Kind: KindSystemNote, Timestamp: ts, Text: "コンテキスト圧縮 (compact)"})
		}
```

- [ ] **Step 4: テストが通ることを確認**

Run: `go test ./internal/parse/ -v`
Expected: PASS (全件)

- [ ] **Step 5: コミット**

```bash
git add internal/parse/
git commit -m "feat: compact 境界の SystemNote と Stats 検証を追加"
```

---

### Task 7: locate (入力解決)

**Files:**
- Create: `internal/locate/locate.go`
- Test: `internal/locate/locate_test.go`

**Interfaces:**
- Consumes: なし (独立パッケージ)
- Produces:
  - `locate.Resolve(arg string, latest bool, project, projectsDir string) (string, error)`
  - `locate.DefaultProjectsDir() (string, error)` — `~/.claude/projects`

- [ ] **Step 1: 失敗するテストを書く**

`internal/locate/locate_test.go`:

```go
package locate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeProjects は ~/.claude/projects 相当の階層を組み立てる。
func fakeProjects(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mk := func(dir, name string, mtime time.Time) string {
		d := filepath.Join(root, dir)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(d, name)
		if err := os.WriteFile(p, []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, mtime, mtime); err != nil {
			t.Fatal(err)
		}
		return p
	}
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	mk("-Users-x-proj-alpha", "aaaa1111-0000-0000-0000-000000000000.jsonl", base)
	mk("-Users-x-proj-alpha", "bbbb2222-0000-0000-0000-000000000000.jsonl", base.Add(2*time.Hour))
	mk("-Users-x-proj-beta", "aaaa9999-0000-0000-0000-000000000000.jsonl", base.Add(1*time.Hour))
	return root
}

func TestResolveDirectPath(t *testing.T) {
	root := fakeProjects(t)
	p := filepath.Join(root, "-Users-x-proj-alpha", "aaaa1111-0000-0000-0000-000000000000.jsonl")
	got, err := Resolve(p, false, "", root)
	if err != nil || got != p {
		t.Fatalf("Resolve(path) = %q, %v", got, err)
	}
}

func TestResolvePrefixUnique(t *testing.T) {
	root := fakeProjects(t)
	got, err := Resolve("bbbb", false, "", root)
	if err != nil || !strings.HasSuffix(got, "bbbb2222-0000-0000-0000-000000000000.jsonl") {
		t.Fatalf("Resolve(prefix) = %q, %v", got, err)
	}
}

func TestResolvePrefixAmbiguous(t *testing.T) {
	root := fakeProjects(t)
	_, err := Resolve("aaaa", false, "", root)
	if err == nil || !strings.Contains(err.Error(), "複数") {
		t.Fatalf("複数一致でエラーにならない: %v", err)
	}
	// 候補一覧を含むこと
	if !strings.Contains(err.Error(), "aaaa1111") || !strings.Contains(err.Error(), "aaaa9999") {
		t.Errorf("候補一覧がない: %v", err)
	}
}

func TestResolvePrefixNotFound(t *testing.T) {
	root := fakeProjects(t)
	_, err := Resolve("zzzz", false, "", root)
	if err == nil || !strings.Contains(err.Error(), "zzzz") {
		t.Fatalf("0件一致でエラーにならない: %v", err)
	}
}

func TestResolveLatest(t *testing.T) {
	root := fakeProjects(t)
	got, err := Resolve("", true, "", root)
	if err != nil || !strings.HasSuffix(got, "bbbb2222-0000-0000-0000-000000000000.jsonl") {
		t.Fatalf("最新 (mtime 基準) が取れない: %q, %v", got, err)
	}
}

func TestResolveLatestWithProject(t *testing.T) {
	root := fakeProjects(t)
	got, err := Resolve("", true, "beta", root)
	if err != nil || !strings.HasSuffix(got, "aaaa9999-0000-0000-0000-000000000000.jsonl") {
		t.Fatalf("--project 絞り込み: %q, %v", got, err)
	}
}

func TestResolveLatestNoMatch(t *testing.T) {
	root := fakeProjects(t)
	_, err := Resolve("", true, "no-such-project", root)
	if err == nil {
		t.Fatal("--latest 0件でエラーにならない")
	}
}
```

- [ ] **Step 2: テストが失敗することを確認**

Run: `go test ./internal/locate/ -v`
Expected: FAIL (Resolve 未定義)

- [ ] **Step 3: locate を実装**

`internal/locate/locate.go`:

```go
// Package locate は入力指定 (パス / セッションID 前方一致 / --latest) をトランスクリプトのパスへ解決する。
package locate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DefaultProjectsDir は ~/.claude/projects を返す。
func DefaultProjectsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("ホームディレクトリを取得できません: %w", err)
	}
	return filepath.Join(home, ".claude", "projects"), nil
}

// Resolve は入力指定からトランスクリプトのパスを1つに決める。
// arg が非空: まずファイルパスとして存在確認し、無ければセッションID 前方一致。
// latest: mtime が最新の1件 (project はディレクトリ名への部分一致で絞り込み)。
func Resolve(arg string, latest bool, project, projectsDir string) (string, error) {
	if arg != "" {
		if fi, err := os.Stat(arg); err == nil && !fi.IsDir() {
			return arg, nil
		}
		pattern := filepath.Join(projectsDir, "*", arg+"*.jsonl")
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return "", err
		}
		switch len(matches) {
		case 0:
			return "", fmt.Errorf("セッションID %q に一致するトランスクリプトがありません (探索: %s)", arg, pattern)
		case 1:
			return matches[0], nil
		default:
			return "", fmt.Errorf("セッションID %q は複数一致します。ID を長くしてください:\n  %s",
				arg, strings.Join(matches, "\n  "))
		}
	}
	if !latest {
		return "", fmt.Errorf("入力を指定してください (パス / セッションID / --latest)")
	}
	matches, err := filepath.Glob(filepath.Join(projectsDir, "*", "*.jsonl"))
	if err != nil {
		return "", err
	}
	var best string
	var bestT time.Time
	for _, m := range matches {
		if project != "" && !strings.Contains(filepath.Base(filepath.Dir(m)), project) {
			continue
		}
		fi, err := os.Stat(m)
		if err != nil {
			continue
		}
		if fi.ModTime().After(bestT) {
			best, bestT = m, fi.ModTime()
		}
	}
	if best == "" {
		return "", fmt.Errorf("トランスクリプトが見つかりません (探索: %s, project=%q)", projectsDir, project)
	}
	return best, nil
}
```

- [ ] **Step 4: テストが通ることを確認**

Run: `go test ./internal/locate/ -v`
Expected: PASS (7件)

- [ ] **Step 5: コミット**

```bash
git add internal/locate/
git commit -m "feat: 入力解決 (locate) を追加"
```

---

### Task 8: templates パッケージと markdown レンダリング

**Files:**
- Create: `templates/embed.go`
- Create: `templates/default.md.tmpl`
- Create: `internal/render/funcs.go`
- Create: `internal/render/render.go`
- Create: `internal/render/testdata/golden.md`
- Test: `internal/render/funcs_test.go`, `internal/render/markdown_test.go`

**Interfaces:**
- Consumes: `parse.Session` (Task 1)
- Produces:
  - `templates.FS embed.FS` (default.md.tmpl / default.html.tmpl を同梱。html は Task 9)
  - `render.Markdown(w io.Writer, s *parse.Session, overridePath string) error`
  - `render.TruncateLines(n int, s string) string` / `render.FirstLine(s string) string`
  - テストヘルパー `fixtureSession() *parse.Session` (Task 9 も使う)

- [ ] **Step 1: 失敗するテストを書く (テンプレート関数)**

`internal/render/funcs_test.go`:

```go
package render

import "testing"

func TestTruncateLines(t *testing.T) {
	in := "1\n2\n3\n4\n5"
	if got := TruncateLines(3, in); got != "1\n2\n3\n… (残り2行省略)" {
		t.Errorf("TruncateLines = %q", got)
	}
	if got := TruncateLines(10, in); got != in {
		t.Errorf("行数以内で変更された: %q", got)
	}
	if got := TruncateLines(3, ""); got != "" {
		t.Errorf("空文字列: %q", got)
	}
}

func TestFirstLine(t *testing.T) {
	if got := FirstLine("head\nrest"); got != "head" {
		t.Errorf("FirstLine = %q", got)
	}
	if got := FirstLine("single"); got != "single" {
		t.Errorf("FirstLine = %q", got)
	}
}
```

- [ ] **Step 2: テストが失敗することを確認**

Run: `go test ./internal/render/ -v`
Expected: FAIL (TruncateLines 未定義)

- [ ] **Step 3: テンプレート関数を実装**

`internal/render/funcs.go`:

```go
// Package render は Session をテンプレートで markdown / HTML へ整形する。
package render

import (
	"fmt"
	"strings"
	"text/template"
)

// TruncateLines は s を先頭 n 行に切り詰め、省略行数を付記する。
// 引数順はテンプレートのパイプ記法 {{.Result | truncateLines 20}} に合わせて n が先。
func TruncateLines(n int, s string) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[:n], "\n") + fmt.Sprintf("\n… (残り%d行省略)", len(lines)-n)
}

// FirstLine は先頭1行を返す (HTML の <details> サマリー用)。
func FirstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// Funcs はテンプレートへ渡す関数群。README の変数一覧と同期させる。
func Funcs() template.FuncMap {
	return template.FuncMap{
		"truncateLines": TruncateLines,
		"firstLine":     FirstLine,
	}
}
```

Run: `go test ./internal/render/ -v` → PASS (2件)

- [ ] **Step 4: 埋め込みテンプレートと Markdown レンダラーを書く**

`templates/embed.go`:

```go
// Package templates はデフォルトテンプレートをバイナリへ同梱する。
package templates

import "embed"

//go:embed default.md.tmpl
var FS embed.FS
```

(Task 9 で `//go:embed default.md.tmpl default.html.tmpl` に更新する)

`templates/default.md.tmpl`:

````
# セッション {{.ID}}

- プロジェクト: {{.ProjectPath}}
- 期間: {{.StartedAt.Format "2006-01-02 15:04"}} 〜 {{.EndedAt.Format "2006-01-02 15:04"}}
- イベント: user {{.Stats.UserMessages}} / assistant {{.Stats.AssistantMessages}} / ツール {{.Stats.ToolCalls}} / 権限拒否 {{.Stats.PermissionDenies}} / サブエージェント {{.Stats.SubagentCalls}}
{{- if .Stats.SkippedLines}}
- 注意: {{.Stats.SkippedLines}}行をスキップ
{{- end}}
{{range .Events}}
{{- if eq .Kind "user_message"}}
## 👤 User ({{.Timestamp.Format "15:04"}})

{{.Text}}
{{else if eq .Kind "assistant_message"}}
## 🤖 Assistant ({{.Timestamp.Format "15:04"}})

{{.Text}}
{{else if eq .Kind "tool_call"}}
🔧 **{{.Tool.Name}}** — `{{firstLine .Tool.Summary}}`
{{- if .Tool.HasResult}}

```
{{truncateLines 20 .Tool.Result}}
```
{{else}}

(結果なし)
{{end}}
{{- else if eq .Kind "permission_deny"}}
🚫 **拒否** {{.Tool.Name}} — `{{firstLine .Tool.Summary}}`
{{- if .Tool.DenyReason}}

> {{.Tool.DenyReason}}
{{end}}
{{- else if eq .Kind "subagent_call"}}
### 🤝 サブエージェント ({{.Subagent.AgentType}})

**依頼:**

{{.Subagent.Prompt}}

**最終回答:**

{{.Subagent.Answer}}
{{else if eq .Kind "system_note"}}
---
📍 {{.Text}}
---
{{end}}
{{- end}}
````

`internal/render/render.go`:

```go
package render

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"text/template"

	"cctx/internal/parse"
	"cctx/templates"
)

// Markdown は Session を markdown へレンダリングする。overridePath 非空なら外部テンプレートを使う。
func Markdown(w io.Writer, s *parse.Session, overridePath string) error {
	var t *template.Template
	if overridePath != "" {
		b, err := os.ReadFile(overridePath)
		if err != nil {
			return fmt.Errorf("テンプレートを読めません: %w", err)
		}
		t, err = template.New(filepath.Base(overridePath)).Funcs(Funcs()).Parse(string(b))
		if err != nil {
			return fmt.Errorf("テンプレートの構文エラー: %w", err)
		}
	} else {
		src, err := templates.FS.ReadFile("default.md.tmpl")
		if err != nil {
			return err
		}
		t = template.Must(template.New("default.md.tmpl").Funcs(Funcs()).Parse(string(src)))
	}
	if err := t.Execute(w, s); err != nil {
		return fmt.Errorf("markdown のレンダリングに失敗: %w", err)
	}
	return nil
}
```

- [ ] **Step 5: golden テストを書く**

`internal/render/markdown_test.go`:

```go
package render

import (
	"bytes"
	"flag"
	"os"
	"testing"
	"time"

	"cctx/internal/parse"
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
		EndedAt:     at(8),
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
		},
		Stats: parse.Stats{
			UserMessages: 1, AssistantMessages: 1, ToolCalls: 2,
			PermissionDenies: 1, SystemNotes: 1, SubagentCalls: 1, SkippedLines: 1,
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
```

- [ ] **Step 6: golden を生成し、テストが通ることを確認**

```bash
mkdir -p internal/render/testdata
go test ./internal/render/ -update
go test ./internal/render/ -v
```

Expected: PASS (全件)。`internal/render/testdata/golden.md` を**目視確認**する:
ヘッダー統計 / 👤 と 🤖 の発話 / Bash 結果が20行 + `… (残り5行省略)` / 🚫 拒否と引用 / サブエージェントの依頼と最終回答 / 📍 compact / Read の `(結果なし)` がすべて含まれること

- [ ] **Step 7: コミット**

```bash
git add templates/ internal/render/
git commit -m "feat: markdown レンダリングとデフォルトテンプレートを追加"
```

---

### Task 9: HTML レンダリング

**Files:**
- Create: `templates/default.html.tmpl`
- Modify: `templates/embed.go` (embed 対象に html を追加)
- Modify: `internal/render/render.go` (HTML 関数を追加)
- Create: `internal/render/testdata/golden.html`
- Test: `internal/render/html_test.go`

**Interfaces:**
- Consumes: Task 8 の fixtureSession / compareGolden / Funcs
- Produces: `render.HTML(w io.Writer, s *parse.Session, overridePath string) error`

- [ ] **Step 1: 失敗するテストを書く**

`internal/render/html_test.go`:

```go
package render

import (
	"bytes"
	"strings"
	"testing"

	"cctx/internal/parse"
)

func TestHTMLGolden(t *testing.T) {
	var buf bytes.Buffer
	if err := HTML(&buf, fixtureSession(), ""); err != nil {
		t.Fatal(err)
	}
	compareGolden(t, "testdata/golden.html", buf.Bytes())
}

func TestHTMLEscapesUserText(t *testing.T) {
	s := &parse.Session{ID: "x", Events: []parse.Event{
		{Kind: parse.KindUserMessage, Text: "<script>alert(1)</script>"},
	}}
	var buf bytes.Buffer
	if err := HTML(&buf, s, ""); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "<script>alert") {
		t.Error("発話がエスケープされていない")
	}
}

func TestHTMLFullResultInDetails(t *testing.T) {
	// HTML はツール結果を切り詰めず全文収録する (details 折りたたみ)
	var buf bytes.Buffer
	if err := HTML(&buf, fixtureSession(), ""); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "<details>") {
		t.Error("details 折りたたみがない")
	}
	if !strings.Contains(out, "25") || strings.Contains(out, "省略") {
		t.Error("ツール結果が全文収録されていない")
	}
}

func TestHTMLEmptySession(t *testing.T) {
	var buf bytes.Buffer
	if err := HTML(&buf, &parse.Session{ID: "empty"}, ""); err != nil {
		t.Fatalf("空セッションでエラー: %v", err)
	}
}
```

- [ ] **Step 2: テストが失敗することを確認**

Run: `go test ./internal/render/ -run TestHTML -v`
Expected: FAIL (HTML 未定義)

- [ ] **Step 3: HTML テンプレートとレンダラーを実装**

`templates/embed.go` の embed 行を更新:

```go
//go:embed default.md.tmpl default.html.tmpl
var FS embed.FS
```

`templates/default.html.tmpl` (1ファイル完結・CSS 埋め込み・外部依存なし):

```html
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>cctx — {{.ID}}</title>
<style>
  :root { --user: #e8f0fe; --assistant: #f1f3f4; --deny: #fdecea; --note: #fef7e0; }
  body { font-family: -apple-system, "Hiragino Sans", sans-serif; max-width: 60rem;
         margin: 2rem auto; padding: 0 1rem; line-height: 1.7; color: #202124; }
  header { border-bottom: 2px solid #dadce0; margin-bottom: 1.5rem; padding-bottom: 1rem; }
  header dl { display: grid; grid-template-columns: 8rem 1fr; gap: 0.2rem 1rem; margin: 0; }
  header dt { color: #5f6368; }
  .msg { border-radius: 0.75rem; padding: 0.75rem 1rem; margin: 0.75rem 0; white-space: pre-wrap; }
  .user { background: var(--user); }
  .assistant { background: var(--assistant); }
  .tool { font-size: 0.9rem; margin: 0.5rem 0 0.5rem 1rem; }
  .tool summary, .sub summary { cursor: pointer; }
  .tool pre, .sub pre { background: #202124; color: #e8eaed; padding: 0.75rem;
                        border-radius: 0.5rem; overflow-x: auto; white-space: pre-wrap; }
  .deny { background: var(--deny); border-left: 4px solid #d93025; padding: 0.5rem 1rem;
          margin: 0.75rem 0 0.75rem 1rem; }
  .note { background: var(--note); text-align: center; border-radius: 0.5rem;
          padding: 0.3rem; margin: 1rem 0; color: #5f6368; }
  .sub { border: 1px solid #dadce0; border-radius: 0.75rem; padding: 0.75rem 1rem; margin: 0.75rem 0; }
  .sub .label { font-weight: bold; color: #5f6368; margin-top: 0.5rem; }
  .sub .body { white-space: pre-wrap; }
  .ts { color: #80868b; font-size: 0.8rem; }
</style>
<header>
  <h1>セッション {{.ID}}</h1>
  <dl>
    <dt>プロジェクト</dt><dd>{{.ProjectPath}}</dd>
    <dt>期間</dt><dd>{{.StartedAt.Format "2006-01-02 15:04"}} 〜 {{.EndedAt.Format "2006-01-02 15:04"}}</dd>
    <dt>イベント</dt><dd>user {{.Stats.UserMessages}} / assistant {{.Stats.AssistantMessages}} / ツール {{.Stats.ToolCalls}} / 🚫 {{.Stats.PermissionDenies}} / 🤝 {{.Stats.SubagentCalls}}</dd>
  </dl>
</header>
<main>
{{- range .Events}}
{{- if eq .Kind "user_message"}}
<div class="msg user"><span class="ts">👤 {{.Timestamp.Format "15:04"}}</span>
{{.Text}}</div>
{{- else if eq .Kind "assistant_message"}}
<div class="msg assistant"><span class="ts">🤖 {{.Timestamp.Format "15:04"}}</span>
{{.Text}}</div>
{{- else if eq .Kind "tool_call"}}
<details class="tool"><summary>🔧 <b>{{.Tool.Name}}</b> <code>{{firstLine .Tool.Summary}}</code></summary>
<pre>{{.Tool.Input}}</pre>
{{- if .Tool.HasResult}}
<pre>{{.Tool.Result}}</pre>
{{- else}}
<p>(結果なし)</p>
{{- end}}
</details>
{{- else if eq .Kind "permission_deny"}}
<div class="deny">🚫 <b>拒否</b> {{.Tool.Name}} <code>{{firstLine .Tool.Summary}}</code>
{{- if .Tool.DenyReason}}
<blockquote>{{.Tool.DenyReason}}</blockquote>
{{- end}}
</div>
{{- else if eq .Kind "subagent_call"}}
<div class="sub">🤝 <b>サブエージェント</b> ({{.Subagent.AgentType}})
<div class="label">依頼</div>
<details><summary>{{firstLine .Subagent.Prompt}}</summary><div class="body">{{.Subagent.Prompt}}</div></details>
<div class="label">最終回答</div>
<div class="body">{{.Subagent.Answer}}</div>
</div>
{{- else if eq .Kind "system_note"}}
<div class="note">📍 {{.Text}}</div>
{{- end}}
{{- end}}
</main>
```

`internal/render/render.go` に追加 (import に `htmltemplate "html/template"` を追加):

```go
// HTML は Session を1ファイル完結の HTML へレンダリングする (自動エスケープ付き)。
func HTML(w io.Writer, s *parse.Session, overridePath string) error {
	var t *htmltemplate.Template
	if overridePath != "" {
		b, err := os.ReadFile(overridePath)
		if err != nil {
			return fmt.Errorf("テンプレートを読めません: %w", err)
		}
		t, err = htmltemplate.New(filepath.Base(overridePath)).Funcs(Funcs()).Parse(string(b))
		if err != nil {
			return fmt.Errorf("テンプレートの構文エラー: %w", err)
		}
	} else {
		src, err := templates.FS.ReadFile("default.html.tmpl")
		if err != nil {
			return err
		}
		t = htmltemplate.Must(htmltemplate.New("default.html.tmpl").Funcs(Funcs()).Parse(string(src)))
	}
	if err := t.Execute(w, s); err != nil {
		return fmt.Errorf("HTML のレンダリングに失敗: %w", err)
	}
	return nil
}
```

※ `html/template` の Funcs は `text/template` の FuncMap をそのまま受け取れる。

- [ ] **Step 4: golden を生成し、テストが通ることを確認**

```bash
go test ./internal/render/ -update
go test ./internal/render/ -v
```

Expected: PASS (全件)。`golden.html` をブラウザで開いて**目視確認**:
チャット風の色分け / ツールの折りたたみ (開くと Input と全文 Result) / 拒否の赤帯 / サブエージェント枠 / compact の帯

- [ ] **Step 5: コミット**

```bash
git add templates/ internal/render/
git commit -m "feat: HTML レンダリングを追加"
```

---

### Task 10: translate (claude -p 翻訳)

**Files:**
- Create: `internal/translate/translate.go`
- Test: `internal/translate/translate_test.go`

**Interfaces:**
- Consumes: `parse.Session` / `parse.KindSubagentCall`
- Produces:
  - `translate.Translator` インターフェース (`Translate(texts []string) ([]string, error)`)
  - `translate.NewClaude() *Claude` (Command="claude", Model="haiku", Timeout=120s)
  - `translate.Apply(s *parse.Session, tr Translator) error` — SubagentCall の Prompt/Answer を訳文で置換
  - `splitSegments(out string, n int) ([]string, error)` (パッケージ内部、単体テスト対象)

- [ ] **Step 1: 失敗するテストを書く**

`internal/translate/translate_test.go`:

```go
package translate

import (
	"errors"
	"testing"

	"cctx/internal/parse"
)

func TestSplitSegments(t *testing.T) {
	out := "<<<CCTX-SEG 1>>>\n訳文1\n<<<CCTX-SEG 2>>>\n訳文2\n"
	got, err := splitSegments(out, 2)
	if err != nil || len(got) != 2 || got[0] != "訳文1" || got[1] != "訳文2" {
		t.Fatalf("splitSegments = %+v, %v", got, err)
	}
}

func TestSplitSegmentsLeadingPreamble(t *testing.T) {
	// マーカーより前の前置きは捨てる
	out := "はい、翻訳します。\n<<<CCTX-SEG 1>>>\n訳文\n"
	got, err := splitSegments(out, 1)
	if err != nil || got[0] != "訳文" {
		t.Fatalf("splitSegments = %+v, %v", got, err)
	}
}

func TestSplitSegmentsCountMismatch(t *testing.T) {
	if _, err := splitSegments("<<<CCTX-SEG 1>>>\nx\n", 2); err == nil {
		t.Fatal("セグメント数不一致でエラーにならない")
	}
}

// mockTranslator は Apply のテスト用。
type mockTranslator struct {
	got []string
	res []string
	err error
}

func (m *mockTranslator) Translate(texts []string) ([]string, error) {
	m.got = texts
	return m.res, m.err
}

func subSession() *parse.Session {
	return &parse.Session{Events: []parse.Event{
		{Kind: parse.KindUserMessage, Text: "こんにちは"},
		{Kind: parse.KindSubagentCall, Subagent: &parse.Subagent{Prompt: "English prompt", Answer: "English answer"}},
	}}
}

func TestApplyReplacesSubagentTexts(t *testing.T) {
	s := subSession()
	m := &mockTranslator{res: []string{"日本語プロンプト", "日本語回答"}}
	if err := Apply(s, m); err != nil {
		t.Fatal(err)
	}
	if len(m.got) != 2 || m.got[0] != "English prompt" || m.got[1] != "English answer" {
		t.Errorf("翻訳対象: %+v", m.got)
	}
	sub := s.Events[1].Subagent
	if sub.Prompt != "日本語プロンプト" || sub.Answer != "日本語回答" {
		t.Errorf("置換結果: %+v", sub)
	}
	if s.Events[0].Text != "こんにちは" {
		t.Error("発話まで書き換わった")
	}
}

func TestApplyNoSubagents(t *testing.T) {
	s := &parse.Session{Events: []parse.Event{{Kind: parse.KindUserMessage, Text: "x"}}}
	m := &mockTranslator{err: errors.New("呼ばれてはいけない")}
	if err := Apply(s, m); err != nil {
		t.Fatalf("対象ゼロなら Translator を呼ばずに成功すべき: %v", err)
	}
}

func TestApplyPropagatesError(t *testing.T) {
	s := subSession()
	m := &mockTranslator{err: errors.New("boom")}
	if err := Apply(s, m); err == nil {
		t.Fatal("翻訳失敗がエラーにならない (フォールバック禁止)")
	}
}
```

- [ ] **Step 2: テストが失敗することを確認**

Run: `go test ./internal/translate/ -v`
Expected: FAIL (splitSegments / Apply 未定義)

- [ ] **Step 3: translate を実装**

`internal/translate/translate.go`:

```go
// Package translate はサブエージェントのプロンプト/回答を claude -p で日本語訳する。
// 英語かどうかの判定はローカルで行わず LLM に委ねる (設計書: 日英混在テキストへの頑健性)。
package translate

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"cctx/internal/parse"
)

type Translator interface {
	Translate(texts []string) ([]string, error)
}

// Claude は claude CLI (headless) を呼ぶ実装。
type Claude struct {
	Command string
	Model   string
	Timeout time.Duration
}

func NewClaude() *Claude {
	return &Claude{Command: "claude", Model: "haiku", Timeout: 120 * time.Second}
}

var segRE = regexp.MustCompile(`(?m)^<<<CCTX-SEG \d+>>>$`)

// Translate は全テキストを1回の claude -p 呼び出しにまとめて翻訳する (逐次起動の遅延を避ける)。
func (c *Claude) Translate(texts []string) ([]string, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	var sb strings.Builder
	sb.WriteString("以下の各セグメントを日本語に翻訳してください。既に日本語主体のセグメントは一切変更せずそのまま返してください。\n")
	sb.WriteString("セグメントは行頭のマーカー <<<CCTX-SEG n>>> で区切られています。出力にも同じマーカー行をそのまま含め、セグメント数を変えないでください。\n")
	sb.WriteString("マーカー行とセグメント本文以外 (前置き・後置き・説明) は出力しないでください。\n")
	for i, t := range texts {
		fmt.Fprintf(&sb, "\n<<<CCTX-SEG %d>>>\n%s\n", i+1, t)
	}
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.Command, "-p", "--model", c.Model)
	cmd.Stdin = strings.NewReader(sb.String())
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("claude -p の実行に失敗しました: %w", err)
	}
	return splitSegments(string(out), len(texts))
}

// splitSegments は翻訳出力をマーカーで分割する。数が合わなければエラー (部分成功の混在を避ける)。
func splitSegments(out string, n int) ([]string, error) {
	parts := segRE.Split(out, -1)
	if len(parts) > 0 {
		parts = parts[1:] // 先頭マーカーより前の前置きを捨てる
	}
	if len(parts) != n {
		return nil, fmt.Errorf("翻訳結果のセグメント数が一致しません (期待 %d、実際 %d)", n, len(parts))
	}
	res := make([]string, n)
	for i, p := range parts {
		res[i] = strings.TrimSpace(p)
	}
	return res, nil
}

// Apply は Session 中の SubagentCall の Prompt / Answer を訳文で置換する。
func Apply(s *parse.Session, tr Translator) error {
	var texts []string
	var slots []*string
	for i := range s.Events {
		sub := s.Events[i].Subagent
		if s.Events[i].Kind != parse.KindSubagentCall || sub == nil {
			continue
		}
		texts = append(texts, sub.Prompt, sub.Answer)
		slots = append(slots, &sub.Prompt, &sub.Answer)
	}
	if len(texts) == 0 {
		return nil
	}
	translated, err := tr.Translate(texts)
	if err != nil {
		return err
	}
	for i, p := range slots {
		*p = translated[i]
	}
	return nil
}
```

- [ ] **Step 4: テストが通ることを確認**

Run: `go test ./internal/translate/ -v`
Expected: PASS (6件。Claude.Translate の実呼び出しはテスト対象外 — 設計書)

- [ ] **Step 5: コミット**

```bash
git add internal/translate/
git commit -m "feat: claude -p による翻訳 (translate) を追加"
```

---

### Task 11: CLI (cmd/cctx) と E2E スモーク

**Files:**
- Create: `cmd/cctx/main.go`
- Test: `cmd/cctx/main_test.go`

**Interfaces:**
- Consumes: locate.Resolve / parse.ParseFile / render.Markdown / render.HTML / translate.NewClaude / translate.Apply
- Produces: `cctx` バイナリ。フラグ仕様は設計書 CLI 節のとおり

- [ ] **Step 1: 失敗するテストを書く (フラグ検証)**

`cmd/cctx/main_test.go`:

```go
package main

import (
	"strings"
	"testing"
)

func TestValidateFlags(t *testing.T) {
	cases := []struct {
		name    string
		cfg     config
		wantErr string
	}{
		{"stdout は md のみ", config{stdout: true, format: "html"}, "--stdout"},
		{"stdout + both もエラー", config{stdout: true, format: "both"}, "--stdout"},
		{"stdout + md は OK", config{stdout: true, format: "md"}, ""},
		{"stdout + format 省略は md 扱い", config{stdout: true, format: ""}, ""},
		{"-o と --stdout の併用", config{stdout: true, format: "md", outDir: "out"}, "-o"},
		{"--latest と位置引数の併用", config{latest: true, arg: "abc"}, "--latest"},
		{"--project 単独", config{project: "x", arg: "abc"}, "--project"},
		{"不正な format", config{format: "pdf", arg: "abc"}, "format"},
		{"入力なし", config{}, "入力"},
		{"通常ケース", config{arg: "abc", format: "both"}, ""},
	}
	for _, c := range cases {
		err := c.cfg.validate()
		if c.wantErr == "" {
			if err != nil {
				t.Errorf("%s: 予期しないエラー %v", c.name, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), c.wantErr) {
			t.Errorf("%s: err = %v, want contains %q", c.name, err, c.wantErr)
		}
	}
}
```

- [ ] **Step 2: テストが失敗することを確認**

Run: `go test ./cmd/cctx/ -v`
Expected: FAIL (config 未定義)

- [ ] **Step 3: main を実装**

`cmd/cctx/main.go`:

```go
// cctx は Claude Code のセッショントランスクリプトを markdown / HTML へ整形する CLI。
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"cctx/internal/locate"
	"cctx/internal/parse"
	"cctx/internal/render"
	"cctx/internal/translate"
)

type config struct {
	arg         string
	format      string
	outDir      string
	tmplMD      string
	tmplHTML    string
	stdout      bool
	doTranslate bool
	latest      bool
	project     string
	projectsDir string
}

// validate はフラグの組み合わせ規則 (設計書 CLI 節) を検査する。矛盾指定は明示エラー。
func (c *config) validate() error {
	if c.stdout {
		if c.format == "" {
			c.format = "md"
		}
		if c.format != "md" {
			return fmt.Errorf("--stdout は --format md のみ使えます (指定: %s)", c.format)
		}
		if c.outDir != "" {
			return fmt.Errorf("-o と --stdout は同時に指定できません")
		}
	}
	if c.format == "" {
		c.format = "both"
	}
	if c.format != "md" && c.format != "html" && c.format != "both" {
		return fmt.Errorf("--format は md|html|both のいずれかです (指定: %s)", c.format)
	}
	if c.latest && c.arg != "" {
		return fmt.Errorf("--latest と位置引数は同時に指定できません")
	}
	if c.project != "" && !c.latest {
		return fmt.Errorf("--project は --latest と組み合わせたときのみ有効です")
	}
	if !c.latest && c.arg == "" {
		return fmt.Errorf("入力を指定してください (パス / セッションID / --latest)")
	}
	return nil
}

func main() {
	var c config
	flag.StringVar(&c.format, "format", "", "出力形式 md|html|both (デフォルト: both、--stdout 時は md)")
	flag.StringVar(&c.outDir, "o", "", "出力先ディレクトリ (デフォルト: カレント)")
	flag.StringVar(&c.tmplMD, "template-md", "", "markdown 用の自作テンプレート")
	flag.StringVar(&c.tmplHTML, "template-html", "", "HTML 用の自作テンプレート")
	flag.BoolVar(&c.stdout, "stdout", false, "ファイルに書かず標準出力へ (md のみ)")
	flag.BoolVar(&c.doTranslate, "translate", false, "サブエージェントの英語プロンプト/回答を日本語訳")
	flag.BoolVar(&c.latest, "latest", false, "最新セッションを対象にする")
	flag.StringVar(&c.project, "project", "", "--latest の対象をプロジェクト名で絞る")
	flag.Parse()
	c.arg = flag.Arg(0)

	if err := run(&c, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "cctx:", err)
		os.Exit(1)
	}
}

func run(c *config, stdout, stderr io.Writer) error {
	if err := c.validate(); err != nil {
		return err
	}
	if c.projectsDir == "" {
		dir, err := locate.DefaultProjectsDir()
		if err != nil {
			return err
		}
		c.projectsDir = dir
	}

	path, err := locate.Resolve(c.arg, c.latest, c.project, c.projectsDir)
	if err != nil {
		return err
	}
	session, err := parse.ParseFile(path, stderr)
	if err != nil {
		return err
	}
	if session.Stats.SkippedLines > 0 {
		fmt.Fprintf(stderr, "cctx: %d行をスキップしました\n", session.Stats.SkippedLines)
	}
	if c.doTranslate {
		if err := translate.Apply(session, translate.NewClaude()); err != nil {
			return err
		}
	}

	if c.stdout {
		return render.Markdown(stdout, session, c.tmplMD)
	}

	outDir := c.outDir
	if outDir == "" {
		outDir = "."
	}
	writeOut := func(ext string, renderFn func(io.Writer, *parse.Session, string) error, tmpl string) error {
		out := filepath.Join(outDir, session.ID+ext)
		f, err := os.Create(out)
		if err != nil {
			return fmt.Errorf("出力先に書けません: %w", err)
		}
		defer f.Close()
		if err := renderFn(f, session, tmpl); err != nil {
			return err
		}
		fmt.Fprintf(stderr, "cctx: %s を書き出しました\n", out)
		return nil
	}
	if c.format == "md" || c.format == "both" {
		if err := writeOut(".md", render.Markdown, c.tmplMD); err != nil {
			return err
		}
	}
	if c.format == "html" || c.format == "both" {
		if err := writeOut(".html", render.HTML, c.tmplHTML); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 4: ユニットテストが通ることを確認**

Run: `go test ./... -v`
Expected: PASS (全パッケージ)

- [ ] **Step 5: E2E スモークテストを追加**

`cmd/cctx/main_test.go` に追加:

```go
import (
	"bytes"
	"os"
	"path/filepath"
	// 既存 import に追加
)

func TestRunEndToEnd(t *testing.T) {
	outDir := t.TempDir()
	var stdout, stderr bytes.Buffer
	c := &config{
		arg:         "../../internal/parse/testdata/session_small.jsonl",
		format:      "both",
		outDir:      outDir,
		projectsDir: t.TempDir(), // 実環境の ~/.claude を触らない
	}
	if err := run(c, &stdout, &stderr); err != nil {
		t.Fatalf("run: %v (stderr: %s)", err, stderr.String())
	}
	md, err := os.ReadFile(filepath.Join(outDir, "sess-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"こんにちは", "確認します", "🚫", "こっちは触らないで", "サブエージェント", "(結果なし)"} {
		if !bytes.Contains(md, []byte(want)) {
			t.Errorf("md に %q がない", want)
		}
	}
	if _, err := os.Stat(filepath.Join(outDir, "sess-0001.html")); err != nil {
		t.Errorf("html が書き出されていない: %v", err)
	}
}

func TestRunStdout(t *testing.T) {
	var stdout, stderr bytes.Buffer
	c := &config{
		arg:         "../../internal/parse/testdata/session_small.jsonl",
		stdout:      true,
		projectsDir: t.TempDir(),
	}
	if err := run(c, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(stdout.Bytes(), []byte("# セッション sess-0001")) {
		t.Error("--stdout で md が出力されていない")
	}
}
```

Run: `go test ./... -v`
Expected: PASS (全件)

- [ ] **Step 6: 実環境で動作確認**

```bash
go build -o /tmp/claude/cctx ./cmd/cctx
/tmp/claude/cctx --latest --stdout | head -40
```

Expected: 実セッションの markdown がヘッダー付きで表示される (エラーが出たら原因を報告して止まる)

- [ ] **Step 7: コミット**

```bash
git add cmd/
git commit -m "feat: CLI エントリポイントと E2E テストを追加"
```

---

### Task 12: README

**Files:**
- Create: `README.md`

**Interfaces:**
- Consumes: これまでの全タスク (仕様の転記元は設計書)
- Produces: 利用者向けドキュメント

- [ ] **Step 1: README を書く**

`README.md` に以下の節を設ける。内容は設計書と実装から転記し、実際の動作と一致させる:

1. **概要** — cctx が何をするか (2〜3行)
2. **インストール** — `go install ./cmd/cctx` または `go build`
3. **使い方** — 設計書 CLI 節の3形態とフラグ一覧 (実装した `-h` の出力と一致させる)
4. **テンプレート変数一覧** — `Session` / `Event` / `ToolCall` / `Subagent` / `Stats` の全フィールドと、テンプレート関数 `truncateLines` / `firstLine` の説明。`--template-md` / `--template-html` の差し替え例を1つ載せる
5. **既知の制限** — markdown 出力はエスケープしない (発話中の ``` が構造を壊し得る)、thinking は出力しない、翻訳は claude CLI が必要

公開ドキュメントのため個人情報 (実際の発話例・実パス) は載せない。例は捏造データを使う。

- [ ] **Step 2: 記載内容の検証**

```bash
go build -o /tmp/claude/cctx ./cmd/cctx && /tmp/claude/cctx -h
```

README のフラグ説明と `-h` 出力が一致することを目視確認する。

- [ ] **Step 3: コミット**

```bash
git add README.md
git commit -m "docs: README を追加"
```

---

## Self-Review 結果 (計画作成時に実施済み)

- **Spec coverage:** 設計書の全節 (CLI 3形態・フラグ規則 → Task 7/11、データモデル・6 EventKind → Task 1〜6、権限拒否仕様 → Task 4、サブエージェント紐付け → Task 5、レンダリング仕様・テンプレート → Task 8/9、翻訳 → Task 10、エラー処理 → Task 2/7/8/11、テスト方針 → 各タスク、README → Task 12) に対応タスクあり
- **設計書との既知の相違:** なし。templates/ はリポジトリ直下に置き、`templates/embed.go` の go:embed で同梱する (embed はパッケージディレクトリ相対のため、直下に embed.go を置く形で設計書のレイアウトを維持)
- **型整合:** ToolCall.HasResult / Subagent.Prompt / EventKind 文字列値は Task 1 の定義を全タスクで使用。buildEvents / toolEvent のシグネチャは Task 3 → 5 で段階的に拡張される (各タスクに変更後の形を明記済み)
