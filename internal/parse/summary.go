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
	for _, k := range []string{"description", "command", "file_path", "pattern", "path", "query", "skill", "url"} {
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
