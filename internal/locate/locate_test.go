package locate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeProjects は ~/.claude/projects 相当の階層を組み立てる。
func fakeProjects(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mk := func(dir, name string, mtime time.Time) string {
		d := filepath.Join(root, dir)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(d, name)
		if err := os.WriteFile(p, []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, mtime, mtime); err != nil {
			t.Fatal(err)
		}
		return p
	}
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	mk("-Users-x-proj-alpha", "aaaa1111-0000-0000-0000-000000000000.jsonl", base)
	mk("-Users-x-proj-alpha", "bbbb2222-0000-0000-0000-000000000000.jsonl", base.Add(2*time.Hour))
	mk("-Users-x-proj-beta", "aaaa9999-0000-0000-0000-000000000000.jsonl", base.Add(1*time.Hour))
	return root
}

func TestResolveDirectPath(t *testing.T) {
	root := fakeProjects(t)
	p := filepath.Join(root, "-Users-x-proj-alpha", "aaaa1111-0000-0000-0000-000000000000.jsonl")
	got, err := Resolve(p, false, "", root)
	if err != nil || got != p {
		t.Fatalf("Resolve(path) = %q, %v", got, err)
	}
}

func TestResolvePrefixUnique(t *testing.T) {
	root := fakeProjects(t)
	got, err := Resolve("bbbb", false, "", root)
	if err != nil || !strings.HasSuffix(got, "bbbb2222-0000-0000-0000-000000000000.jsonl") {
		t.Fatalf("Resolve(prefix) = %q, %v", got, err)
	}
}

func TestResolvePrefixAmbiguous(t *testing.T) {
	root := fakeProjects(t)
	_, err := Resolve("aaaa", false, "", root)
	if err == nil || !strings.Contains(err.Error(), "複数") {
		t.Fatalf("複数一致でエラーにならない: %v", err)
	}
	// 候補一覧を含むこと
	if !strings.Contains(err.Error(), "aaaa1111") || !strings.Contains(err.Error(), "aaaa9999") {
		t.Errorf("候補一覧がない: %v", err)
	}
}

func TestResolvePrefixNotFound(t *testing.T) {
	root := fakeProjects(t)
	_, err := Resolve("zzzz", false, "", root)
	if err == nil || !strings.Contains(err.Error(), "zzzz") {
		t.Fatalf("0件一致でエラーにならない: %v", err)
	}
}

func TestResolveLatest(t *testing.T) {
	root := fakeProjects(t)
	got, err := Resolve("", true, "", root)
	if err != nil || !strings.HasSuffix(got, "bbbb2222-0000-0000-0000-000000000000.jsonl") {
		t.Fatalf("最新 (mtime 基準) が取れない: %q, %v", got, err)
	}
}

func TestResolveLatestWithProject(t *testing.T) {
	root := fakeProjects(t)
	got, err := Resolve("", true, "beta", root)
	if err != nil || !strings.HasSuffix(got, "aaaa9999-0000-0000-0000-000000000000.jsonl") {
		t.Fatalf("--project 絞り込み: %q, %v", got, err)
	}
}

func TestResolveLatestNoMatch(t *testing.T) {
	root := fakeProjects(t)
	_, err := Resolve("", true, "no-such-project", root)
	if err == nil {
		t.Fatal("--latest 0件でエラーにならない")
	}
}
