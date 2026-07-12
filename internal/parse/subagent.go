package parse

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type subagentMeta struct {
	AgentType string `json:"agentType"`
	ToolUseID string `json:"toolUseId"`
}

// loadSubagents は subagents/ 配下の *.meta.json を走査し、toolUseId で索引化して返す。
// meta.json の toolUseId が本流の Agent tool_use の id と1対1対応する (設計書・実地調査)。
// Answer は agent-<id>.jsonl の最後の assistant レコードの text ブロック連結。
func loadSubagents(dir string) map[string]Subagent {
	m := map[string]Subagent{}
	metas, err := filepath.Glob(filepath.Join(dir, "*.meta.json"))
	if err != nil {
		return m
	}
	for _, mp := range metas {
		b, err := os.ReadFile(mp)
		if err != nil {
			continue
		}
		var meta subagentMeta
		if json.Unmarshal(b, &meta) != nil || meta.ToolUseID == "" {
			continue
		}
		jsonlPath := strings.TrimSuffix(mp, ".meta.json") + ".jsonl"
		answer, err := subagentAnswer(jsonlPath)
		if err != nil {
			continue // 回答が読めない記録は未解決扱い (呼び出し側で縮退警告)
		}
		id := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(mp), "agent-"), ".meta.json")
		m[meta.ToolUseID] = Subagent{AgentID: id, AgentType: meta.AgentType, Answer: answer}
	}
	return m
}

// subagentAnswer はサブエージェント JSONL の最後の assistant レコードのテキストを返す。
// 本流側の tool_result はハーネスが agentId 行と <usage> タグを付加するため使わない (設計書)。
func subagentAnswer(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	records, _, err := readRecords(f, true) // サブエージェント JSONL は全レコードが isSidechain=true
	if err != nil {
		return "", err
	}
	answer := ""
	for _, rec := range records {
		if rec.Type != "assistant" {
			continue
		}
		var parts []string
		for _, b := range rec.Message.blocks() {
			if b.Type == "text" && b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
		if len(parts) > 0 {
			answer = strings.Join(parts, "\n\n")
		}
	}
	if answer == "" {
		// 空の回答を正常な記録として登録すると縮退経路が働かないため、明示エラーにする
		return "", fmt.Errorf("assistant のテキストがありません: %s", path)
	}
	return answer, nil
}
