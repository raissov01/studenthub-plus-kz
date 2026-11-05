package printshop

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/raissov01/studenthub-plus-kz/backend/internal/shopkit"
)

// Prices are integers in tiyn and whole tenge (Kaspi takes tenge). The client sends only ids and options; this
// file is the only place a price is computed, from the shop's own rows.

// Sizes a print service may price, in display order (A2…A0 are posters).
var Sizes = []string{"A5", "A4", "A3", "A2", "A1", "A0"}

// SizePrice is the price of one printed side at one paper size. Color 0 = no colour at this size; a duplex price
// 0 = no duplex at this size.
type SizePrice struct {
	BW          int64 `json:"bw"`
	Color       int64 `json:"color"`
	BWDuplex    int64 `json:"bwDuplex"`
	ColorDuplex int64 `json:"colorDuplex"`
}

// Tier: a bound copy of at most MaxSheets sheets costs Price.
type Tier struct {
	MaxSheets int   `json:"maxSheets"`
	Price     int64 `json:"price"`
}

// Pricing is one service's price rules; which fields apply depends on the service kind.
type Pricing struct {
	Sizes  map[string]SizePrice `json:"sizes,omitempty"`  // print: per printed side
	Tiers  []Tier               `json:"tiers,omitempty"`  // binding: per bound copy, by sheets
	Sheet  map[string]int64     `json:"sheet,omitempty"`  // lamination: per sheet, by size
	Price  int64                `json:"price,omitempty"`  // unit: per unit
	UnitKK string               `json:"unitKk,omitempty"` // unit: «бет», «дана»
	UnitRU string               `json:"unitRu,omitempty"`
}

var Kinds = []string{"print", "binding", "lamination", "unit"}

func priceOK(p int64, zeroOK bool) bool {
	if p == 0 {
		return zeroOK
	}
	return p >= 100 && p <= 10000000 && p%100 == 0
}

func knownSize(s string) bool {
	for _, x := range Sizes {
		if x == s {
			return true
		}
	}
	return false
}

// ValidPricing checks a service's rules as a manager entered them.
func ValidPricing(kind string, p Pricing) error {
	bad := errors.New("printshop: bad pricing")
	switch kind {
	case "print":
		if len(p.Sizes) == 0 || len(p.Tiers) > 0 || len(p.Sheet) > 0 || p.Price != 0 {
			return bad
		}
		for size, sp := range p.Sizes {
			if !knownSize(size) || !priceOK(sp.BW, true) || !priceOK(sp.Color, true) || !priceOK(sp.BWDuplex, true) || !priceOK(sp.ColorDuplex, true) ||
				(sp.BW == 0 && sp.Color == 0) || (sp.BW == 0 && sp.BWDuplex != 0) || (sp.Color == 0 && sp.ColorDuplex != 0) {
				return bad
			}
		}
	case "binding":
		if len(p.Tiers) == 0 || len(p.Tiers) > 10 || len(p.Sizes) > 0 || len(p.Sheet) > 0 || p.Price != 0 {
			return bad
		}
		prev := 0
		for _, t := range p.Tiers {
			if t.MaxSheets <= prev || t.MaxSheets > 1000 || !priceOK(t.Price, false) {
				return bad
			}
			prev = t.MaxSheets
		}
	case "lamination":
		if len(p.Sheet) == 0 || len(p.Sizes) > 0 || len(p.Tiers) > 0 || p.Price != 0 {
			return bad
		}
		for size, v := range p.Sheet {
			if !knownSize(size) || !priceOK(v, false) {
				return bad
			}
		}
	case "unit":
		if !priceOK(p.Price, false) || len(p.Sizes) > 0 || len(p.Tiers) > 0 || len(p.Sheet) > 0 ||
			!shopkit.TextOK(p.UnitKK, 1, 20) || !shopkit.TextOK(p.UnitRU, 1, 20) {
			return bad
		}
	default:
		return bad
	}
	return nil
}

// ---------- page ranges ----------

var reRange = regexp.MustCompile(`^\d{1,4}(-\d{1,4})?$`)

