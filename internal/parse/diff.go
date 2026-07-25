package parse

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	// diffMaxLines は Edit の擬似 diff で片側あたり表示する最大行数。超過分は省略行に畳む。
	diffMaxLines = 5
	// writeMaxLines は Write の擬似 diff で表示する最大行数。
	// Write は追加側しか持たないため、Edit の両側合計 (最大10行) と分量を揃える。
	writeMaxLines = 10
)

// editDiff は Edit の input から擬似 unified diff を組み立てる。
// old_string の先頭 diffMaxLines 行を "- "、new_string の先頭 diffMaxLines 行を "+ " で並べ、
// 超過分は「… (あと N 行)」の1行に畳む。old/new とも取れなければ空を返す。
// 真の差分計算はせず、改行コードは LF のみを想定する (CRLF は正規化しない)。
func editDiff(input json.RawMessage) string {
	var p struct {
		OldString  string `json:"old_string"`
		NewString  string `json:"new_string"`
		ReplaceAll bool   `json:"replace_all"`
	}
	if err := json.Unmarshal(input, &p); err != nil {
		return ""
	}
	oldLines := splitDiffLines(p.OldString)
	newLines := splitDiffLines(p.NewString)
	if len(oldLines) == 0 && len(newLines) == 0 {
		return ""
	}
	var out []string
	if p.ReplaceAll {
		out = append(out, "(replace_all)")
	}
	out = append(out, prefixDiffLines("- ", oldLines, diffMaxLines)...)
	out = append(out, prefixDiffLines("+ ", newLines, diffMaxLines)...)
	return strings.Join(out, "\n")
}

// writeDiff は Write の input から擬似 diff を組み立てる。
// Write は書き込む内容しか持たないため、content の先頭 writeMaxLines 行を "+ " で並べ、
// 超過分は「… (あと N 行)」の1行に畳む。content が取れなければ空を返す。
func writeDiff(input json.RawMessage) string {
	var p struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(input, &p); err != nil {
		return ""
	}
	lines := splitDiffLines(p.Content)
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(prefixDiffLines("+ ", lines, writeMaxLines), "\n")
}

// splitDiffLines は末尾の改行を1つだけ落としてから行分割する。落とした結果が空文字列なら0行。
func splitDiffLines(s string) []string {
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// prefixDiffLines は先頭 max 行に prefix を付け、超過分を省略行1行に畳む。
func prefixDiffLines(prefix string, lines []string, max int) []string {
	shown := len(lines)
	if shown > max {
		shown = max
	}
	out := make([]string, 0, shown+1)
	for _, l := range lines[:shown] {
		out = append(out, prefix+l)
	}
	if rest := len(lines) - max; rest > 0 {
		out = append(out, fmt.Sprintf("… (あと %d 行)", rest))
	}
	return out
}
