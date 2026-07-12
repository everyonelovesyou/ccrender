package render

import (
	"bytes"
	"strings"
	"testing"

	"cctx/internal/parse"
)

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
	// file:// リンクが期待どおり生成される (#ZgotmplZ でない)
	if strings.Contains(out, "ZgotmplZ") {
		t.Error("href が #ZgotmplZ に置換されている")
	}
	if !strings.Contains(out, `href="file:///Users/example/.claude/skills/ohayou"`) {
		t.Error("ユーザー呼び出しの file:// リンクがない")
	}
	if !strings.Contains(out, `href="file:///Users/example/plug/skills/brainstorming"`) {
		t.Error("エージェント発動の file:// リンクがない")
	}
	// ユーザー呼び出しはコマンド再現がユーザーバブルに入る
	if !strings.Contains(out, "/ohayou 今日も") {
		t.Error("コマンド再現がない")
	}
	if !strings.Contains(out, `class="skill"`) {
		t.Error(".skill チップがない")
	}
}

func TestHTMLSkillPathEscaping(t *testing.T) {
	s := &parse.Session{ID: "x", Events: []parse.Event{
		{Kind: parse.KindSkillInvocation, Skill: &parse.SkillInvocation{
			Name: "odd", Path: "/Users/example/my skills/foo#bar",
		}},
	}}
	var buf bytes.Buffer
	if err := HTML(&buf, s, ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `href="file:///Users/example/my%20skills/foo%23bar"`) {
		t.Errorf("空白・# が %%エスケープされていない:\n%s", buf.String())
	}
}

func TestHTMLSkillRelativePathDegradesToText(t *testing.T) {
	s := &parse.Session{ID: "x", Events: []parse.Event{
		{Kind: parse.KindSkillInvocation, Skill: &parse.SkillInvocation{
			Name: "rel", Path: "skills/rel",
		}},
	}}
	var buf bytes.Buffer
	if err := HTML(&buf, s, ""); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Contains(out, `href="skills/rel"`) || strings.Contains(out, `href=""`) {
		t.Error("相対パスがリンクになっている (または空 href が出ている)")
	}
	if !strings.Contains(out, "<code>skills/rel</code>") {
		t.Error("相対パスがテキスト表示へ縮退していない")
	}
}
