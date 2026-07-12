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