// PageRange counts the pages of "1-5, 8, 10-12" inside 1…pages ("" = all of them) and returns the normalized
// form. Pages out of range, reversed spans and more than 50 parts are refused.
func PageRange(s string, pages int) (int, string, error) {
	s = strings.TrimSpace(s)
	if pages < 1 {
		return 0, "", errRange
	}
	if s == "" {
		return pages, "", nil
	}
	parts := strings.Split(strings.ReplaceAll(s, " ", ""), ",")
	if len(parts) > 50 {
		return 0, "", errRange
	}
	seen := map[int]bool{}
	type span struct{ a, b int }
	var spans []span
	for _, p := range parts {
		if !reRange.MatchString(p) {
			return 0, "", errRange
		}
		a, b, isSpan := strings.Cut(p, "-")
		from, _ := strconv.Atoi(a)
		to := from
		if isSpan {
			to, _ = strconv.Atoi(b)
		}
		if from < 1 || to < from || to > pages {
			return 0, "", errRange
		}
		for i := from; i <= to; i++ {
			seen[i] = true
		}
		spans = append(spans, span{from, to})
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].a < spans[j].a })
	// merge for the normalized form
	var merged []span
	for _, sp := range spans {
		if n := len(merged); n > 0 && sp.a <= merged[n-1].b+1 {
			if sp.b > merged[n-1].b {
				merged[n-1].b = sp.b
			}
			continue
		}
		merged = append(merged, sp)
	}
	var out []string
	for _, sp := range merged {
		if sp.a == sp.b {
			out = append(out, strconv.Itoa(sp.a))
		} else {
			out = append(out, fmt.Sprintf("%d-%d", sp.a, sp.b))
		}
	}
	norm := strings.Join(out, ",")
	if len(seen) == pages {
		norm = ""
	}
	return len(seen), norm, nil
}

// ---------- the quote ----------

// FileInfo is what the server knows about one uploaded file.
type FileInfo struct {
	ID          int64
	Kind        string // pdf, docx, xlsx, pptx, jpg, png
	DisplayName string
	Pages       *int // counted by the server; nil = the student declares, the operator confirms
	PagesHint   *int
}

// FileInput is one file's options as the client sends them. No price.
type FileInput struct {
	FileID       int64  `json:"fileId"`
	ServiceID    int64  `json:"serviceId"`
	Size         string `json:"size"`
	Color        bool   `json:"color"`
	Duplex       bool   `json:"duplex"`
	Copies       int    `json:"copies"`
	Range        string `json:"range"`
	Pages        int    `json:"pages"` // declared, only when the server could not count
	BindingID    int64  `json:"bindingId"`
	LaminationID int64  `json:"laminationId"`
}

type QtyInput struct {
	ID  int64 `json:"id"`
	Qty int   `json:"qty"`
}

// QuoteInput is a whole job as the client sends it.
type QuoteInput struct {
	ShopID   int64       `json:"shopId"`
	Files    []FileInput `json:"files"`
	Goods    []QtyInput  `json:"goods"`
	Services []QtyInput  `json:"services"` // unit services: copies at the counter, scans, cards
	// accepted and ignored: a total from the client is never trusted
	Total *int64 `json:"total,omitempty"`
}

// Part is one priced component of a line (print, binding, lamination).
type Part struct {
	NameKK string `json:"nameKk"`
	NameRU string `json:"nameRu"`
	Amount int64  `json:"amount"`
}

// Line is the job's snapshot: names, options and prices as they were when the job was placed.
type Line struct {
	Kind          string `json:"kind"` // file, goods, service, minimum
	FileID        int64  `json:"fileId,omitempty"`
	RefID         int64  `json:"refId,omitempty"` // service or goods id
	NameKK        string `json:"nameKk"`
	NameRU        string `json:"nameRu"`
	ServiceID     int64  `json:"serviceId,omitempty"`
	Size          string `json:"size,omitempty"`
	Color         bool   `json:"color,omitempty"`
	Duplex        bool   `json:"duplex,omitempty"`
	Copies        int    `json:"copies,omitempty"`
	Range         string `json:"range,omitempty"`
	FilePages     int    `json:"filePages,omitempty"` // pages of the whole file
	Pages         int    `json:"pages,omitempty"`     // printed pages per copy
	Sheets        int    `json:"sheets,omitempty"`    // sheets per copy
	PagesDeclared bool   `json:"pagesDeclared,omitempty"`
	BindingID     int64  `json:"bindingId,omitempty"`
	LaminationID  int64  `json:"laminationId,omitempty"`
	Qty           int    `json:"qty,omitempty"`
	UnitPrice     int64  `json:"unitPrice,omitempty"`
	Parts         []Part `json:"parts,omitempty"`
	Total         int64  `json:"total"`
}

