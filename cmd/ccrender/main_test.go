package main

import (
	"bytes"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSubcommand(t *testing.T) {
	cases := []struct {
		name       string
		sub        string
		args       []string
		wantFormat string
		wantStdout bool
		wantErr    string
	}{
		{"md は format=md", "md", []string{"abc"}, "md", false, ""},
		{"html は format=html", "html", []string{"abc"}, "html", false, ""},
		{"both は format=both", "both", []string{"abc"}, "both", false, ""},
		{"stdout は format=md + stdout", "stdout", []string{"abc"}, "md", true, ""},
		{"md の固有フラグ", "md", []string{"-o", "out", "--template-md", "t.tmpl", "abc"}, "md", false, ""},
		{"html の固有フラグ", "html", []string{"--template-html", "t.tmpl", "abc"}, "html", false, ""},
		{"共通フラグは全サブコマンドで使える", "stdout", []string{"--translate", "--latest", "--project", "x"}, "md", true, ""},
		{"属さないフラグ: stdout に -o", "stdout", []string{"-o", "out", "abc"}, "", false, "-o"},
		{"属さないフラグ: md に --template-html", "md", []string{"--template-html", "t", "abc"}, "", false, "template-html"},
		{"属さないフラグ: html に --template-md", "html", []string{"--template-md", "t", "abc"}, "", false, "template-md"},
		{"未知のサブコマンド", "foo", nil, "", false, "未知のサブコマンド"},
	}
	for _, tc := range cases {
		c, err := parseSubcommand(tc.sub, tc.args)
		if tc.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("%s: err = %v, want contains %q", tc.name, err, tc.wantErr)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: 予期しないエラー %v", tc.name, err)
			continue
		}
		if c.format != tc.wantFormat || c.stdout != tc.wantStdout {
			t.Errorf("%s: format=%q stdout=%v, want %q %v", tc.name, c.format, c.stdout, tc.wantFormat, tc.wantStdout)
		}
	}
}

func TestParseSubcommandPositionalArgs(t *testing.T) {
	c, err := parseSubcommand("md", []string{"-o", "out", "abc", "def"})
	if err != nil {
		t.Fatal(err)
	}
	if c.arg != "abc" || c.narg != 2 {
		t.Errorf("arg=%q narg=%d, want %q 2", c.arg, c.narg, "abc")
	}
	if c.outDir != "out" {
		t.Errorf("outDir=%q, want %q", c.outDir, "out")
	}
}

func TestParseSubcommandHelp(t *testing.T) {
	if _, err := parseSubcommand("md", []string{"-h"}); !errors.Is(err, flag.ErrHelp) {
		t.Errorf("err = %v, want flag.ErrHelp", err)
	}
}

