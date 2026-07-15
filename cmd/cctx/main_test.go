package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateFlags(t *testing.T) {
	cases := []struct {
		name    string
		cfg     config
		wantErr string
	}{
		{"stdout は md のみ", config{stdout: true, format: "html"}, "--stdout"},
		{"stdout + both もエラー", config{stdout: true, format: "both"}, "--stdout"},
		{"stdout + md は OK", config{stdout: true, format: "md"}, ""},
		{"stdout + format 省略は md 扱い", config{stdout: true, format: ""}, ""},
		{"-o と --stdout の併用", config{stdout: true, format: "md", outDir: "out"}, "-o"},
		{"--latest と位置引数の併用", config{latest: true, arg: "abc"}, "--latest"},
		{"--project 単独", config{project: "x", arg: "abc"}, "--project"},
		{"不正な format", config{format: "pdf", arg: "abc"}, "format"},
		{"入力なし", config{}, "入力"},
		{"位置引数が2つ以上", config{arg: "abc", narg: 2}, "位置引数"},
		{"位置引数1つは OK", config{arg: "abc", narg: 1}, ""},
		{"通常ケース", config{arg: "abc", format: "both"}, ""},
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
		"こんにちは", "確認します", "🚫", "こっちは触らないで", "サブエージェント", "(結果なし)",
		"/ohayou 今日も", "Skill(ohayou)", "Skill(superpowers:brainstorming)",
	} {
		if !bytes.Contains(md, []byte(want)) {
			t.Errorf("md に %q がない", want)
		}
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
