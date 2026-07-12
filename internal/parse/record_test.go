package parse

import "testing"

func TestBlocksStringContent(t *testing.T) {
	m := &rawMessage{Role: "user", Content: []byte(`"こんにちは"`)}
	got := m.blocks()
	if len(got) != 1 || got[0].Type != "text" || got[0].Text != "こんにちは" {
		t.Fatalf("blocks() = %+v", got)
	}
}

func TestBlocksArrayContent(t *testing.T) {
	m := &rawMessage{Role: "assistant", Content: []byte(
		`[{"type":"text","text":"a"},{"type":"tool_use","id":"tu1","name":"Bash","input":{"command":"ls"}}]`)}
	got := m.blocks()
	if len(got) != 2 || got[0].Text != "a" || got[1].Name != "Bash" || got[1].ID != "tu1" {
		t.Fatalf("blocks() = %+v", got)
	}
}

func TestBlocksNil(t *testing.T) {
	var m *rawMessage
	if got := m.blocks(); got != nil {
		t.Fatalf("nil message: %+v", got)
	}
}

func TestResultTextString(t *testing.T) {
	b := contentBlock{Content: []byte(`"line1\nline2"`)}
	if got := b.resultText(); got != "line1\nline2" {
		t.Fatalf("resultText() = %q", got)
	}
}

func TestResultTextArray(t *testing.T) {
	b := contentBlock{Content: []byte(`[{"type":"text","text":"p1"},{"type":"text","text":"p2"}]`)}
	if got := b.resultText(); got != "p1\np2" {
		t.Fatalf("resultText() = %q", got)
	}
}

func TestResultStringOnlyString(t *testing.T) {
	if _, ok := (contentBlock{Content: []byte(`[{"type":"text","text":"x"}]`)}).resultString(); ok {
		t.Fatal("配列形で ok=true になった")
	}
	s, ok := (contentBlock{Content: []byte(`"deny text"`)}).resultString()
	if !ok || s != "deny text" {
		t.Fatalf("resultString() = %q, %v", s, ok)
	}
}
