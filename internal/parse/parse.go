package parse

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"
)

// ParseFile はトランスクリプト JSONL を読み、正規化 Session を返す。
// warn には縮退警告を書く (nil なら破棄)。全行パース失敗はエラー。
func ParseFile(path string, warn io.Writer) (*Session, error) {
	if warn == nil {
		warn = io.Discard
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("トランスクリプトを開けません: %w", err)
	}
	defer f.Close()

	records, skipped, err := readRecords(f, false)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("有効な行がありません (%s、スキップ %d行)", path, skipped)
	}

	results := collectToolResults(records)
	idx := collectSkillIndex(records)
	subs := loadSubagents(strings.TrimSuffix(path, ".jsonl") + "/subagents")
	s := &Session{}
	for _, rec := range records {
		if s.ID == "" {
			s.ID = rec.SessionID
		}
		if s.ProjectPath == "" {
			s.ProjectPath = rec.CWD
		}
		ts := parseTime(rec.Timestamp)
		if !ts.IsZero() {
			if s.StartedAt.IsZero() {
				s.StartedAt = ts
			}
			s.EndedAt = ts
		}
		if rec.Type == "assistant" && rec.Message != nil && rec.Message.Model != "" {
			if !slices.Contains(s.Models, rec.Message.Model) {
				s.Models = append(s.Models, rec.Message.Model)
			}
		}
		s.Events = append(s.Events, buildEvents(rec, ts, s.ProjectPath, results, subs, idx, warn)...)
	}
	s.Events = insertAssistantHeadings(s.Events)
	s.Stats = computeStats(s.Events, skipped)
	return s, nil
}

// readRecords は全行をデコードする。壊れた行はスキップして数える。
// keepSidechain が false のとき isSidechain=true の行もスキップする (本流の読み込み用)。
// keepSidechain が true のときは全レコードを保持する (サブエージェント JSONL 用)。
func readRecords(f io.Reader, keepSidechain bool) ([]rawRecord, int, error) {
	r := bufio.NewReaderSize(f, 1<<20)
	var records []rawRecord
	skipped := 0
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 1 {
			var rec rawRecord
			if json.Unmarshal(line, &rec) != nil {
				skipped++
			} else if keepSidechain || !rec.IsSidechain {
				records = append(records, rec)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, 0, fmt.Errorf("読み込みエラー: %w", err)
		}
	}
	return records, skipped, nil
}

func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	// 記録は UTC。表示はローカル時刻で行う
	return t.Local()
}

// collectToolResults は user レコード中の tool_result を tool_use_id で索引化する。
func collectToolResults(records []rawRecord) map[string]contentBlock {
	m := map[string]contentBlock{}
	for _, rec := range records {
		if rec.Type != "user" {
			continue
		}
		for _, b := range rec.Message.blocks() {
			if b.Type == "tool_result" && b.ToolUseID != "" {
				m[b.ToolUseID] = b
			}
		}
	}
	return m
}

// buildEvents は1レコードをイベント列へ変換する。
func buildEvents(rec rawRecord, ts time.Time, projectRoot string, results map[string]contentBlock, subs map[string]Subagent, idx skillIndex, warn io.Writer) []Event {
	var events []Event
	switch rec.Type {
	case "user":
		if p, ok := skillExpansionPath(rec); ok {
			if rec.SourceToolUseID != "" {
				// エージェント発動: Skill tool_use 側で描画する (対応が無ければ非表示)
				return nil
			}
			return []Event{userSkillEvent(rec, p, ts, idx)}
		}
		for _, b := range rec.Message.blocks() {
			if b.Type != "text" {
				continue // tool_result は tool_use 側で統合済み
			}
			if text := stripNoise(b.Text); text != "" {
				events = append(events, Event{Kind: KindUserMessage, Timestamp: ts, Text: text})
			}
		}
	case "assistant":
		for _, b := range rec.Message.blocks() {
			switch b.Type {
			case "text":
				if text := stripNoise(b.Text); text != "" {
					events = append(events, Event{Kind: KindAssistantMessage, Timestamp: ts, Text: text})
				}
			case "tool_use":
				events = append(events, toolEvent(b, ts, projectRoot, results, subs, idx, warn))
			}
		}
	case "system":
		// 節目情報は最小限 (設計書)。現状は compact 境界のみ
		if rec.Subtype == "compact_boundary" {
			events = append(events, Event{Kind: KindSystemNote, Timestamp: ts, Text: "コンテキスト圧縮 (compact)"})
		}
	}
	return events
}

