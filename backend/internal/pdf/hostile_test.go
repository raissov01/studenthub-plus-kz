package pdf

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"
)

// classic builds a PDF with a classic xref table; objs[i] is the body of object i+1.
func classic(trailer string, objs ...string) []byte {
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offs := []int{}
	for i, o := range objs {
		offs = append(offs, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	x := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, o := range offs {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	trailer = strings.ReplaceAll(trailer, "XREF", fmt.Sprint(x))
	fmt.Fprintf(&b, "trailer\n<</Size %d %s>>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, trailer, x)
	return b.Bytes()
}

// xrefStream builds a PDF whose only object is an xref stream (object 2) with W [1 1 1] rows;
// extra is appended to its dictionary ("XREF" = its own offset).
func xrefStream(rows []byte, extra string, before string) []byte {
	size := len(rows) / 3
	var b bytes.Buffer
	b.WriteString("%PDF-1.5\n")
	b.WriteString(before)
	off := b.Len()
	extra = strings.ReplaceAll(extra, "XREF", fmt.Sprint(off))
	rows = bytes.ReplaceAll(rows, []byte{0xEE}, []byte{byte(off)})
	fmt.Fprintf(&b, "2 0 obj\n<</Type/XRef/Size %d/W[1 1 1]%s/Length %d>>\nstream\n", size, extra, len(rows))
	b.Write(rows)
	fmt.Fprintf(&b, "\nendstream\nendobj\nstartxref\n%d\n%%%%EOF\n", off)
	return b.Bytes()
}

func TestHostileFilesEnd(t *testing.T) {
	cases := map[string]struct {
		data []byte
		use  func(r *Reader)
	}{
		// the trailer's /Prev points back at the same xref table
		"xref table Prev loop": {data: classic("/Root 1 0 R/Prev XREF", "<</Type/Catalog>>")},
		// the xref stream's /Prev points at itself
		"xref stream Prev loop": {data: xrefStream([]byte{0, 0, 0, 1, 0xEE, 0, 1, 0xEE, 0}, "/Prev XREF", "")},
		// object 1 lives in object stream 1: resolving it recursed until the stack overflowed (fatal, not a panic)
		"object stream inside itself": {data: xrefStream([]byte{0, 0, 0, 2, 1, 0, 1, 0xEE, 0}, "/Root 1 0 R", ""),
			use: func(r *Reader) { r.Trailer().Key("Root").Key("Pages") }},
		// an object stream that /Extends itself and never holds the object
		"object stream Extends loop": {use: func(r *Reader) { r.Trailer().Key("Root") }},
		// a page tree node that lists itself as its only kid
		"page tree loop": {data: classic("/Root 1 0 R", "<</Type/Catalog/Pages 2 0 R>>", "<</Type/Pages/Count 5/Kids[2 0 R]>>"),
			use: func(r *Reader) { r.Page(3) }},
	}
	// The Extends case needs object 3 = an ObjStm extending itself; write it before the xref stream.
	ext := "3 0 obj\n<</Type/ObjStm/N 0/First 1/Extends 3 0 R/Length 1>>\nstream\n \nendstream\nendobj\n"
	c := cases["object stream Extends loop"]
	c.data = xrefStream([]byte{0, 0, 0, 2, 3, 0, 1, 0xEE, 0, 1, byte(len("%PDF-1.5\n")), 0}, "/Root 1 0 R", ext)
	cases["object stream Extends loop"] = c

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			done := make(chan struct{})
			go func() {
				defer close(done)
				defer func() { _ = recover() }() // a panic is an acceptable "malformed" answer
				r, err := NewReader(bytes.NewReader(c.data), int64(len(c.data)))
				if err != nil || c.use == nil {
					return
				}
				c.use(r)
			}()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("parser did not finish: hostile file loops forever")
			}
		})
	}
}

// A file whose reads go on past the deadline stops with a panic instead of running forever.
func TestDeadlineReader(t *testing.T) {
	data := classic("/Root 1 0 R", "<</Type/Catalog>>")
	r := Deadline(bytes.NewReader(data), time.Now().Add(-time.Second))
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("read after the deadline did not stop the parser")
			}
		}()
		_, _ = NewReader(r, int64(len(data)))
	}()
	ok := Deadline(bytes.NewReader(data), time.Now().Add(time.Minute))
	if _, err := NewReader(ok, int64(len(data))); err != nil {
		t.Fatalf("valid file before the deadline: %v", err)
	}
}