// Quoted is the server's price of a job. Review: some page counts were declared, the operator confirms first.
type Quoted struct {
	Lines  []Line `json:"lines"`
	Total  int64  `json:"total"`
	Review bool   `json:"review"`
}

// Catalog is the part of a shop the quote reads.
type Catalog struct {
	MinOrder int64
	Accepts  []string
	MaxTotal int64
	Services map[int64]Service
	Goods    map[int64]Goods
}

const (
	maxJobFiles   = 5
	maxQtyLines   = 15
	maxCopies     = 100
	maxQty        = 50
	maxSides      = 20000
	maxDeclared   = 2000
	minDeclared   = 1
	imagePages    = 1
	maxFileDetail = 120
)

// quote errors (code → message in printshop.go)
var (
	errNoLines    = qerr("empty_job")
	errFile       = qerr("bad_file")
	errFormat     = qerr("format_not_accepted")
	errService    = qerr("service_unavailable")
	errSize       = qerr("bad_size")
	errColor      = qerr("no_color")
	errDuplex     = qerr("no_duplex")
	errRange      = qerr("bad_range")
	errPages      = qerr("pages_required")
	errCopies     = qerr("bad_copies")
	errBinding    = qerr("binding_too_thick")
	errGoods      = qerr("goods_unavailable")
	errTooMuch    = qerr("too_large")
	errDuplicates = qerr("duplicate_file")
)

type quoteErr string

func (e quoteErr) Error() string { return string(e) }
func qerr(code string) error     { return quoteErr(code) }

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func ceilHalf(n int) int { return (n + 1) / 2 }

