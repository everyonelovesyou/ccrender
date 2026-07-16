package render

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"cctx/internal/parse"
)

// ts は "HH:MM" 形式から 2026-07-12 の time.Time を組み立てる (テスト用)
func ts(t *testing.T, hhmm string) time.Time {
	t.Helper()
	tm, err := time.Parse("2006-01-02 15:04", "2026-07-12 "+hhmm)
	if err != nil {
		t.Fatal(err)
	}
	return tm
}

// extractBlock は out 内で prefix から始まる最初の要素ブロックを、対応する開始タグの
// 深さが 0 に戻るまで (簡易的に次の同種開始タグ or 文字列末尾まで) 抜き出す。
// このテンプレートでは同種ブロックは入れ子にならないため、次の "<div class=\"msg" もしくは
// 次の兄弟要素の開始位置までを1ブロックとして扱えば十分。
func extractBlock(out, prefix string) string {
	start := strings.Index(out, prefix)
	if start < 0 {
		return ""
	}
	rest := out[start+len(prefix):]
	end := strings.Index(rest, "</div>\n</div>")
	if end < 0 {
		return out[start:]
	}
	return out[start : start+len(prefix)+end+len("</div>\n</div>")]
}

func TestHTMLGolden(t *testing.T) {
	var buf bytes.Buffer
	if err := HTML(&buf, fixtureSession(), ""); err != nil {
		t.Fatal(err)
	}
	compareGolden(t, "testdata/golden.html", buf.Bytes())
}

func TestHTMLEscapesUserText(t *testing.T) {
	s := &parse.Session{ID: "x", Events: []parse.Event{
		{Kind: parse.KindUserMessage, Text: "<script>alert(1)</script>"},
	}}
	var buf bytes.Buffer
	if err := HTML(&buf, s, ""); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "<script>alert") {
		t.Error("発話がエスケープされていない")
	}
}

func TestHTMLFullResultInDetails(t *testing.T) {
	// HTML はツール結果を切り詰めず全文収録する (details 折りたたみ)
	var buf bytes.Buffer
	if err := HTML(&buf, fixtureSession(), ""); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "<details>") {
		t.Error("details 折りたたみがない")
	}
	if !strings.Contains(out, "25") || strings.Contains(out, "省略") {
		t.Error("ツール結果が全文収録されていない")
	}
}

func TestHTMLEmptySession(t *testing.T) {
	var buf bytes.Buffer
	if err := HTML(&buf, &parse.Session{ID: "empty"}, ""); err != nil {
		t.Fatalf("空セッションでエラー: %v", err)
	}
}

func TestHTMLSkillChip(t *testing.T) {
	var buf bytes.Buffer
	if err := HTML(&buf, fixtureSession(), ""); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	// パスはリンクではなくコピー用テキストとして表示する
	if strings.Contains(out, `href="file://`) {
		t.Error("スキルパスが file:// リンクになっている (テキスト表示が期待値)")
	}
	if !strings.Contains(out, `class="path copy-src">/Users/example/.claude/skills/ohayou<`) {
		t.Error("ユーザー呼び出しのパス表示がない")
	}
	if !strings.Contains(out, `class="path copy-src">/Users/example/plug/skills/brainstorming<`) {
		t.Error("エージェント発動のパス表示がない")
	}
	// ユーザー呼び出しはコマンド再現がユーザーバブルに入る
	if !strings.Contains(out, "/ohayou 今日も") {
		t.Error("コマンド再現がない")
	}
	if !strings.Contains(out, `class="skill"`) {
		t.Error(".skill チップがない")
	}
}

func TestHTMLTimelineIncludesUserSkill(t *testing.T) {
	var buf bytes.Buffer
	if err := HTML(&buf, fixtureSession(), ""); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	// nav.toc 内にユーザー発動スキルの Command がリンクとして出力される
	tocEnd := strings.Index(out, "</nav>")
	if tocEnd < 0 {
		t.Fatal("nav.toc が見つからない")
	}
	toc := out[:tocEnd]
	if !strings.Contains(toc, "/ohayou 今日も") {
		t.Error("ユーザー発動スキルのコマンドがタイムラインに含まれていない")
	}
	// エージェント発動スキルはタイムラインに含まれない
	if strings.Contains(toc, "brainstorming") {
		t.Error("エージェント発動スキルがタイムラインに含まれている")
	}
}

