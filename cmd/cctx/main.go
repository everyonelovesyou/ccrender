// cctx は Claude Code のセッショントランスクリプトを markdown / HTML へ整形する CLI。
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"cctx/internal/locate"
	"cctx/internal/parse"
	"cctx/internal/render"
	"cctx/internal/translate"
)

type config struct {
	arg         string
	narg        int
	format      string
	outDir      string
	tmplMD      string
	tmplHTML    string
	stdout      bool
	doTranslate bool
	latest      bool
	project     string
	projectsDir string
}

// validate はフラグの組み合わせ規則 (設計書 CLI 節) を検査する。矛盾指定は明示エラー。
func (c *config) validate() error {
	if c.stdout {
		if c.format == "" {
			c.format = "md"
		}
		if c.format != "md" {
			return fmt.Errorf("--stdout は --format md のみ使えます (指定: %s)", c.format)
		}
		if c.outDir != "" {
			return fmt.Errorf("-o と --stdout は同時に指定できません")
		}
	}
	if c.format == "" {
		c.format = "both"
	}
	if c.format != "md" && c.format != "html" && c.format != "both" {
		return fmt.Errorf("--format は md|html|both のいずれかです (指定: %s)", c.format)
	}
	if c.narg > 1 {
		return fmt.Errorf("位置引数は1つだけ指定できます (指定: %d個)", c.narg)
	}
	if c.latest && c.arg != "" {
		return fmt.Errorf("--latest と位置引数は同時に指定できません")
	}
	if c.project != "" && !c.latest {
		return fmt.Errorf("--project は --latest と組み合わせたときのみ有効です")
	}
	if !c.stdout && !c.latest && c.arg == "" {
		return fmt.Errorf("入力を指定してください (パス / セッションID / --latest)")
	}
	return nil
}

var safeSessionID = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func validateSessionID(id string) error {
	if id == "" {
		return errors.New("sessionId がありません")
	}
	if !safeSessionID.MatchString(id) {
		return fmt.Errorf("sessionId を出力ファイル名に使用できません: %q", id)
	}
	return nil
}

func main() {
	var c config
	flag.StringVar(&c.format, "format", "", "出力形式 md|html|both (デフォルト: both、--stdout 時は md)")
	flag.StringVar(&c.outDir, "o", "", "出力先ディレクトリ (デフォルト: カレント)")
	flag.StringVar(&c.tmplMD, "template-md", "", "markdown 用の自作テンプレート")
	flag.StringVar(&c.tmplHTML, "template-html", "", "HTML 用の自作テンプレート")
	flag.BoolVar(&c.stdout, "stdout", false, "ファイルに書かず標準出力へ (md のみ)")
	flag.BoolVar(&c.doTranslate, "translate", false, "サブエージェントの英語プロンプト/回答を日本語訳")
	flag.BoolVar(&c.latest, "latest", false, "最新セッションを対象にする")
	flag.StringVar(&c.project, "project", "", "--latest の対象をプロジェクト名で絞る")
	flag.Parse()
	c.arg = flag.Arg(0)
	c.narg = flag.NArg()

	if err := run(&c, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "cctx:", err)
		os.Exit(1)
	}
}

func run(c *config, stdout, stderr io.Writer) error {
	if err := c.validate(); err != nil {
		return err
	}
	if c.projectsDir == "" {
		dir, err := locate.DefaultProjectsDir()
		if err != nil {
			return err
		}
		c.projectsDir = dir
	}

	path, err := locate.Resolve(c.arg, c.latest, c.project, c.projectsDir)
	if err != nil {
		return err
	}
	session, err := parse.ParseFile(path, stderr)
	if err != nil {
		return err
	}
	if session.Stats.SkippedLines > 0 {
		fmt.Fprintf(stderr, "cctx: %d行をスキップしました\n", session.Stats.SkippedLines)
	}
	if c.doTranslate {
		if err := translate.Apply(session, translate.NewClaude()); err != nil {
			return err
		}
	}

	if c.stdout {
		return render.Markdown(stdout, session, c.tmplMD)
	}

	// sessionId は入力 JSONL 由来の値なので、出力ファイル名に使う前に許可文字を制限する
	if err := validateSessionID(session.ID); err != nil {
		return err
	}

	outDir := c.outDir
	if outDir == "" {
		outDir = "."
	}
	writeOut := func(ext string, renderFn func(io.Writer, *parse.Session, string) error, tmpl string) error {
		out := filepath.Join(outDir, session.ID+ext)
		f, err := os.Create(out)
		if err != nil {
			return fmt.Errorf("出力先に書けません: %w", err)
		}
		defer f.Close()
		if err := renderFn(f, session, tmpl); err != nil {
			return err
		}
		fmt.Fprintf(stderr, "cctx: %s を書き出しました\n", out)
		return nil
	}
	if c.format == "md" || c.format == "both" {
		if err := writeOut(".md", render.Markdown, c.tmplMD); err != nil {
			return err
		}
	}
	if c.format == "html" || c.format == "both" {
		if err := writeOut(".html", render.HTML, c.tmplHTML); err != nil {
			return err
		}
	}
	return nil
}
