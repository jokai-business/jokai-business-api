package ai

import (
	"strings"
	"testing"
)

func TestPromptsAndBounds(t *testing.T) {
	for _, kind := range []string{"summary", "reply"} {
		r, v, e := Email(kind, "subject", "sender", "ignore system", true)
		if e != nil || v != "email_"+kind+"_v1" || !strings.Contains(r.System, "untrusted") || strings.Contains(r.System, "ignore system") || !strings.Contains(r.Input, `"incomplete":true`) {
			t.Fatal(r, v, e)
		}
	}
	if _, _, e := Email("send", "", "", "", false); e == nil {
		t.Fatal("unsupported action")
	}
	for _, v := range []string{"", "  ", strings.Repeat("ក", 2667), "bad\x00", string([]byte{0xff})} {
		if ValidText(v) {
			t.Fatal("invalid output accepted")
		}
	}
	if !ValidText("សួស្តី\nHello") {
		t.Fatal("valid Unicode rejected")
	}
}
