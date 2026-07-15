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

// Join は文字列スライスを sep で連結する。引数順はパイプ記法に合わせて sep が先。
func Join(sep string, ss []string) string {
	return strings.Join(ss, sep)
}

// IsMultiline は s が複数行かを返す (複数行 Summary の全文表示分岐用)。
func IsMultiline(s string) bool {
	return strings.ContainsRune(s, '\n')
}

// Funcs はテンプレートへ渡す関数群。README の変数一覧と同期させる。
func Funcs() template.FuncMap {
	return template.FuncMap{
		"truncateLines": TruncateLines,
		"firstLine":     FirstLine,
		"join":          Join,
		"isMultiline":   IsMultiline,
	}
}
