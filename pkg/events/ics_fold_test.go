package events

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestFoldICS(t *testing.T) {
	long := "SUMMARY:" + strings.Repeat("Қазақ тілі мерекесіне арналған кездесу ", 6)
	out := foldICS(long)
	for i, l := range strings.Split(out, "\r\n") {
		if len(l) > 75 {
			t.Errorf("line %d has %d octets", i, len(l))
		}
		if i > 0 && !strings.HasPrefix(l, " ") {
			t.Errorf("continuation line %d must start with a space", i)
		}
		if !utf8.ValidString(l) {
			t.Errorf("line %d splits a UTF-8 character", i)
		}
	}
	if strings.ReplaceAll(out, "\r\n ", "") != long {
		t.Error("unfolding must restore the original line")
	}
	if foldICS("DTSTART:20261101T050000Z") != "DTSTART:20261101T050000Z" {
		t.Error("short lines stay as they are")
	}
}
