package render

import (
	"bytes"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/everyonelovesyou/ccrender/internal/parse"
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

func TestHTMLCrossDayEndedAtShowsDate(t *testing.T) {
	// 日をまたぐセッションでは終了側にも日付を出す
	s := &parse.Session{ID: "x", StartedAt: ts(t, "23:50")}
	s.EndedAt = s.StartedAt.Add(20 * time.Minute) // 翌日 00:10
	var buf bytes.Buffer
	if err := HTML(&buf, s, ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "2026-07-13 00:10") {
		t.Error("日またぎの終了時刻に日付が表示されていない")
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

func TestHTMLPermissionDenyShowsDiff(t *testing.T) {
	// 「何を拒否されたか」が一番見たい情報なので、拒否された Edit でも diff を描画する
	s := &parse.Session{ID: "x", Events: []parse.Event{
		{Kind: parse.KindPermissionDeny, Timestamp: ts(t, "10:00"), Tool: &parse.ToolCall{
			Name:       "Edit",
			Summary:    "/tmp/x.txt",
			Diff:       "- 消される行\n+ 足される行",
			IsError:    true,
			HasResult:  true,
			DenyReason: "こっちは触らないで",
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
	if !strings.Contains(block, `<pre class="diff">`) {
		t.Errorf("拒否ブロックに diff が描画されていない:\n%s", block)
	}
	// html/template は "+" を &#43; へエスケープする (tool_call 側の diff と同じ表現)
	if !strings.Contains(block, "- 消される行") || !strings.Contains(block, "&#43; 足される行") {
		t.Errorf("diff の中身が描画されていない:\n%s", block)
	}

	// 拒否ブロックは <details> ではないため、diff の装飾が details.tool 配下に
	// 限定されていると色が付かないまま出力される
	if !regexp.MustCompile(`(?m)^\s*pre\.diff\b`).MatchString(out) {
		t.Error("diff の装飾が details.tool 配下に限定されており、拒否ブロックに適用されない")
	}
}

func TestHTMLPermissionDenyWithoutDiff(t *testing.T) {
	// Edit 以外の拒否では Diff が空なので、空の pre を出さない
	s := &parse.Session{ID: "x", Events: []parse.Event{
		{Kind: parse.KindPermissionDeny, Timestamp: ts(t, "10:00"), Tool: &parse.ToolCall{
			Name: "Bash", Summary: "rm -rf /", IsError: true, HasResult: true,
		}},
	}}
	var buf bytes.Buffer
	if err := HTML(&buf, s, ""); err != nil {
		t.Fatal(err)
	}
	block := extractBlock(buf.String(), `<div class="deny`)
	if strings.Contains(block, `class="diff"`) {
		t.Errorf("Diff が空なのに diff が描画されている:\n%s", block)
	}
}

func TestHTMLSuccessResultOmittedForFileTools(t *testing.T) {
	for _, name := range []string{"Read", "Edit", "Write"} {
		t.Run(name, func(t *testing.T) {
			s := &parse.Session{ID: "x", Events: []parse.Event{
				{Kind: parse.KindToolCall, Timestamp: ts(t, "10:00"), Tool: &parse.ToolCall{
					Name: name, Summary: "/tmp/a.txt", Input: "{}",
					HasResult: true, Result: "更新に成功しましたという定型文",
				}},
			}}
			var buf bytes.Buffer
			if err := HTML(&buf, s, ""); err != nil {
				t.Fatal(err)
			}
			block := extractBlock(buf.String(), `<div class="tools`)
			if strings.Contains(block, `<pre class="result">`) {
				t.Errorf("%s の成功結果が描画されている:\n%s", name, block)
			}
			if strings.Contains(block, "noresult") {
				t.Errorf("%s に「(結果なし)」が描画されている:\n%s", name, block)
			}
		})
	}
}

func TestHTMLInputOmittedForRead(t *testing.T) {
	// Read の Input は file_path だけで要約と重複する。読み取り範囲は Range が持つので描画しない
	s := &parse.Session{ID: "x", Events: []parse.Event{
		{Kind: parse.KindToolCall, Timestamp: ts(t, "10:00"), Tool: &parse.ToolCall{
			Name: "Read", Summary: "a.go", Range: "L 17〜46",
			Input: "{\n  \"file_path\": \"/proj/a.go\",\n  \"offset\": 17\n}",
		}},
	}}
	var buf bytes.Buffer
	if err := HTML(&buf, s, ""); err != nil {
		t.Fatal(err)
	}
	block := extractBlock(buf.String(), `<div class="tools`)
	if strings.Contains(block, `<pre class="input">`) {
		t.Errorf("Read の Input が描画されている:\n%s", block)
	}
	if !strings.Contains(block, "L 17〜46") {
		t.Errorf("読み取り範囲が描画されていない:\n%s", block)
	}
}

func TestHTMLReadRangeOutsideCopyTarget(t *testing.T) {
	// パスのコピーを濁さないよう、読み取り範囲は copy-src の外側に置く
	s := &parse.Session{ID: "x", Events: []parse.Event{
		{Kind: parse.KindToolCall, Timestamp: ts(t, "10:00"), Tool: &parse.ToolCall{
			Name: "Read", Summary: "/proj/a.go", Range: "L 17〜46",
		}},
	}}
	var buf bytes.Buffer
	if err := HTML(&buf, s, ""); err != nil {
		t.Fatal(err)
	}
	block := extractBlock(buf.String(), `<div class="tools`)
	if !strings.Contains(block, `<code class="copy-src">/proj/a.go</code>`) {
		t.Errorf("コピー対象がパス単体になっていない:\n%s", block)
	}
}

func TestHTMLNoRangeElementWhenAbsent(t *testing.T) {
	// 範囲指定のない Read では空の要素を出さない
	s := &parse.Session{ID: "x", Events: []parse.Event{
		{Kind: parse.KindToolCall, Timestamp: ts(t, "10:00"), Tool: &parse.ToolCall{
			Name: "Read", Summary: "/proj/a.go",
		}},
	}}
	var buf bytes.Buffer
	if err := HTML(&buf, s, ""); err != nil {
		t.Fatal(err)
	}
	block := extractBlock(buf.String(), `<div class="tools`)
	if strings.Contains(block, `class="range"`) {
		t.Errorf("Range が空なのに要素が描画されている:\n%s", block)
	}
}

func TestHTMLInputShownForOtherTools(t *testing.T) {
	// Read 以外は Input に要約へ出ないパラメータが載るため従来どおり描画する
	s := &parse.Session{ID: "x", Events: []parse.Event{
		{Kind: parse.KindToolCall, Timestamp: ts(t, "10:00"), Tool: &parse.ToolCall{
			Name: "Grep", Summary: "foo",
			Input: "{\n  \"pattern\": \"foo\",\n  \"glob\": \"*.go\"\n}",
		}},
	}}
	var buf bytes.Buffer
	if err := HTML(&buf, s, ""); err != nil {
		t.Fatal(err)
	}
	block := extractBlock(buf.String(), `<div class="tools`)
	if !strings.Contains(block, `<pre class="input">`) {
		t.Errorf("Grep の Input が描画されていない:\n%s", block)
	}
}

func TestHTMLEmptyToolNotCollapsible(t *testing.T) {
	// 中身が何もないツール行 (成功した Read) は折りたたまず 1行の div で出す
	s := &parse.Session{ID: "x", Events: []parse.Event{
		{Kind: parse.KindToolCall, Timestamp: ts(t, "10:00"), Tool: &parse.ToolCall{
			Name: "Read", Summary: "a.go", Range: "L 17〜46",
			Input: "{}", HasResult: true, Result: "1\tpackage main",
		}},
	}}
	var buf bytes.Buffer
	if err := HTML(&buf, s, ""); err != nil {
		t.Fatal(err)
	}
	block := extractBlock(buf.String(), `<div class="tools`)
	if strings.Contains(block, "<details") {
		t.Errorf("中身のないツール行が折りたたみになっている:\n%s", block)
	}
	if !strings.Contains(block, `<div class="tool">`) {
		t.Errorf("div のツール行になっていない:\n%s", block)
	}
	// 開けない行に開閉記号を残さない
	if strings.Contains(block, `class="chev"`) {
		t.Errorf("開けない行に開閉記号が描画されている:\n%s", block)
	}
	// 要約・範囲・コピーボタンは折りたたみ時と同じく出す
	if !strings.Contains(block, `<code class="copy-src">a.go</code>`) ||
		!strings.Contains(block, "L 17〜46") ||
		!strings.Contains(block, `<button class="copy"`) {
		t.Errorf("見出し行の中身が欠けている:\n%s", block)
	}
}

func TestHTMLNoResultShownInline(t *testing.T) {
	// 「(結果なし)」だけの行も折りたたまず、注記を見出し行の脇に出す
	s := &parse.Session{ID: "x", Events: []parse.Event{
		{Kind: parse.KindToolCall, Timestamp: ts(t, "10:00"), Tool: &parse.ToolCall{
			Name: "Read", Summary: "a.go", HasResult: false,
		}},
	}}
	var buf bytes.Buffer
	if err := HTML(&buf, s, ""); err != nil {
		t.Fatal(err)
	}
	block := extractBlock(buf.String(), `<div class="tools`)
	if strings.Contains(block, "<details") {
		t.Errorf("「(結果なし)」だけの行が折りたたみになっている:\n%s", block)
	}
	if !strings.Contains(block, `<span class="noresult">(結果なし)</span>`) {
		t.Errorf("注記が見出し行の脇に出ていない:\n%s", block)
	}
}

func TestHTMLToolWithBodyStaysCollapsible(t *testing.T) {
	// 中身を持つ行は従来どおり折りたたむ
	cases := map[string]*parse.ToolCall{
		"失敗した Read": {Name: "Read", Summary: "a.go", HasResult: true, IsError: true, Result: "File does not exist."},
		"入力を持つ Bash": {Name: "Bash", Summary: "ls", Input: "{\n  \"command\": \"ls\"\n}", HasResult: true, Result: "a.go"},
		"diff を持つ Edit": {Name: "Edit", Summary: "a.go", Input: "{}", Diff: "- x\n+ y", HasResult: true, Result: "ok"},
		"結果を持たない Bash": {Name: "Bash", Summary: "ls", Input: "{\n  \"command\": \"ls\"\n}", HasResult: false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			s := &parse.Session{ID: "x", Events: []parse.Event{
				{Kind: parse.KindToolCall, Timestamp: ts(t, "10:00"), Tool: tc},
			}}
			var buf bytes.Buffer
			if err := HTML(&buf, s, ""); err != nil {
				t.Fatal(err)
			}
			block := extractBlock(buf.String(), `<div class="tools`)
			if !strings.Contains(block, "<details") {
				t.Errorf("中身があるのに折りたたみになっていない:\n%s", block)
			}
		})
	}
}

func TestHTMLCopyScopeCoversToolHead(t *testing.T) {
	// 折りたたまない行では summary が無くなるため、コピー対象の探索範囲に見出し行が要る
	var buf bytes.Buffer
	if err := HTML(&buf, fixtureSession(), ""); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	scopeStart := strings.Index(out, `btn.closest(`)
	if scopeStart < 0 {
		t.Fatal("コピー対象の探索処理が見つからない")
	}
	scopeEnd := strings.Index(out[scopeStart:], ");")
	if scopeEnd < 0 {
		t.Fatal("コピー対象の探索式を取得できない")
	}
	if !strings.Contains(out[scopeStart:scopeStart+scopeEnd], ".toolhead") {
		t.Error("コピー対象の探索範囲にツール見出し行が含まれていない")
	}
}

func TestHTMLErrorResultShownForFileTools(t *testing.T) {
	s := &parse.Session{ID: "x", Events: []parse.Event{
		{Kind: parse.KindToolCall, Timestamp: ts(t, "10:00"), Tool: &parse.ToolCall{
			Name: "Edit", Summary: "/tmp/a.txt", Input: "{}",
			HasResult: true, IsError: true, Result: "String to replace not found",
		}},
	}}
	var buf bytes.Buffer
	if err := HTML(&buf, s, ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "String to replace not found") {
		t.Errorf("Edit の失敗結果が描画されていない:\n%s", buf.String())
	}
}

func TestHTMLMissingResultShowsNote(t *testing.T) {
	for _, name := range []string{"Bash", "Read", "Edit"} {
		t.Run(name, func(t *testing.T) {
			s := &parse.Session{ID: "x", Events: []parse.Event{
				{Kind: parse.KindToolCall, Timestamp: ts(t, "10:00"), Tool: &parse.ToolCall{
					Name: name, Summary: "x", Input: "{}", HasResult: false,
				}},
			}}
			var buf bytes.Buffer
			if err := HTML(&buf, s, ""); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(buf.String(), `class="noresult"`) {
				t.Errorf("%s: 結果が欠けているのに注記がない:\n%s", name, buf.String())
			}
		})
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
