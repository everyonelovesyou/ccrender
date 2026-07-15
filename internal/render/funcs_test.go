package render

import "testing"

func TestTruncateLines(t *testing.T) {
	in := "1\n2\n3\n4\n5"
	if got := TruncateLines(3, in); got != "1\n2\n3\n… (残り2行省略)" {
		t.Errorf("TruncateLines = %q", got)
	}
	if got := TruncateLines(10, in); got != in {
		t.Errorf("行数以内で変更された: %q", got)
	}
	if got := TruncateLines(3, ""); got != "" {
		t.Errorf("空文字列: %q", got)
	}
}

func TestFirstLine(t *testing.T) {
	if got := FirstLine("head\nrest"); got != "head" {
		t.Errorf("FirstLine = %q", got)
	}
	if got := FirstLine("single"); got != "single" {
		t.Errorf("FirstLine = %q", got)
	}
}

func TestJoin(t *testing.T) {
	if got := Join(", ", []string{"a", "b"}); got != "a, b" {
		t.Errorf("Join = %q, want %q", got, "a, b")
	}
	if got := Join(", ", nil); got != "" {
		t.Errorf("Join(nil) = %q, want empty", got)
	}
}
