// Student Hub fork of github.com/ledongthuc/pdf v0.0.0-20250511090121-5959a4027728 (BSD, see LICENSE).
//
// Student uploads (print shop files, quiz notes) are parsed by this package, so hostile files must end.
// Changes from upstream, each marked "studenthub:" in the code:
//   - xref /Prev chains (tables and streams) refuse an offset already visited (an endless loop upstream);
//   - object resolution refuses nesting deeper than maxDepth: an object stream that contains itself
//     overflowed the goroutine stack upstream, a fatal error that kills the whole process;
//   - object stream /Extends chains and page tree walks are bounded;
//   - Deadline wraps the file so that any read after a deadline panics, which ends every other loop
//     that keeps reading (callers already recover from parser panics).

package pdf

import (
	"io"
	"time"
)

// maxDepth bounds nested object resolution and page tree depth.
const maxDepth = 64

type deadlineReader struct {
	r        io.ReaderAt
	deadline time.Time
}

// ErrDeadline is the panic value of a read after the deadline.
var ErrDeadline = errorString("pdf: parse deadline exceeded")

type errorString string

func (e errorString) Error() string { return string(e) }

func (d deadlineReader) ReadAt(p []byte, off int64) (int, error) {
	if time.Now().After(d.deadline) {
		panic(ErrDeadline)
	}
	return d.r.ReadAt(p, off)
}

// Deadline returns r whose reads panic with ErrDeadline after t. Parse it in a function that recovers.
func Deadline(r io.ReaderAt, t time.Time) io.ReaderAt { return deadlineReader{r, t} }
