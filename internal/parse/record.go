package parse

import (
	"encoding/json"
	"strings"
)

// rawRecord はトランスクリプト JSONL の1行。出力に使わないフィールドは持たない。
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
