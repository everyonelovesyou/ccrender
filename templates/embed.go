// Package templates はデフォルトテンプレートをバイナリへ同梱する。
package templates

import "embed"

//go:embed default.md.tmpl
var FS embed.FS
