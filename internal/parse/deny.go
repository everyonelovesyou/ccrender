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