func TestHTMLEmptyAssistantHeading(t *testing.T) {
	// テキストなしでツールだけ呼んだターンの assistant_message は見出しのみ描画され、コピーボタンと本文 div を持たない
	s := &parse.Session{
		ID: "s1", ProjectPath: "/proj",
		Events: []parse.Event{
			{Kind: parse.KindAssistantMessage, Timestamp: ts(t, "10:00")},
			{Kind: parse.KindToolCall, Timestamp: ts(t, "10:00"), Tool: &parse.ToolCall{Name: "Edit", Summary: "a.go"}},
		},
	}
	var buf bytes.Buffer
	if err := HTML(&buf, s, ""); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "Claude") {
		t.Error("見出しが描画されていない")
	}
	// 空見出しブロックに copy-src が含まれないこと (ブロック単位で検証)
	head := extractBlock(out, `<div class="msg ev-talk"`)
	if strings.Contains(head, "copy-src") {
		t.Error("空見出しにコピー対象が描画されている")
	}
	if strings.Contains(head, `class="copy"`) {
		t.Error("空見出しにコピーボタンが描画されている")
	}
}

func TestHTMLPermissionDenyMultilineSummary(t *testing.T) {
	s := &parse.Session{ID: "x", Events: []parse.Event{
		{Kind: parse.KindPermissionDeny, Timestamp: ts(t, "10:00"), Tool: &parse.ToolCall{
			Name:       "Bash",
			Summary:    "rm -rf /\n--no-preserve-root",
			IsError:    true,
			HasResult:  true,
			DenyReason: "危険なコマンド",
		}},
	}}
	var buf bytes.Buffer
	if err := HTML(&buf, s, ""); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	block := extractBlock(out, `<div class="deny`)
	if block == "" {
		t.Fatal("deny ブロックが見つからない")
	}
	if !strings.Contains(block, `class="copy-src"`) {
		t.Error("Summary に copy-src がない")
	}
	if !strings.Contains(block, `<button class="copy"`) {
		t.Error("コピーボタンがない")
	}
	if !strings.Contains(block, "--no-preserve-root") {
		t.Error("複数行 Summary が全文出力されていない")
	}

	scriptStart := strings.Index(out, "<script>")
	if scriptStart < 0 {
		t.Fatal("script が見つからない")
	}
	script := out[scriptStart:]
	scopeStart := strings.Index(script, `btn.closest(`)
	if scopeStart < 0 {
		t.Fatal("コピー対象の探索処理が見つからない")
	}
	scopeEnd := strings.Index(script[scopeStart:], ");")
	if scopeEnd < 0 {
		t.Fatal("コピー対象の探索式を取得できない")
	}
	scopeExpression := script[scopeStart : scopeStart+scopeEnd]
	if !strings.Contains(scopeExpression, ".deny") {
		t.Error("コピー対象の探索範囲に .deny が含まれていない")
	}
}

func TestHTMLSkillPathShownVerbatim(t *testing.T) {
	// パスは URL エスケープせず、そのままテキストで表示する
	s := &parse.Session{ID: "x", Events: []parse.Event{
		{Kind: parse.KindSkillInvocation, Skill: &parse.SkillInvocation{
			Name: "odd", Path: "/Users/example/my skills/foo#bar",
		}},
	}}
	var buf bytes.Buffer
	if err := HTML(&buf, s, ""); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, `class="path copy-src">/Users/example/my skills/foo#bar<`) {
		t.Errorf("パスがそのまま表示されていない:\n%s", out)
	}
	if strings.Contains(out, "%20") || strings.Contains(out, "%23") {
		t.Error("パスが URL エスケープされている")
	}
}
