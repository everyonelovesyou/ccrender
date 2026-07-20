package parse

import "testing"

func TestRelToRoot(t *testing.T) {
	tests := []struct {
		name string
		root string
		path string
		want string
	}{
		{"subpath", "/repo", "/repo/src/main.go", "src/main.go"},
		{"outside", "/repo", "/other/file.txt", "/other/file.txt"},
		{"similar prefix outside", "/repo", "/repo-other/file.txt", "/repo-other/file.txt"},
		{"parent of root", "/repo/sub", "/repo", "/repo"},
		{"root itself", "/repo", "/repo", "/repo"},

		{"empty root", "", "/repo/file.go", "/repo/file.go"},
		{"empty path", "/repo", "", ""},
		{"relative path unchanged", "/repo", "relative/path.go", "relative/path.go"},

		{"dirty subpath", "/repo", "/repo/./src/../src/main.go", "src/main.go"},
		{"duplicate separators", "/repo", "/repo//src///main.go", "src/main.go"},
		{"trailing slash root", "/repo/", "/repo/src/main.go", "src/main.go"},
		{"dirty root", "/repo/./", "/repo/src/main.go", "src/main.go"},
		{"filesystem root", "/", "/repo/src/main.go", "repo/src/main.go"},

		{"traversal escaping root", "/repo", "/repo/../outside.txt", "/repo/../outside.txt"},
		{"nested traversal escaping root", "/repo", "/repo/src/../../outside.txt", "/repo/src/../../outside.txt"},
		// ".." で始まる名前をroot外と誤判定しない
		{"dotdot-prefixed name inside", "/repo", "/repo/..cache/file.txt", "..cache/file.txt"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := relToRoot(tt.root, tt.path)
			if got != tt.want {
				t.Errorf("relToRoot(%q, %q) = %q, want %q", tt.root, tt.path, got, tt.want)
			}
		})
	}
}