// Quote prices a job from the shop's own catalogue and the server's own knowledge of the files. Unknown,
// inactive or offline services, sizes or options the service does not price, out-of-stock goods and
// over-limit quantities are refused. A file whose pages the server could not count takes the student's
// declared count and makes the whole quote a review (the operator confirms before payment).
func Quote(cat Catalog, files map[int64]FileInfo, in QuoteInput) (*Quoted, error) {
	if len(in.Files) == 0 && len(in.Goods) == 0 && len(in.Services) == 0 {
		return nil, errNoLines
	}
	if len(in.Files) > maxJobFiles || len(in.Goods)+len(in.Services) > maxQtyLines {
		return nil, errTooMuch
	}
	q := &Quoted{Lines: []Line{}}
	usedFiles := map[int64]bool{}
	sides := 0
	for _, fi := range in.Files {
		f, ok := files[fi.FileID]
		if !ok {
			return nil, errFile
		}
		if usedFiles[f.ID] {
			return nil, errDuplicates
		}
		usedFiles[f.ID] = true
		if !contains(cat.Accepts, f.Kind) {
			return nil, errFormat
		}
		svc, ok := cat.Services[fi.ServiceID]
		if !ok || !svc.Active || !svc.Online || svc.Kind != "print" {
			return nil, errService
		}
		sp, ok := svc.Pricing.Sizes[fi.Size]
		if !ok {
			return nil, errSize
		}
		if fi.Copies < 1 || fi.Copies > maxCopies {
			return nil, errCopies
		}
		line := Line{Kind: "file", FileID: f.ID, NameKK: f.DisplayName, NameRU: f.DisplayName, ServiceID: svc.ID, Size: fi.Size,
			Color: fi.Color, Duplex: fi.Duplex, Copies: fi.Copies, Parts: []Part{}}
		// pages of the file: the server's count, or the student's declared one (then the operator confirms)
		filePages := 0
		switch {
		case f.Kind == "jpg" || f.Kind == "png":
			filePages = imagePages
		case f.Pages != nil:
			filePages = *f.Pages
		default:
			if fi.Pages < minDeclared || fi.Pages > maxDeclared {
				return nil, errPages
			}
			filePages = fi.Pages
			line.PagesDeclared = true
			q.Review = true
		}
		if (f.Kind == "jpg" || f.Kind == "png") && fi.Range != "" {
			return nil, errRange
		}
		pages, norm, err := PageRange(fi.Range, filePages)
		if err != nil {
			return nil, err
		}
		line.FilePages, line.Pages, line.Range = filePages, pages, norm
		if fi.Duplex && pages < 2 {
			line.Duplex, fi.Duplex = false, false // one page: nothing to print on the back
		}
		var side int64
		switch {
		case fi.Color && fi.Duplex:
			side = sp.ColorDuplex
		case fi.Color:
			side = sp.Color
		case fi.Duplex:
			side = sp.BWDuplex
		default:
			side = sp.BW
		}
		if side == 0 {
			if fi.Color && sp.Color == 0 {
				return nil, errColor
			}
			if fi.Duplex {
				return nil, errDuplex
			}
			return nil, errSize
		}
		sheets := pages
		if fi.Duplex {
			sheets = ceilHalf(pages)
		}
		line.Sheets = sheets
		sides += pages * fi.Copies
		if sides > maxSides {
			return nil, errTooMuch
		}
		printAmt := side * int64(pages) * int64(fi.Copies)
		line.Parts = append(line.Parts, Part{NameKK: svc.NameKK, NameRU: svc.NameRU, Amount: printAmt})
		line.Total = printAmt
		if fi.BindingID != 0 {
			b, ok := cat.Services[fi.BindingID]
			if !ok || !b.Active || !b.Online || b.Kind != "binding" {
				return nil, errService
			}
			var price int64
			for _, t := range b.Pricing.Tiers {
				if sheets <= t.MaxSheets {
					price = t.Price
					break
				}
			}
			if price == 0 {
				return nil, errBinding
			}
			amt := price * int64(fi.Copies)
			line.BindingID = b.ID
			line.Parts = append(line.Parts, Part{NameKK: b.NameKK, NameRU: b.NameRU, Amount: amt})
			line.Total += amt
		}
		if fi.LaminationID != 0 {
			l, ok := cat.Services[fi.LaminationID]
			if !ok || !l.Active || !l.Online || l.Kind != "lamination" {
				return nil, errService
			}
			per, ok := l.Pricing.Sheet[fi.Size]
			if !ok {
				return nil, errSize
			}
			amt := per * int64(sheets) * int64(fi.Copies)
			line.LaminationID = l.ID
			line.Parts = append(line.Parts, Part{NameKK: l.NameKK, NameRU: l.NameRU, Amount: amt})
			line.Total += amt
		}
		q.Lines = append(q.Lines, line)
		q.Total += line.Total
	}
	seenGoods := map[int64]bool{}
	for _, g := range in.Goods {
		it, ok := cat.Goods[g.ID]
		if !ok || it.Archived || !it.InStock || seenGoods[g.ID] {
			return nil, errGoods
		}
		seenGoods[g.ID] = true
		if g.Qty < 1 || g.Qty > maxQty {
			return nil, errTooMuch
		}
		amt := it.Price * int64(g.Qty)
		q.Lines = append(q.Lines, Line{Kind: "goods", RefID: it.ID, NameKK: it.NameKK, NameRU: it.NameRU, Qty: g.Qty, UnitPrice: it.Price, Total: amt})
		q.Total += amt
	}
	seenSvc := map[int64]bool{}
	for _, s := range in.Services {
		svc, ok := cat.Services[s.ID]
		if !ok || !svc.Active || !svc.Online || svc.Kind != "unit" || seenSvc[s.ID] {
			return nil, errService
		}
		seenSvc[s.ID] = true
		if s.Qty < 1 || s.Qty > 500 {
			return nil, errTooMuch
		}
		amt := svc.Pricing.Price * int64(s.Qty)
		q.Lines = append(q.Lines, Line{Kind: "service", RefID: svc.ID, NameKK: svc.NameKK, NameRU: svc.NameRU, Qty: s.Qty, UnitPrice: svc.Pricing.Price, Total: amt})
		q.Total += amt
	}
	if q.Total > 0 && q.Total < cat.MinOrder {
		gap := cat.MinOrder - q.Total
		q.Lines = append(q.Lines, Line{Kind: "minimum", NameKK: "Ең аз тапсырыс сомасына дейін", NameRU: "До минимальной суммы заказа", Total: gap})
		q.Total = cat.MinOrder
	}
	if q.Total <= 0 || (cat.MaxTotal > 0 && q.Total > cat.MaxTotal) {
		return nil, errTooMuch
	}
	return q, nil
}
