package quiz

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"sort"
	"strconv"
	"strings"
)

// Office Open XML files (xlsx, docx, pptx) are zip archives. Everything here reads them from
// memory with hard caps, so a zip bomb (tiny archive, gigabytes inflated, or 100k entries)
// fails fast instead of eating the server's 150 MB.

const (
	zipMaxEntries = 2000
	zipMaxEntry   = 20 << 20 // inflated bytes of one XML part we read
	zipMaxTotal   = 60 << 20 // inflated bytes over all parts we read from one file
)

var errZip = errors.New("file_invalid")

type ooxml struct {
	zr     *zip.Reader
	budget int64
}

func isZip(b []byte) bool { return len(b) >= 4 && string(b[:4]) == "PK\x03\x04" }

func openOOXML(data []byte) (*ooxml, error) {
	if !isZip(data) {
		return nil, errType
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil || len(zr.File) > zipMaxEntries {
		return nil, errZip
	}
	return &ooxml{zr: zr, budget: zipMaxTotal}, nil
}

func (o *ooxml) has(name string) bool { return o.find(name) != nil }

func (o *ooxml) find(name string) *zip.File {
	for _, f := range o.zr.File {
		if f.Name == name {
			return f
		}
	}
	return nil
}

// names returns entries matching prefix+<n>+suffix ordered by n (slide2 before slide10).
func (o *ooxml) numbered(prefix, suffix string) []string {
	type e struct {
		n    int
		name string
	}
	var out []e
	for _, f := range o.zr.File {
		if strings.HasPrefix(f.Name, prefix) && strings.HasSuffix(f.Name, suffix) {
			n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(f.Name, prefix), suffix))
			if err == nil {
				out = append(out, e{n, f.Name})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].n < out[j].n })
	names := make([]string, len(out))
	for i, x := range out {
		names[i] = x.name
	}
	return names
}

// read inflates one entry, refusing entries whose real (not declared) size exceeds the caps.
func (o *ooxml) read(name string) ([]byte, error) {
	f := o.find(name)
	if f == nil {
		return nil, errZip
	}
	limit := min(int64(zipMaxEntry), o.budget)
	if f.UncompressedSize64 > uint64(limit) {
		return nil, errTooLarge
	}
	rc, err := f.Open()
	if err != nil {
		return nil, errZip
	}
	defer rc.Close()
	b, err := io.ReadAll(io.LimitReader(rc, limit+1))
	if err != nil {
		return nil, errZip
	}
	if int64(len(b)) > limit {
		return nil, errTooLarge
	}
	o.budget -= int64(len(b))
	return b, nil
}

// xmlText collects the character data of every <textTag> element (w:t in Word, a:t in
// PowerPoint), inserting a newline at the end of each <paraTag> (w:p, a:p).
func xmlText(b []byte, textTag, paraTag string) (string, error) {
	d := xml.NewDecoder(bytes.NewReader(b))
	d.Strict = false
	var sb strings.Builder
	in := 0
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", errZip
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case textTag:
				in++
			case "tab":
				sb.WriteByte('\t')
			case "br":
				sb.WriteByte('\n')
			}
		case xml.EndElement:
			switch t.Name.Local {
			case textTag:
				in--
			case paraTag:
				sb.WriteByte('\n')
			}
		case xml.CharData:
			if in > 0 {
				sb.Write(t)
			}
		}
	}
	return sb.String(), nil
}
