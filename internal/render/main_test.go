package render

import (
	"os"
	"testing"
	"time"
)

// parseTime がローカル時刻へ変換するため、golden 比較が実行環境のタイムゾーンに
// 依存しないよう UTC に固定する。
func TestMain(m *testing.M) {
	time.Local = time.UTC
	os.Exit(m.Run())
}
