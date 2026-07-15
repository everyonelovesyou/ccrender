package parse

import (
	"encoding/json"
	"strings"
)

// toolSummary はツール呼び出しの1行要約を返す。ツール固有の主要パラメータを優先し、
// 未知のツールは汎用キーの探索にフォールバックする (どれも無ければ空)。
// パス系の値は projectRoot 配下なら相対パスへ短縮する (コマンド文字列は再現性優先で不変)。
func toolSummary(name string, input json.RawMessage, projectRoot string) string {
	var m map[string]any
	_ = json.Unmarshal(input, &m)
	str := func(k string) string {
		s, _ := m[k].(string)
		return s
	}
	path := func(k string) string {
		return relToRoot(projectRoot, str(k))
	}
	switch name {
	case "Bash":
		if s := str("command"); s != "" {
			return s
		}
	case "Read", "Edit", "Write", "NotebookEdit":
		if s := path("file_path"); s != "" {
			return s
		}
	case "Agent":
		if s := str("description"); s != "" {
			return s
		}
	}
	for _, k := range []string{"description", "command", "file_path", "pattern", "path", "query", "skill", "url"} {
		s := str(k)
		if s == "" {
			continue
		}
		if k == "file_path" || k == "path" {
			s = relToRoot(projectRoot, s)
		}
		return s
	}
	return ""
}

// relToRoot は p が root 配下の絶対パスなら root からの相対パスへ短縮する。
// root 自身・root 外・相対パスはそのまま返す。
func relToRoot(root, p string) string {
	if root == "" || p == "" {
		return p
	}
	if rest, ok := strings.CutPrefix(p, root+"/"); ok && rest != "" {
		return rest
	}
	return p
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
