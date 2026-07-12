package translate

import (
	"errors"
	"testing"

	"cctx/internal/parse"
)

func TestSplitSegments(t *testing.T) {
	out := "<<<CCTX-SEG 1>>>\n訳文1\n<<<CCTX-SEG 2>>>\n訳文2\n"
	got, err := splitSegments(out, 2)
	if err != nil || len(got) != 2 || got[0] != "訳文1" || got[1] != "訳文2" {
		t.Fatalf("splitSegments = %+v, %v", got, err)
	}
}

func TestSplitSegmentsLeadingPreamble(t *testing.T) {
	// マーカーより前の前置きは捨てる
	out := "はい、翻訳します。\n<<<CCTX-SEG 1>>>\n訳文\n"
	got, err := splitSegments(out, 1)
	if err != nil || got[0] != "訳文" {
		t.Fatalf("splitSegments = %+v, %v", got, err)
	}
}

func TestSplitSegmentsCountMismatch(t *testing.T) {
	if _, err := splitSegments("<<<CCTX-SEG 1>>>\nx\n", 2); err == nil {
		t.Fatal("セグメント数不一致でエラーにならない")
	}
}

// mockTranslator は Apply のテスト用。
type mockTranslator struct {
	got []string
	res []string
	err error
}

func (m *mockTranslator) Translate(texts []string) ([]string, error) {
	m.got = texts
	return m.res, m.err
}

func subSession() *parse.Session {
	return &parse.Session{Events: []parse.Event{
		{Kind: parse.KindUserMessage, Text: "こんにちは"},
		{Kind: parse.KindSubagentCall, Subagent: &parse.Subagent{Prompt: "English prompt", Answer: "English answer"}},
	}}
}

func TestApplyReplacesSubagentTexts(t *testing.T) {
	s := subSession()
	m := &mockTranslator{res: []string{"日本語プロンプト", "日本語回答"}}
	if err := Apply(s, m); err != nil {
		t.Fatal(err)
	}
	if len(m.got) != 2 || m.got[0] != "English prompt" || m.got[1] != "English answer" {
		t.Errorf("翻訳対象: %+v", m.got)
	}
	sub := s.Events[1].Subagent
	if sub.Prompt != "日本語プロンプト" || sub.Answer != "日本語回答" {
		t.Errorf("置換結果: %+v", sub)
	}
	if s.Events[0].Text != "こんにちは" {
		t.Error("発話まで書き換わった")
	}
}

func TestApplyNoSubagents(t *testing.T) {
	s := &parse.Session{Events: []parse.Event{{Kind: parse.KindUserMessage, Text: "x"}}}
	m := &mockTranslator{err: errors.New("呼ばれてはいけない")}
	if err := Apply(s, m); err != nil {
		t.Fatalf("対象ゼロなら Translator を呼ばずに成功すべき: %v", err)
	}
}

func TestApplyPropagatesError(t *testing.T) {
	s := subSession()
	m := &mockTranslator{err: errors.New("boom")}
	if err := Apply(s, m); err == nil {
		t.Fatal("翻訳失敗がエラーにならない (フォールバック禁止)")
	}
}
