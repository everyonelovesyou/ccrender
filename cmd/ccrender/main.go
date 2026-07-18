// ccrender は Claude Code のセッショントランスクリプトから Markdown / HTML に描画する CLI。
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"github.com/everyonelovesyou/ccrender/internal/locate"
	"github.com/everyonelovesyou/ccrender/internal/parse"
	"github.com/everyonelovesyou/ccrender/internal/render"
	"github.com/everyonelovesyou/ccrender/internal/translate"
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

// newSubcommand は名前に応じた config と FlagSet を組み立てる。未知の名前は ok=false。
// config.format / config.stdout はフラグではなくサブコマンド名から決まる。
func newSubcommand(name string) (c *config, fs *flag.FlagSet, ok bool) {
	c = &config{}
	fs = flag.NewFlagSet("ccrender "+name, flag.ContinueOnError)
	fs.SetOutput(io.Discard) // 出力先 (stdout/stderr) は呼び出し側が制御する
	addOut := func() { fs.StringVar(&c.outDir, "o", "", "出力先ディレクトリ (デフォルト: カレント)") }
	addMD := func() { fs.StringVar(&c.tmplMD, "template-md", "", "Markdown 用の自作テンプレート") }
	addHTML := func() { fs.StringVar(&c.tmplHTML, "template-html", "", "HTML 用の自作テンプレート") }
	switch name {
	case "md":
		c.format = "md"
		addOut()
		addMD()
	case "html":
		c.format = "html"
		addOut()
		addHTML()
	case "both":
		c.format = "both"
		addOut()
		addMD()
		addHTML()
	case "stdout":
		c.format = "md"
		c.stdout = true
		addMD()
	default:
		return nil, nil, false
	}
	fs.BoolVar(&c.doTranslate, "translate", false, "サブエージェントの英語プロンプト/回答を日本語訳")
	fs.BoolVar(&c.latest, "latest", false, "最新セッションを対象にする")
	fs.StringVar(&c.project, "project", "", "--latest の対象をプロジェクト名で絞る")
	return c, fs, true
}

// parseSubcommand はサブコマンドのフラグ列をパースして config を返す。
// -h は flag.ErrHelp をそのまま返す (exit 0 にする扱いは呼び出し側)。
func parseSubcommand(name string, args []string) (*config, error) {
	c, fs, ok := newSubcommand(name)
	if !ok {
		return nil, fmt.Errorf("未知のサブコマンドです: %q", name)
	}
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	c.arg = fs.Arg(0)
	c.narg = fs.NArg()
	return c, nil
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

const usageText = `使い方: ccrender <サブコマンド> [フラグ] <入力>

サブコマンド:
  md      Markdown をファイルに書き出す
  html    HTML をファイルに書き出す
  both    Markdown と HTML の両方をファイルに書き出す
  stdout  Markdown を標準出力へ書く (パイプ用)
  help    使い方を表示する (help <サブコマンド> でフラグ一覧)

入力 (全サブコマンド共通):
  <パス>                  パス直接指定
  <セッションID>          前方一致で ~/.claude/projects/ を探索
  --latest [--project p]  最新セッション (mtime 基準)

用例:
  ccrender both --latest
  ccrender md abc123
  ccrender html -o out path/to/session.jsonl
  ccrender stdout --latest --project my-app

フラグは位置引数より前に置いてください。詳細: ccrender help <サブコマンド>
`

func printUsage(w io.Writer) {
	fmt.Fprint(w, usageText)
}

// printSubcommandUsage は name のフラグ一覧を w へ書く。未知の name は呼び出し側で弾いておく。
func printSubcommandUsage(name string, w io.Writer) {
	_, fs, ok := newSubcommand(name)
	if !ok {
		return
	}
	fmt.Fprintf(w, "使い方: ccrender %s [フラグ] <入力>\n\nフラグ:\n", name)
	fs.SetOutput(w)
	fs.PrintDefaults()
}

// realMain はサブコマンドへ分岐し exit コードを返す。
// help 系の正常表示は stdout (exit 0)、エラー起因の表示は stderr (exit 1)。
func realMain(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stderr)
		return 1
	}
	switch args[0] {
	case "-h", "-help", "--help", "help":
		if args[0] == "help" && len(args) > 1 {
			if _, _, ok := newSubcommand(args[1]); !ok {
				fmt.Fprintf(stderr, "ccrender: 未知のサブコマンドです: %q\n", args[1])
				printUsage(stderr)
				return 1
			}
			printSubcommandUsage(args[1], stdout)
			return 0
		}
		printUsage(stdout)
		return 0
	}
	if _, _, ok := newSubcommand(args[0]); !ok {
		fmt.Fprintf(stderr, "ccrender: 未知のサブコマンドです: %q\n", args[0])
		printUsage(stderr)
		return 1
	}
	c, err := parseSubcommand(args[0], args[1:])
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printSubcommandUsage(args[0], stdout)
			return 0
		}
		fmt.Fprintln(stderr, "ccrender:", err)
		return 1
	}
	if err := run(c, stdout, stderr); err != nil {
		fmt.Fprintln(stderr, "ccrender:", err)
		return 1
	}
	return 0
}

func main() {
	os.Exit(realMain(os.Args[1:], os.Stdout, os.Stderr))
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
		fmt.Fprintf(stderr, "ccrender: %d行をスキップしました\n", session.Stats.SkippedLines)
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
	// 失敗時に不完全・部分的な成果物を残さないよう、全形式をバッファへ描画してから書き出す
	type output struct {
		path string
		data []byte
	}
	var outputs []output
	renderTo := func(ext string, renderFn func(io.Writer, *parse.Session, string) error, tmpl string) error {
		var buf bytes.Buffer
		if err := renderFn(&buf, session, tmpl); err != nil {
			return err
		}
		outputs = append(outputs, output{filepath.Join(outDir, session.ID+ext), buf.Bytes()})
		return nil
	}
	if c.format == "md" || c.format == "both" {
		if err := renderTo(".md", render.Markdown, c.tmplMD); err != nil {
			return err
		}
	}
	if c.format == "html" || c.format == "both" {
		if err := renderTo(".html", render.HTML, c.tmplHTML); err != nil {
			return err
		}
	}
	for _, o := range outputs {
		if err := os.WriteFile(o.path, o.data, 0o644); err != nil {
			return fmt.Errorf("出力先に書けません: %w", err)
		}
		fmt.Fprintf(stderr, "ccrender: %s を書き出しました\n", o.path)
	}
	return nil
}
