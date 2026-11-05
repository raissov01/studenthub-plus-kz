// Package htmlx has the few DOM helpers the feed readers need on top of golang.org/x/net/html:
// class tests, text with line breaks, and a tiny selector language for per-source HTML lists.
package htmlx

import (
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Parse parses a document.
func Parse(b []byte) (*html.Node, error) { return html.Parse(strings.NewReader(string(b))) }

// Attr returns the attribute value or "".
func Attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

// HasClass reports whether the element has the class.
func HasClass(n *html.Node, class string) bool {
	if n.Type != html.ElementNode {
		return false
	}
	for _, c := range strings.Fields(Attr(n, "class")) {
		if c == class {
			return true
		}
	}
	return false
}

// FindAll returns every descendant element (not n itself) matching pred, in document order.
// With skip, a matching subtree is not searched further.
func FindAll(n *html.Node, pred func(*html.Node) bool, skip bool) []*html.Node {
	var out []*html.Node
	var walk func(*html.Node)
	walk = func(x *html.Node) {
		for c := x.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && pred(c) {
				out = append(out, c)
				if skip {
					continue
				}
			}
			walk(c)
		}
	}
	walk(n)
	return out
}

// First returns the first descendant matching pred, or nil.
func First(n *html.Node, pred func(*html.Node) bool) *html.Node {
	if all := FindAll(n, pred, true); len(all) > 0 {
		return all[0]
	}
	return nil
}

// ByClass matches elements with the class.
func ByClass(class string) func(*html.Node) bool {
	return func(n *html.Node) bool { return HasClass(n, class) }
}

var blocks = map[atom.Atom]bool{atom.P: true, atom.Div: true, atom.Li: true, atom.H1: true, atom.H2: true, atom.H3: true,
	atom.H4: true, atom.Tr: true, atom.Blockquote: true, atom.Section: true, atom.Article: true}

// Text returns the visible text: <br> and block ends become newlines, runs of spaces collapse,
// script/style are dropped.
func Text(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(x *html.Node) {
		switch x.Type {
		case html.TextNode:
			b.WriteString(x.Data)
			return
		case html.ElementNode:
			switch x.DataAtom {
			case atom.Script, atom.Style, atom.Noscript, atom.Template:
				return
			case atom.Br:
				b.WriteString("\n")
				return
			}
		}
		for c := x.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
		if x.Type == html.ElementNode && blocks[x.DataAtom] {
			b.WriteString("\n")
		}
	}
	walk(n)
	return Clean(b.String())
}

// Clean collapses spaces inside lines and blank lines between them.
func Clean(s string) string {
	lines := strings.Split(strings.ReplaceAll(s, " ", " "), "\n")
	out := make([]string, 0, len(lines))
	blank := false
	for _, l := range lines {
		l = strings.Join(strings.Fields(l), " ")
		if l == "" {
			if !blank && len(out) > 0 {
				out = append(out, "")
			}
			blank = true
			continue
		}
		blank = false
		out = append(out, l)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// Links returns the href of every <a> under n, in order, without duplicates.
func Links(n *html.Node) []string {
	seen := map[string]bool{}
	var out []string
	for _, a := range FindAll(n, func(x *html.Node) bool { return x.DataAtom == atom.A }, false) {
		h := strings.TrimSpace(Attr(a, "href"))
		if h != "" && !seen[h] {
			seen[h] = true
			out = append(out, h)
		}
	}
	return out
}

// ---------- selectors ----------

// compound is one step: tag, classes, an id and attributes that must be present.
type compound struct {
	tag     string
	id      string
	classes []string
	attrs   []string
}

func parseCompound(s string) compound {
	var c compound
	for s != "" {
		switch s[0] {
		case '.':
			end := nextSep(s[1:]) + 1
			c.classes = append(c.classes, s[1:end])
			s = s[end:]
		case '#':
			end := nextSep(s[1:]) + 1
			c.id = s[1:end]
			s = s[end:]
		case '[':
			end := strings.IndexByte(s, ']')
			if end < 0 {
				end = len(s) - 1
			}
			c.attrs = append(c.attrs, s[1:end])
			s = s[end+1:]
		default:
			end := nextSep(s)
			c.tag = strings.ToLower(s[:end])
			s = s[end:]
		}
	}
	return c
}

func nextSep(s string) int {
	if i := strings.IndexAny(s, ".#["); i >= 0 {
		return i
	}
	return len(s)
}

func (c compound) match(n *html.Node) bool {
	if n.Type != html.ElementNode || (c.tag != "" && c.tag != "*" && n.Data != c.tag) {
		return false
	}
	if c.id != "" && Attr(n, "id") != c.id {
		return false
	}
	for _, cl := range c.classes {
		if !HasClass(n, cl) {
			return false
		}
	}
	for _, a := range c.attrs {
		found := false
		for _, x := range n.Attr {
			if x.Key == a {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// Select returns the elements under root matching a selector: compounds (tag, .class, #id,
// [attr], combined like "div.card[data-id]") separated by spaces (descendant). Commas give
// alternatives. That is all the per-source config may use.
func Select(root *html.Node, selector string) []*html.Node {
	var out []*html.Node
	seen := map[*html.Node]bool{}
	for _, alt := range strings.Split(selector, ",") {
		steps := strings.Fields(alt)
		if len(steps) == 0 {
			continue
		}
		cur := []*html.Node{root}
		for _, st := range steps {
			c := parseCompound(st)
			var next []*html.Node
			inNext := map[*html.Node]bool{}
			for _, n := range cur {
				for _, m := range FindAll(n, c.match, false) {
					if !inNext[m] {
						inNext[m] = true
						next = append(next, m)
					}
				}
			}
			cur = next
		}
		for _, n := range cur {
			if !seen[n] {
				seen[n] = true
				out = append(out, n)
			}
		}
	}
	return out
}

// SelectFirst is the first match or nil.
func SelectFirst(root *html.Node, selector string) *html.Node {
	if all := Select(root, selector); len(all) > 0 {
		return all[0]
	}
	return nil
}
