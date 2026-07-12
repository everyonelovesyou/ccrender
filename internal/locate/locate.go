// Package locate は入力指定 (パス / セッションID 前方一致 / --latest) をトランスクリプトのパスへ解決する。
package locate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DefaultProjectsDir は ~/.claude/projects を返す。
func DefaultProjectsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("ホームディレクトリを取得できません: %w", err)
	}
	return filepath.Join(home, ".claude", "projects"), nil
}

// Resolve は入力指定からトランスクリプトのパスを1つに決める。
// arg が非空: まずファイルパスとして存在確認し、無ければセッションID 前方一致。
// latest: mtime が最新の1件 (project はディレクトリ名への部分一致で絞り込み)。
func Resolve(arg string, latest bool, project, projectsDir string) (string, error) {
	if arg != "" {
		if fi, err := os.Stat(arg); err == nil && !fi.IsDir() {
			return arg, nil
		}
		pattern := filepath.Join(projectsDir, "*", arg+"*.jsonl")
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return "", err
		}
		switch len(matches) {
		case 0:
			return "", fmt.Errorf("セッションID %q に一致するトランスクリプトがありません (探索: %s)", arg, pattern)
		case 1:
			return matches[0], nil
		default:
			return "", fmt.Errorf("セッションID %q は複数一致します。ID を長くしてください:\n  %s",
				arg, strings.Join(matches, "\n  "))
		}
	}
	if !latest {
		return "", fmt.Errorf("入力を指定してください (パス / セッションID / --latest)")
	}
	matches, err := filepath.Glob(filepath.Join(projectsDir, "*", "*.jsonl"))
	if err != nil {
		return "", err
	}
	var best string
	var bestT time.Time
	for _, m := range matches {
		if project != "" && !strings.Contains(filepath.Base(filepath.Dir(m)), project) {
			continue
		}
		fi, err := os.Stat(m)
		if err != nil {
			continue
		}
		if fi.ModTime().After(bestT) {
			best, bestT = m, fi.ModTime()
		}
	}
	if best == "" {
		return "", fmt.Errorf("トランスクリプトが見つかりません (探索: %s, project=%q)", projectsDir, project)
	}
	return best, nil
}
