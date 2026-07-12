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

func TestFileURL(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"通常の絶対パス", "/Users/example/.claude/skills/ohayou", "file:///Users/example/.claude/skills/ohayou"},
		{"空白と # を %エスケープする", "/Users/example/my skills/foo#bar", "file:///Users/example/my%20skills/foo%23bar"},
		{"? を %エスケープする", "/Users/example/q?x", "file:///Users/example/q%3Fx"},
		{"相対パスは空文字列 (テキスト表示へ縮退)", "skills/ohayou", ""},
		{"空文字列も空", "", ""},
	}
	for _, c := range cases {
		if got := string(FileURL(c.in)); got != c.want {
			t.Errorf("%s: FileURL(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}
