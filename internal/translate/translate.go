// Package translate はサブエージェントのプロンプト/回答を claude -p で日本語訳する。
// 英語かどうかの判定はローカルで行わず LLM に委ねる (設計書: 日英混在テキストへの頑健性)。
package translate

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/everyonelovesyou/ccrender/internal/parse"
)

type Translator interface {
	Translate(texts []string) ([]string, error)
}

// Claude は claude CLI (headless) を呼ぶ実装。
type Claude struct {
	Command string
	Model   string
	Timeout time.Duration
}

func NewClaude() *Claude {
	return &Claude{Command: "claude", Model: "haiku", Timeout: 120 * time.Second}
}

var segRE = regexp.MustCompile(`(?m)^<<<CCRENDER-SEG \d+>>>$`)

// Translate は全テキストを1回の claude -p 呼び出しにまとめて翻訳する (逐次起動の遅延を避ける)。
func (c *Claude) Translate(texts []string) ([]string, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	var sb strings.Builder
	sb.WriteString("以下の各セグメントを日本語に翻訳してください。既に日本語主体のセグメントは一切変更せずそのまま返してください。\n")
	sb.WriteString("セグメントは行頭のマーカー <<<CCRENDER-SEG n>>> で区切られています。出力にも同じマーカー行をそのまま含め、セグメント数を変えないでください。\n")
	sb.WriteString("マーカー行とセグメント本文以外 (前置き・後置き・説明) は出力しないでください。\n")
	for i, t := range texts {
		fmt.Fprintf(&sb, "\n<<<CCRENDER-SEG %d>>>\n%s\n", i+1, t)
	}
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.Command, "-p", "--model", c.Model)
	cmd.Stdin = strings.NewReader(sb.String())
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("claude -p の実行に失敗しました: %w", err)
	}
	return splitSegments(string(out), len(texts))
}

// splitSegments は翻訳出力をマーカーで分割する。数が合わなければエラー (部分成功の混在を避ける)。
func splitSegments(out string, n int) ([]string, error) {
	parts := segRE.Split(out, -1)
	if len(parts) > 0 {
		parts = parts[1:] // 先頭マーカーより前の前置きを捨てる
	}
	if len(parts) != n {
		return nil, fmt.Errorf("翻訳結果のセグメント数が一致しません (期待 %d、実際 %d)", n, len(parts))
	}
	res := make([]string, n)
	for i, p := range parts {
		res[i] = strings.TrimSpace(p)
	}
	return res, nil
}

// Apply は Session 中の SubagentCall の Prompt / Answer を訳文で置換する。
func Apply(s *parse.Session, tr Translator) error {
	var texts []string
	var slots []*string
	for i := range s.Events {
		sub := s.Events[i].Subagent
		if s.Events[i].Kind != parse.KindSubagentCall || sub == nil {
			continue
		}
		texts = append(texts, sub.Prompt, sub.Answer)
		slots = append(slots, &sub.Prompt, &sub.Answer)
	}
	if len(texts) == 0 {
		return nil
	}
	translated, err := tr.Translate(texts)
	if err != nil {
		return err
	}
	for i, p := range slots {
		*p = translated[i]
	}
	return nil
}
