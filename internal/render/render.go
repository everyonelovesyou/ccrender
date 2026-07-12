package render

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"text/template"

	"cctx/internal/parse"
	"cctx/templates"
)

// Markdown は Session を markdown へレンダリングする。overridePath 非空なら外部テンプレートを使う。
func Markdown(w io.Writer, s *parse.Session, overridePath string) error {
	var t *template.Template
	if overridePath != "" {
		b, err := os.ReadFile(overridePath)
		if err != nil {
			return fmt.Errorf("テンプレートを読めません: %w", err)
		}
		t, err = template.New(filepath.Base(overridePath)).Funcs(Funcs()).Parse(string(b))
		if err != nil {
			return fmt.Errorf("テンプレートの構文エラー: %w", err)
		}
	} else {
		src, err := templates.FS.ReadFile("default.md.tmpl")
		if err != nil {
			return err
		}
		t = template.Must(template.New("default.md.tmpl").Funcs(Funcs()).Parse(string(src)))
	}
	if err := t.Execute(w, s); err != nil {
		return fmt.Errorf("markdown のレンダリングに失敗: %w", err)
	}
	return nil
}
