package parse

import (
	"encoding/json"
	"testing"
)

func TestEditDiff(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "通常: old と new が各1行",
			input: `{"file_path":"a.go","old_string":"foo","new_string":"bar"}`,
			want:  "- foo\n+ bar",
		},
		{
			name:  "複数行: 5行以内は省略なし",
			input: `{"old_string":"a\nb\nc\nd\ne","new_string":"x"}`,
			want:  "- a\n- b\n- c\n- d\n- e\n+ x",
		},
		{
			name:  "省略: 6行目以降は「… (あと N 行)」に畳む",
			input: `{"old_string":"1\n2\n3\n4\n5\n6\n7","new_string":"x"}`,
			want:  "- 1\n- 2\n- 3\n- 4\n- 5\n… (あと 2 行)\n+ x",
		},
		{
			name:  "片側空: new のみ (追加のみ)",
			input: `{"old_string":"","new_string":"added"}`,
			want:  "+ added",
		},
		{
			name:  "片側空: old のみ (削除のみ)",
			input: `{"old_string":"removed","new_string":""}`,
			want:  "- removed",
		},
		{
			name:  "replace_all: 先頭に印を1行添える",
			input: `{"old_string":"a","new_string":"b","replace_all":true}`,
			want:  "(replace_all)\n- a\n+ b",
		},
		{
			name:  "末尾改行: 1つだけ落として行数を数える",
			input: `{"old_string":"a\n","new_string":"b\n"}`,
			want:  "- a\n+ b",
		},
		{
			name:  "境界値: 改行1つだけの文字列は0行",
			input: `{"old_string":"\n","new_string":"x"}`,
			want:  "+ x",
		},
		{
			name:  "境界値: 改行2つは空行2行として表示",
			input: `{"old_string":"\n\n","new_string":"x"}`,
			want:  "- \n- \n+ x",
		},
		{
			name:  "両方空: Diff なし",
			input: `{"old_string":"","new_string":""}`,
			want:  "",
		},
		{
			name:  "両方空 + replace_all: 印も出さない",
			input: `{"old_string":"","new_string":"","replace_all":true}`,
			want:  "",
		},
		{
			name:  "両方欠落: Diff なし",
			input: `{"file_path":"a.go"}`,
			want:  "",
		},
		{
			name:  "壊れた JSON: 黙って空を返す",
			input: `{"old_string": broken`,
			want:  "",
		},
		{
			name:  "中身が - / + 始まりでも機械的に前置するだけ",
			input: `{"old_string":"- item","new_string":"+ item"}`,
			want:  "- - item\n+ + item",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := editDiff(json.RawMessage(c.input)); got != c.want {
				t.Errorf("editDiff() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestWriteDiff(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "通常: content の全行に + を付ける",
			input: `{"file_path":"a.go","content":"foo\nbar"}`,
			want:  "+ foo\n+ bar",
		},
		{
			name:  "1行",
			input: `{"content":"hello"}`,
			want:  "+ hello",
		},
		{
			name:  "境界値: ちょうど10行は省略なし",
			input: `{"content":"1\n2\n3\n4\n5\n6\n7\n8\n9\n10"}`,
			want:  "+ 1\n+ 2\n+ 3\n+ 4\n+ 5\n+ 6\n+ 7\n+ 8\n+ 9\n+ 10",
		},
		{
			name:  "省略: 11行目以降は「… (あと N 行)」に畳む",
			input: `{"content":"1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12"}`,
			want:  "+ 1\n+ 2\n+ 3\n+ 4\n+ 5\n+ 6\n+ 7\n+ 8\n+ 9\n+ 10\n… (あと 2 行)",
		},
		{
			name:  "末尾改行: 1つだけ落として行数を数える",
			input: `{"content":"a\n"}`,
			want:  "+ a",
		},
		{
			name:  "境界値: 改行1つだけの文字列は0行",
			input: `{"content":"\n"}`,
			want:  "",
		},
		{
			name:  "空 content: Diff なし",
			input: `{"file_path":"a.go","content":""}`,
			want:  "",
		},
		{
			name:  "content 欠落: Diff なし",
			input: `{"file_path":"a.go"}`,
			want:  "",
		},
		{
			name:  "壊れた JSON: 黙って空を返す",
			input: `{"content": broken`,
			want:  "",
		},
		{
			name:  "中身が + 始まりでも機械的に前置するだけ",
			input: `{"content":"+ item"}`,
			want:  "+ + item",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := writeDiff(json.RawMessage(c.input)); got != c.want {
				t.Errorf("writeDiff() = %q, want %q", got, c.want)
			}
		})
	}
}
