// Package render は Session をテンプレートで markdown / HTML へ整形する。
package render

import (
	"fmt"
	htmltemplate "html/template"
	"net/url"
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

// Funcs はテンプレートへ渡す関数群。README の変数一覧と同期させる。
func Funcs() template.FuncMap {
	return template.FuncMap{
		"truncateLines": TruncateLines,
		"firstLine":     FirstLine,
		"fileURL":       FileURL,
	}
}