func TestValidateFlags(t *testing.T) {
	cases := []struct {
		name    string
		cfg     config
		wantErr string
	}{
		{"位置引数が2つ以上", config{arg: "abc", narg: 2}, "位置引数"},
		{"--latest と位置引数の併用", config{latest: true, arg: "abc", narg: 1}, "--latest"},
		{"--project 単独", config{project: "x", arg: "abc", narg: 1}, "--project"},
		{"入力なし", config{}, "入力"},
		{"stdout でも入力は必須", config{stdout: true, format: "md"}, "入力"},
		{"位置引数1つは OK", config{arg: "abc", narg: 1, format: "both"}, ""},
		{"--latest 単独は OK", config{latest: true, format: "md"}, ""},
		{"--latest --project は OK", config{latest: true, project: "x", format: "html"}, ""},
	}
	for _, c := range cases {
		err := c.cfg.validate()
		if c.wantErr == "" {
			if err != nil {
				t.Errorf("%s: 予期しないエラー %v", c.name, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), c.wantErr) {
			t.Errorf("%s: err = %v, want contains %q", c.name, err, c.wantErr)
		}
	}
}

func TestRealMainDispatch(t *testing.T) {
	cases := []struct {
		name       string
		args       []string
		wantExit   int
		wantStdout string // 空なら stdout は検査しない
		wantStderr string // 空なら stderr は検査しない
	}{
		{"引数なしは使い方を stderr へ", nil, 1, "", "使い方"},
		{"-h は使い方を stdout へ", []string{"-h"}, 0, "使い方", ""},
		{"help は使い方を stdout へ", []string{"help"}, 0, "使い方", ""},
		{"使い方に用例を含む", []string{"help"}, 0, "ccrender both --latest", ""},
		{"help md はフラグ一覧を stdout へ", []string{"help", "md"}, 0, "-template-md", ""},
		{"help stdout もフラグ一覧を stdout へ", []string{"help", "stdout"}, 0, "-template-md", ""},
		{"md -h はフラグ一覧を stdout へ", []string{"md", "-h"}, 0, "-template-md", ""},
		{"未知のサブコマンド", []string{"foo"}, 1, "", "未知のサブコマンドです"},
		{"未知のサブコマンドにも使い方", []string{"foo"}, 1, "", "使い方"},
		{"help の後の未知サブコマンド", []string{"help", "foo"}, 1, "", "未知のサブコマンドです"},
		{"旧形式 (ID 直接) は未知サブコマンド扱い", []string{"abc123"}, 1, "", "未知のサブコマンドです"},
		{"属さないフラグは stderr + exit 1", []string{"stdout", "-o", "x", "abc"}, 1, "", "-o"},
		{"残る検査: --latest と位置引数", []string{"md", "--latest", "abc"}, 1, "", "--latest"},
		{"残る検査: --project 単独", []string{"md", "--project", "x", "abc"}, 1, "", "--project"},
		{"残る検査: 入力なし", []string{"both"}, 1, "", "入力"},
		{"残る検査: 位置引数2つ", []string{"md", "abc", "def"}, 1, "", "位置引数"},
	}
	for _, tc := range cases {
		var stdout, stderr bytes.Buffer
		got := realMain(tc.args, &stdout, &stderr)
		if got != tc.wantExit {
			t.Errorf("%s: exit = %d, want %d (stderr: %s)", tc.name, got, tc.wantExit, stderr.String())
		}
		if tc.wantStdout != "" && !strings.Contains(stdout.String(), tc.wantStdout) {
			t.Errorf("%s: stdout に %q がない: %s", tc.name, tc.wantStdout, stdout.String())
		}
		if tc.wantStderr != "" && !strings.Contains(stderr.String(), tc.wantStderr) {
			t.Errorf("%s: stderr に %q がない: %s", tc.name, tc.wantStderr, stderr.String())
		}
		if tc.wantExit == 0 && stderr.Len() > 0 {
			t.Errorf("%s: 正常系なのに stderr に出力がある: %s", tc.name, stderr.String())
		}
	}
}

func TestRealMainStdoutEndToEnd(t *testing.T) {
	var stdout, stderr bytes.Buffer
	got := realMain([]string{"stdout", "../../internal/parse/testdata/session_small.jsonl"}, &stdout, &stderr)
	if got != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", got, stderr.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte("# セッション sess-0001")) {
		t.Error("stdout サブコマンドで md が出力されていない")
	}
}

