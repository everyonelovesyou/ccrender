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
	for _, want := range []string{"こんにちは", "確認します", "🚫", "こっちは触らないで", "サブエージェント", "(結果なし)"} {
		if !bytes.Contains(md, []byte(want)) {
			t.Errorf("md に %q がない", want)
		}
	}
	if _, err := os.Stat(filepath.Join(outDir, "sess-0001.html")); err != nil {
		t.Errorf("html が書き出されていない: %v", err)
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
