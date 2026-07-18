package render

import (
	"fmt"
	htmltemplate "html/template"
	"io"
	"os"
	"path/filepath"
	"text/template"

	"github.com/everyonelovesyou/ccrender/internal/parse"
	"github.com/everyonelovesyou/ccrender/templates"
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

// HTML は Session を1ファイル完結の HTML へレンダリングする (自動エスケープ付き)。
func HTML(w io.Writer, s *parse.Session, overridePath string) error {
	var t *htmltemplate.Template
	if overridePath != "" {
		b, err := os.ReadFile(overridePath)
		if err != nil {
			return fmt.Errorf("テンプレートを読めません: %w", err)
		}
		t, err = htmltemplate.New(filepath.Base(overridePath)).Funcs(Funcs()).Parse(string(b))
		if err != nil {
			return fmt.Errorf("テンプレートの構文エラー: %w", err)
		}
	} else {
		src, err := templates.FS.ReadFile("default.html.tmpl")
		if err != nil {
			return err
		}
		t = htmltemplate.Must(htmltemplate.New("default.html.tmpl").Funcs(Funcs()).Parse(string(src)))
	}
	if err := t.Execute(w, s); err != nil {
		return fmt.Errorf("HTML のレンダリングに失敗: %w", err)
	}
	return nil
}