func TestRunEndToEnd(t *testing.T) {
	outDir := t.TempDir()
	var stdout, stderr bytes.Buffer
	c := &config{
		arg:         "../../internal/parse/testdata/session_small.jsonl",
		format:      "both",
		outDir:      outDir,
		projectsDir: t.TempDir(), // 実環境の ~/.claude を触らない
	}
	if err := run(c, &stdout, &stderr); err != nil {
		t.Fatalf("run: %v (stderr: %s)", err, stderr.String())
	}
	md, err := os.ReadFile(filepath.Join(outDir, "sess-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"こんにちは", "確認します", "🚫", "こっちは触らないで", "サブエージェント",
		"/ohayou 今日も", "Skill(ohayou)", "Skill(superpowers:brainstorming)",
	} {
		if !bytes.Contains(md, []byte(want)) {
			t.Errorf("md に %q がない", want)
		}
	}
	// fixture 中で結果が無いのは Read (tu4) のみで、Read は但し書きも出さない
	if bytes.Contains(md, []byte("(結果なし)")) {
		t.Error("Read に「(結果なし)」が表示されている")
	}
	if bytes.Contains(md, []byte("現れてはならない")) {
		t.Error("スキル展開本文が md に漏れている")
	}
	html, err := os.ReadFile(filepath.Join(outDir, "sess-0001.html"))
	if err != nil {
		t.Fatalf("html が書き出されていない: %v", err)
	}
	if !bytes.Contains(html, []byte(`class="path copy-src">/Users/example/.claude/skills/ohayou<`)) {
		t.Error("html にスキルパスのテキスト表示がない")
	}
	if bytes.Contains(html, []byte(`href="file://`)) {
		t.Error("html にスキルの file:// リンクが残っている")
	}
	if bytes.Contains(html, []byte("現れてはならない")) {
		t.Error("スキル展開本文が html に漏れている")
	}
}

func TestRunRejectsUnsafeSessionID(t *testing.T) {
	inDir := t.TempDir()
	in := filepath.Join(inDir, "evil.jsonl")
	line := `{"type":"user","uuid":"u1","isSidechain":false,"cwd":"/tmp","sessionId":"../../outside","timestamp":"2026-07-12T00:00:01Z","message":{"role":"user","content":"hi"}}` + "\n"
	if err := os.WriteFile(in, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	// 逸脱先 (outDir の2階層上) もサンドボックス内に収まるよう入れ子にする
	sandbox := t.TempDir()
	outDir := filepath.Join(sandbox, "a", "b")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	c := &config{arg: in, format: "md", outDir: outDir, projectsDir: t.TempDir()}
	err := run(c, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "sessionId") {
		t.Fatalf("err = %v, want sessionId のエラー", err)
	}
	if _, statErr := os.Stat(filepath.Join(sandbox, "outside.md")); statErr == nil {
		t.Error("出力先ディレクトリの外にファイルが作られている")
	}
}

func TestRunNoPartialFileOnRenderError(t *testing.T) {
	// 実行時に必ず失敗するテンプレート (存在しないフィールド参照)
	tmplDir := t.TempDir()
	tmpl := filepath.Join(tmplDir, "broken.md.tmpl")
	if err := os.WriteFile(tmpl, []byte("{{.NoSuchField}}"), 0o644); err != nil {
		t.Fatal(err)
	}
	outDir := t.TempDir()
	var stdout, stderr bytes.Buffer
	c := &config{
		arg:         "../../internal/parse/testdata/session_small.jsonl",
		format:      "md",
		outDir:      outDir,
		tmplMD:      tmpl,
		projectsDir: t.TempDir(),
	}
	if err := run(c, &stdout, &stderr); err == nil {
		t.Fatal("レンダリング失敗がエラーになっていない")
	}
	if _, err := os.Stat(filepath.Join(outDir, "sess-0001.md")); err == nil {
		t.Error("失敗したのに不完全な出力ファイルが残っている")
	}
}

func TestRunBothNoPartialSuccess(t *testing.T) {
	// --format both で HTML 側だけ失敗しても、md だけ書かれる部分成功にしない
	tmplDir := t.TempDir()
	tmpl := filepath.Join(tmplDir, "broken.html.tmpl")
	if err := os.WriteFile(tmpl, []byte("{{.NoSuchField}}"), 0o644); err != nil {
		t.Fatal(err)
	}
	outDir := t.TempDir()
	var stdout, stderr bytes.Buffer
	c := &config{
		arg:         "../../internal/parse/testdata/session_small.jsonl",
		format:      "both",
		outDir:      outDir,
		tmplHTML:    tmpl,
		projectsDir: t.TempDir(),
	}
	if err := run(c, &stdout, &stderr); err == nil {
		t.Fatal("HTML レンダリング失敗がエラーになっていない")
	}
	if _, err := os.Stat(filepath.Join(outDir, "sess-0001.md")); err == nil {
		t.Error("HTML が失敗したのに md だけ書き出されている")
	}
}

func TestRunStdout(t *testing.T) {
	var stdout, stderr bytes.Buffer
	c := &config{
		arg:         "../../internal/parse/testdata/session_small.jsonl",
		stdout:      true,
		projectsDir: t.TempDir(),
	}
	if err := run(c, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(stdout.Bytes(), []byte("# セッション sess-0001")) {
		t.Error("--stdout で md が出力されていない")
	}
}
