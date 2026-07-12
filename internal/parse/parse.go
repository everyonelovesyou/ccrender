package parse

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
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

	records, skipped, err := readRecords(f)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("有効な行がありません (%s、スキップ %d行)", path, skipped)
	}

	results := collectToolResults(records)
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
		s.Events = append(s.Events, buildEvents(rec, ts, results)...)
	}
	s.Stats = computeStats(s.Events, skipped)
	return s, nil
}

// readRecords は全行をデコードする。壊れた行と isSidechain=true はスキップして数える。
func readRecords(f io.Reader) ([]rawRecord, int, error) {
	r := bufio.NewReaderSize(f, 1<<20)
	var records []rawRecord
	skipped := 0
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 1 {
			var rec rawRecord
			if json.Unmarshal(line, &rec) != nil {
				skipped++
			} else if !rec.IsSidechain {
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
	return t
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
func buildEvents(rec rawRecord, ts time.Time, results map[string]contentBlock) []Event {
	var events []Event
	switch rec.Type {
	case "user":
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
				events = append(events, toolEvent(b, ts, results))
			}
		}
	}
	return events
}

// toolEvent は tool_use ブロックを ToolCall イベントへ変換する。
func toolEvent(b contentBlock, ts time.Time, results map[string]contentBlock) Event {
	tc := &ToolCall{
		Name:    b.Name,
		Summary: toolSummary(b.Name, b.Input),
		Input:   formatInput(b.Input),
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
	return Event{Kind: kind, Timestamp: ts, Tool: tc}
}

func computeStats(events []Event, skipped int) Stats {
	st := Stats{SkippedLines: skipped}
	for _, e := range events {
		switch e.Kind {
		case KindUserMessage:
			st.UserMessages++
		case KindAssistantMessage:
			st.AssistantMessages++
		case KindToolCall:
			st.ToolCalls++
		case KindPermissionDeny:
			st.PermissionDenies++
		case KindSystemNote:
			st.SystemNotes++
		case KindSubagentCall:
			st.SubagentCalls++
		}
	}
	return st
}
