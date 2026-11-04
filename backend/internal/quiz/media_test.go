package quiz

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testJPEG(t *testing.T, w, h int, exif string) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := range w {
		for y := range h {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 100, 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	b := buf.Bytes()
	if exif == "" {
		return b
	}
	payload := append([]byte("Exif\x00\x00"), []byte(exif)...)
	seg := []byte{0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(seg[2:], uint16(len(payload)+2))
	out := append([]byte{}, b[:2]...)
	out = append(out, seg...)
	out = append(out, payload...)
	return append(out, b[2:]...)
}

func fileExists(name string) bool {
	_, err := os.Stat(filepath.Join(UploadDir(), name))
	return err == nil
}

func uploadJPEG(t *testing.T, uid int64) string {
	t.Helper()
	w := doMultipart(t, "/api/quiz/media?kind=image", uid, []part{{"file", "a.jpg", testJPEG(t, 60, 40, "")}})
	if w.Code != 201 {
		t.Fatalf("upload: %d %s", w.Code, w.Body)
	}
	return decode[map[string]any](t, w)["name"].(string)
}

func TestImageUploadStripsEXIFAndValidates(t *testing.T) {
	uid, other := newUser(t), newUser(t)
	src := testJPEG(t, 2000, 1000, "GPSLatitude=51.1694;Author=Secret Student")
	if !bytes.Contains(src, []byte("GPSLatitude")) {
		t.Fatal("fixture has no exif")
	}
	w := doMultipart(t, "/api/quiz/media?kind=image", uid, []part{{"file", "photo.jpg", src}})
	if w.Code != 201 {
		t.Fatalf("upload: %d %s", w.Code, w.Body)
	}
	res := decode[map[string]any](t, w)
	name := res["name"].(string)
	if res["width"].(float64) != imageMaxSide || res["height"].(float64) != imageMaxSide/2 {
		t.Fatalf("not resized: %v", res)
	}
	stored, err := os.ReadFile(filepath.Join(UploadDir(), name))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(stored, []byte("GPSLatitude")) || bytes.Contains(stored, []byte("Exif")) || bytes.Contains(stored, []byte("Secret")) {
		t.Fatal("EXIF survived re-encoding")
	}
	// unattached media: owner only
	if w := do(t, "GET", "/api/quiz/media/"+name, other, nil); w.Code != 404 {
		t.Fatalf("foreign private media: %d", w.Code)
	}
	if w := do(t, "GET", "/api/quiz/media/"+name, uid, nil); w.Code != 200 || w.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("own media: %d", w.Code)
	}
	// someone else's upload cannot be put into my quiz
	body := sampleQuiz("public")
	body["questions"].([]map[string]any)[0]["image"] = name
	if w := do(t, "POST", "/api/quiz/quizzes", other, body); w.Code != 400 || errCode(w) != "bad_media" {
		t.Fatalf("foreign media in quiz: %d %s", w.Code, w.Body)
	}
	// attached to a public quiz → public
	createQuizT(t, uid, body)
	if w := do(t, "GET", "/api/quiz/media/"+name, 0, nil); w.Code != 200 {
		t.Fatalf("public media: %d", w.Code)
	}

	var pngBuf bytes.Buffer
	png.Encode(&pngBuf, image.NewRGBA(image.Rect(0, 0, 10, 10)))
	cases := []struct {
		name string
		data []byte
		code int
		err  string
	}{
		{"png ok", pngBuf.Bytes(), 201, ""},
		{"too large", append(testJPEG(t, 10, 10, ""), make([]byte, 1<<20)...), 413, "media_too_large"},
		{"html", []byte("<html><script>alert(1)</script></html>"), 415, "media_type"},
		{"heic", append([]byte("\x00\x00\x00\x18ftypheic"), make([]byte, 100)...), 415, "file_heic"},
		{"truncated jpeg", testJPEG(t, 10, 10, "")[:20], 400, "media_invalid"},
	}
	for _, c := range cases {
		w := doMultipart(t, "/api/quiz/media?kind=image", uid, []part{{"file", "x", c.data}})
		if w.Code != c.code || (c.err != "" && errCode(w) != c.err) {
			t.Errorf("%s: %d %s", c.name, w.Code, w.Body)
		}
	}
	// a decompression bomb header: tiny PNG declaring a huge canvas
	bomb := pngBuf.Bytes()
	bomb = append([]byte{}, bomb...)
	binary.BigEndian.PutUint32(bomb[16:], 30000)
	binary.BigEndian.PutUint32(bomb[20:], 30000)
	if _, _, _, err := processImage(ctxb(), bomb, 1<<20, 1280, 1<<20); err != errPixels && err != errDecode {
		t.Fatalf("bomb: %v", err)
	}
}

func mp3Frame() []byte {
	// MPEG1 Layer III, 128 kbps, 44.1 kHz, no padding: 417 bytes
	f := make([]byte, 417)
	f[0], f[1], f[2], f[3] = 0xFF, 0xFB, 0x90, 0x00
	return f
}

func TestAudioUpload(t *testing.T) {
	uid := newUser(t)
	id3 := append([]byte("ID3\x03\x00\x00\x00\x00\x00\x10"), []byte("TPE1 Secret Name")...)
	data := append(id3, bytes.Repeat(mp3Frame(), 5)...)
	tag := append([]byte("TAG"), make([]byte, 125)...)
	data = append(data, tag...)
	w := doMultipart(t, "/api/quiz/media?kind=audio", uid, []part{{"file", "a.mp3", data}})
	if w.Code != 201 {
		t.Fatalf("mp3: %d %s", w.Code, w.Body)
	}
	name := decode[map[string]any](t, w)["name"].(string)
	stored, _ := os.ReadFile(filepath.Join(UploadDir(), name))
	if bytes.Contains(stored, []byte("Secret")) || bytes.HasPrefix(stored, []byte("ID3")) || len(stored) != 5*417 {
		t.Fatalf("tags kept: %d bytes", len(stored))
	}
	for _, bad := range [][]byte{[]byte("OggS....."), bytes.Repeat([]byte{0x41}, 2000), append(mp3Frame()[:4], make([]byte, 10)...)} {
		if w := doMultipart(t, "/api/quiz/media?kind=audio", uid, []part{{"file", "a.mp3", bad}}); w.Code < 400 {
			t.Errorf("bad audio accepted: %d", w.Code)
		}
	}
}

func TestExcelImport(t *testing.T) {
	uid := newUser(t)
	good, err := writeXLSX(append(templateRows, []string{"", "mcq", "a", "b"}, []string{"Bad row", "mcq", "x", "y", "", "", "", "", "z"}))
	if err != nil {
		t.Fatal(err)
	}
	w := doMultipart(t, "/api/quiz/import/xlsx", uid, []part{{"file", "q.xlsx", good}})
	if w.Code != 200 {
		t.Fatalf("import: %d %s", w.Code, w.Body)
	}
	res := decode[struct {
		Questions []Question      `json:"questions"`
		Warnings  []ImportWarning `json:"warnings"`
	}](t, w)
	if len(res.Questions) != 5 {
		t.Fatalf("questions: %+v", res)
	}
	types := []string{}
	for _, q := range res.Questions {
		types = append(types, q.Type)
	}
	if strings.Join(types, ",") != "mcq,mcq_multi,true_false,fill_blank,reorder" || *res.Questions[0].CorrectIndex != 1 ||
		len(res.Questions[3].Answers) != 3 || res.Questions[0].TimeLimit != 20 {
		t.Fatalf("parsed: %+v", res.Questions)
	}
	if len(res.Warnings) != 2 || res.Warnings[0].Row != 7 || res.Warnings[1].Row != 8 {
		t.Fatalf("warnings: %+v", res.Warnings)
	}
	// the imported questions save as a quiz unchanged
	if w := do(t, "POST", "/api/quiz/quizzes?source=excel", uid, map[string]any{"title": "Excel", "questions": res.Questions}); w.Code != 201 {
		t.Fatalf("save imported: %d %s", w.Code, w.Body)
	}
	// Quizizz-style header with options 1–5 and a numeric correct column
	qz, _ := writeXLSX([][]string{
		{"#", "Question Text", "Question Type", "Option 1", "Option 2", "Option 3", "Option 4", "Option 5", "Correct Answer", "Time in seconds"},
		{"1", "Capital of KZ?", "Multiple Choice", "Almaty", "Astana", "", "", "", "2", "30"},
	})
	res2 := decode[struct{ Questions []Question }](t, doMultipart(t, "/api/quiz/import/xlsx", uid, []part{{"file", "q.xlsx", qz}}))
	if len(res2.Questions) != 1 || *res2.Questions[0].CorrectIndex != 1 {
		t.Fatalf("quizizz layout: %+v", res2)
	}
	// bad files
	for name, data := range map[string][]byte{
		"not a zip": []byte("Question,Type\nx,y"),
		"docx":      mustDocx(t, "hello"),
	} {
		if w := doMultipart(t, "/api/quiz/import/xlsx", uid, []part{{"file", "x.xlsx", data}}); w.Code != 400 || errCode(w) != "xlsx_invalid" {
			t.Errorf("%s: %d %s", name, w.Code, w.Body)
		}
	}
	empty, _ := writeXLSX([][]string{{"Question", "Correct"}})
	if w := doMultipart(t, "/api/quiz/import/xlsx", uid, []part{{"file", "x.xlsx", empty}}); w.Code != 422 {
		t.Errorf("empty: %d", w.Code)
	}
	// template downloads and re-imports
	tw := do(t, "GET", "/api/quiz/import/template.xlsx", 0, nil)
	if tw.Code != 200 || !isZip(tw.Body.Bytes()) {
		t.Fatalf("template: %d", tw.Code)
	}
	rows, err := readSheet(tw.Body.Bytes())
	if err != nil || len(rows) != len(templateRows) || rows[1][0] != templateRows[1][0] {
		t.Fatalf("template roundtrip: %v %v", err, rows)
	}
	_ = fmt.Sprint
}