// toolEvent は tool_use ブロックを ToolCall (または Agent の場合 SubagentCall) イベントへ変換する。
func toolEvent(b contentBlock, ts time.Time, projectRoot string, results map[string]contentBlock, subs map[string]Subagent, idx skillIndex, warn io.Writer) Event {
	tc := &ToolCall{
		Name:    b.Name,
		Summary: toolSummary(b.Name, b.Input, projectRoot),
		Input:   formatInput(b.Input),
	}
	switch b.Name {
	case "Edit":
		tc.Diff = editDiff(b.Input)
	case "Write":
		tc.Diff = writeDiff(b.Input)
	}
	kind := KindToolCall
	if res, ok := results[b.ID]; ok {
		tc.HasResult = true
		tc.Result = res.resultText()
		tc.IsError = res.IsError
		if res.IsError {
			if s, isStr := res.resultString(); isStr {
				if reason, deny := denyReason(s); deny {
					kind = KindPermissionDeny
					tc.DenyReason = reason
				}
			}
		}
	}
	if b.Name == "Read" && tc.HasResult && !tc.IsError {
		// Read の成功結果はファイル内容の再掲にすぎないため表示しない (TODO.md)
		tc.HasResult = false
		tc.Result = ""
	}
	if kind == KindPermissionDeny {
		// 拒否された Agent tool_use は SubagentCall ではなく PermissionDeny として扱う
		// (サブエージェントは実際には起動されていないため)
		return Event{Kind: kind, Timestamp: ts, Tool: tc}
	}
	if b.Name == "Skill" {
		if p, ok := idx.expansions[b.ID]; ok {
			return Event{Kind: KindSkillInvocation, Timestamp: ts, Skill: agentSkillInvocation(b, p)}
		}
	}
	if b.Name == "Agent" {
		return subagentEvent(b, ts, results, subs, warn)
	}
	return Event{Kind: kind, Timestamp: ts, Tool: tc}
}

// subagentEvent は Agent tool_use を SubagentCall イベントへ変換する。
func subagentEvent(b contentBlock, ts time.Time, results map[string]contentBlock, subs map[string]Subagent, warn io.Writer) Event {
	var input struct {
		Prompt       string `json:"prompt"`
		SubagentType string `json:"subagent_type"`
	}
	_ = json.Unmarshal(b.Input, &input)
	sub, found := subs[b.ID]
	sub.Prompt = input.Prompt
	if sub.AgentType == "" {
		sub.AgentType = input.SubagentType
	}
	if !found {
		// 明示的な縮退: 本流 tool_result の text で代替し、警告を出す (無言のフォールバックはしない)
		fmt.Fprintf(warn, "ccrender: 警告: サブエージェント記録が見つかりません (tool_use %s)。本流の tool_result で代替します\n", b.ID)
		if res, ok := results[b.ID]; ok {
			sub.Answer = res.resultText()
		}
	}
	return Event{Kind: KindSubagentCall, Timestamp: ts, Subagent: &sub}
}

// insertAssistantHeadings は、直前に assistant の発話が無いまま assistant 側イベント
// (ツール呼び出し等) が始まる箇所へ、空テキストの assistant_message (見出しのみ) を挿入する。
// テキストなしでツールだけ呼んだターンの帰属が user に見える問題への対処。
func insertAssistantHeadings(events []Event) []Event {
	out := make([]Event, 0, len(events))
	inAssistant := false
	for _, e := range events {
		switch e.Kind {
		case KindAssistantMessage:
			inAssistant = true
		case KindUserMessage, KindSystemNote:
			inAssistant = false
		case KindSkillInvocation:
			if e.Skill != nil && e.Skill.ByUser {
				inAssistant = false
			} else if !inAssistant {
				out = append(out, Event{Kind: KindAssistantMessage, Timestamp: e.Timestamp})
				inAssistant = true
			}
		case KindToolCall, KindPermissionDeny, KindSubagentCall:
			if !inAssistant {
				out = append(out, Event{Kind: KindAssistantMessage, Timestamp: e.Timestamp})
				inAssistant = true
			}
		}
		out = append(out, e)
	}
	return out
}

func computeStats(events []Event, skipped int) Stats {
	st := Stats{SkippedLines: skipped}
	for _, e := range events {
		switch e.Kind {
		case KindUserMessage:
			st.UserMessages++
		case KindAssistantMessage:
			if e.Text != "" {
				st.AssistantMessages++
			}
		case KindToolCall:
			st.ToolCalls++
		case KindPermissionDeny:
			st.PermissionDenies++
		case KindSystemNote:
			st.SystemNotes++
		case KindSubagentCall:
			st.SubagentCalls++
		case KindSkillInvocation:
			st.SkillInvocations++
		}
	}
	return st
}
