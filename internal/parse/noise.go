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

// ユーザーのシェル実行 (! prefix)。タグを剥がし、コマンドは "$ " を付けて残す。
var (
	bashInputRE  = regexp.MustCompile(`(?s)<bash-input>(.*?)</bash-input>`)
	bashOutputRE = regexp.MustCompile(`(?s)<bash-(?:stdout|stderr)>(.*?)</bash-(?:stdout|stderr)>`)
)

func stripNoise(s string) string {
	s = noiseRE.ReplaceAllString(s, "")
	s = bashInputRE.ReplaceAllString(s, "$$ ${1}")
	s = bashOutputRE.ReplaceAllString(s, "${1}")
	return strings.TrimSpace(s)
}
