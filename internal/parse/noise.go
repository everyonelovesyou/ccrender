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
	`|<local-command-caveat>.*?</local-command-caveat>` +
	`|<task-notification>.*?</task-notification>`)

func stripNoise(s string) string {
	return strings.TrimSpace(noiseRE.ReplaceAllString(s, ""))
}
