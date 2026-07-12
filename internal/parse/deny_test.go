package parse

import (
	"testing"
	"time"
)

func TestDenyReason(t *testing.T) {
	cases := []struct {
		name    string
		content string
		wantMsg string
		wantOK  bool
	}{
		{
			"メッセージ付き + Note 除去",
			"The user doesn't want to proceed with this tool use. The tool use was rejected. To tell you how to proceed, the user said:\nこっちは触らないで\n\nNote: The user's next message may contain a correction.",
			"こっちは触らないで", true,
		},
		{
			"メッセージなし",
			"The user doesn't want to proceed with this tool use. STOP what you are doing.",
			"", true,
		},
		{
			"本文が Note: を含んでも過剰に切らない",
			"The user doesn't want to proceed with this tool use. the user said:\n手順は\n\nNote: を参照して",
			"手順は\n\nNote: を参照して", true,
		},
		{
			"自動拒否は対象外",
			"Permission to use Bash with command rm has been denied.",
			"", false,
		},
		{
			"通常のツールエラーは対象外",
			"exit status 1: command not found",
			"", false,
		},
	}
	for _, c := range cases {
		msg, ok := denyReason(c.content)
		if ok != c.wantOK || msg != c.wantMsg {
			t.Errorf("%s: denyReason = (%q, %v), want (%q, %v)", c.name, msg, ok, c.wantMsg, c.wantOK)
		}
	}
}

func TestPermissionDenyEvent(t *testing.T) {
	s := mustParse(t)
	got := eventsOfKind(s, KindPermissionDeny)
	if len(got) != 1 {
		t.Fatalf("PermissionDeny %d件", len(got))
	}
	tc := got[0].Tool
	if tc.Name != "Edit" || tc.DenyReason != "こっちは触らないで" || !tc.IsError {
		t.Errorf("PermissionDeny: %+v", tc)
	}
}

func TestDenyRequiresIsError(t *testing.T) {
	// is_error=false で文言に前方一致する行 (引用) は検出しない
	b := contentBlock{IsError: false, Content: []byte(`"The user doesn't want to proceed with this tool use."`)}
	ev := toolEvent(contentBlock{Type: "tool_use", ID: "x", Name: "Bash"}, timeZero(), map[string]contentBlock{"x": b})
	if ev.Kind != KindToolCall {
		t.Errorf("引用行を拒否として誤検出: %v", ev.Kind)
	}
}

func TestDenyArrayContentNotDetected(t *testing.T) {
	// content が配列形の拒否文言はクラッシュせず非該当
	b := contentBlock{IsError: true, Content: []byte(`[{"type":"text","text":"The user doesn't want to proceed with this tool use."}]`)}
	ev := toolEvent(contentBlock{Type: "tool_use", ID: "x", Name: "Bash"}, timeZero(), map[string]contentBlock{"x": b})
	if ev.Kind != KindToolCall {
		t.Errorf("配列形を拒否として検出: %v", ev.Kind)
	}
}

func timeZero() time.Time { return time.Time{} }
