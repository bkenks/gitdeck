package ui

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"charm.land/glamour/v2"
	gansi "charm.land/glamour/v2/ansi"
	"github.com/charmbracelet/x/ansi"

	. "github.com/bkenks/gitdeck/internal/theme"
)

func itoa(n int) string { return strconv.Itoa(n) }

// ago is lazygit's compact recency: 5m, 2h, 3d, 2w, 4mo, 1y.
func ago(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 14*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d < 60*24*time.Hour:
		return fmt.Sprintf("%dw", int(d.Hours()/24/7))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dmo", int(d.Hours()/24/30))
	default:
		return fmt.Sprintf("%dy", int(d.Hours()/24/365))
	}
}

func agoLong(t time.Time) string {
	s := ago(t)
	if s == "now" || s == "" {
		return "just now"
	}
	return s + " ago"
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return itoa(n) + " " + word + "s"
}

// initials turns "Brian Kenkel" into "BK", as lazygit shows authors.
func initials(name string) string {
	f := strings.Fields(name)
	switch len(f) {
	case 0:
		return "??"
	case 1:
		r := []rune(f[0])
		if len(r) >= 2 {
			return strings.ToUpper(string(r[:2]))
		}
		return strings.ToUpper(string(r))
	default:
		a, b := []rune(f[0]), []rune(f[len(f)-1])
		return strings.ToUpper(string(a[0]) + string(b[0]))
	}
}

// ─── Diffs ──────────────────────────────────────────────────────────────────

// colorDiff styles plain `git diff`/`git show` output with ANSI colors.
func colorDiff(s string) []string {
	s = strings.ReplaceAll(strings.TrimRight(s, "\n"), "\t", "    ")
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	out := make([]string, len(lines))
	inHeader := false
	for i, l := range lines {
		switch {
		case strings.HasPrefix(l, "commit "):
			out[i] = T.Yellow.Render(l)
		case strings.HasPrefix(l, "diff --git"), strings.HasPrefix(l, "diff --cc"):
			inHeader = true
			out[i] = T.Bold.Render(l)
		case inHeader && (strings.HasPrefix(l, "index ") || strings.HasPrefix(l, "new file") ||
			strings.HasPrefix(l, "deleted file") || strings.HasPrefix(l, "similarity") ||
			strings.HasPrefix(l, "rename ") || strings.HasPrefix(l, "old mode") ||
			strings.HasPrefix(l, "new mode") || strings.HasPrefix(l, "--- ") ||
			strings.HasPrefix(l, "+++ ") || strings.HasPrefix(l, "Binary files")):
			out[i] = T.Bold.Render(l)
		case strings.HasPrefix(l, "@@"):
			inHeader = false
			if j := strings.Index(l[2:], "@@"); j >= 0 {
				out[i] = T.Cyan.Render(l[:j+4]) + l[j+4:]
			} else {
				out[i] = T.Cyan.Render(l)
			}
		case strings.HasPrefix(l, "+"):
			out[i] = T.Green.Render(l)
		case strings.HasPrefix(l, "-"):
			out[i] = T.Red.Render(l)
		case strings.HasPrefix(l, `\ No newline`):
			out[i] = T.Dim.Render(l)
		case strings.Contains(l, " | ") && (strings.HasSuffix(l, "+") || strings.HasSuffix(l, "-") || strings.Contains(l, "Bin ")):
			// --stat line: color the +++--- graph.
			j := strings.LastIndex(l, " ")
			graph := l[j+1:]
			g := strings.ReplaceAll(graph, "+", "\x00")
			g = strings.ReplaceAll(g, "-", "\x01")
			g = strings.ReplaceAll(g, "\x00", T.Green.Render("+"))
			g = strings.ReplaceAll(g, "\x01", T.Red.Render("-"))
			out[i] = l[:j+1] + g
		default:
			out[i] = l
		}
	}
	return out
}

// splitANSI splits already-colored output (git log --graph) into lines.
func splitANSI(s string) []string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(strings.ReplaceAll(s, "\t", "    "), "\n")
}

// wrap hard-wraps plain or styled text to width.
func wrap(s string, width int) []string {
	if width < 4 {
		width = 4
	}
	return strings.Split(ansi.Wrap(s, width, " "), "\n")
}

// ─── Markdown ───────────────────────────────────────────────────────────────

func sp(s string) *string { return &s }
func bp(b bool) *bool     { return &b }
func up(u uint) *uint     { return &u }

// mdStyle is a glamour style built only from ANSI color slots, so rendered
// PR bodies follow the terminal theme like everything else.
func mdStyle() gansi.StyleConfig {
	return gansi.StyleConfig{
		Document:   gansi.StyleBlock{Margin: up(0)},
		BlockQuote: gansi.StyleBlock{StylePrimitive: gansi.StylePrimitive{Faint: bp(true)}, Indent: up(1), IndentToken: sp("│ ")},
		Paragraph:  gansi.StyleBlock{},
		List:       gansi.StyleList{LevelIndent: 2},
		Heading: gansi.StyleBlock{StylePrimitive: gansi.StylePrimitive{
			BlockSuffix: "\n", Color: sp("4"), Bold: bp(true),
		}},
		H1:            gansi.StyleBlock{StylePrimitive: gansi.StylePrimitive{Prefix: "# ", Color: sp("5")}},
		H2:            gansi.StyleBlock{StylePrimitive: gansi.StylePrimitive{Prefix: "## "}},
		H3:            gansi.StyleBlock{StylePrimitive: gansi.StylePrimitive{Prefix: "### "}},
		H4:            gansi.StyleBlock{StylePrimitive: gansi.StylePrimitive{Prefix: "#### "}},
		H5:            gansi.StyleBlock{StylePrimitive: gansi.StylePrimitive{Prefix: "##### "}},
		H6:            gansi.StyleBlock{StylePrimitive: gansi.StylePrimitive{Prefix: "###### ", Bold: bp(false)}},
		Strikethrough: gansi.StylePrimitive{CrossedOut: bp(true)},
		Emph:          gansi.StylePrimitive{Italic: bp(true)},
		Strong:        gansi.StylePrimitive{Bold: bp(true)},
		HorizontalRule: gansi.StylePrimitive{
			Faint: bp(true), Format: "\n────────\n",
		},
		Item:        gansi.StylePrimitive{BlockPrefix: "• "},
		Enumeration: gansi.StylePrimitive{BlockPrefix: ". "},
		Task: gansi.StyleTask{
			Ticked:   "[✓] ",
			Unticked: "[ ] ",
		},
		Link:      gansi.StylePrimitive{Color: sp("4"), Underline: bp(true)},
		LinkText:  gansi.StylePrimitive{Color: sp("6"), Bold: bp(true)},
		Image:     gansi.StylePrimitive{Color: sp("5"), Underline: bp(true)},
		ImageText: gansi.StylePrimitive{Faint: bp(true), Format: "Image: {{.text}} →"},
		Code:      gansi.StyleBlock{StylePrimitive: gansi.StylePrimitive{Color: sp("3")}},
		CodeBlock: gansi.StyleCodeBlock{
			StyleBlock: gansi.StyleBlock{Margin: up(2)},
			Chroma: &gansi.Chroma{
				Text:                gansi.StylePrimitive{},
				Error:               gansi.StylePrimitive{Color: sp("#cd0000")},
				Comment:             gansi.StylePrimitive{Color: sp("#7f7f7f")},
				CommentPreproc:      gansi.StylePrimitive{Color: sp("#cd00cd")},
				Keyword:             gansi.StylePrimitive{Color: sp("#cd00cd")},
				KeywordReserved:     gansi.StylePrimitive{Color: sp("#cd00cd")},
				KeywordNamespace:    gansi.StylePrimitive{Color: sp("#cd00cd")},
				KeywordType:         gansi.StylePrimitive{Color: sp("#cdcd00")},
				Operator:            gansi.StylePrimitive{Color: sp("#00cdcd")},
				Punctuation:         gansi.StylePrimitive{},
				Name:                gansi.StylePrimitive{},
				NameBuiltin:         gansi.StylePrimitive{Color: sp("#00cdcd")},
				NameTag:             gansi.StylePrimitive{Color: sp("#0000ee")},
				NameAttribute:       gansi.StylePrimitive{Color: sp("#00cd00")},
				NameClass:           gansi.StylePrimitive{Color: sp("#cdcd00"), Bold: bp(true)},
				NameDecorator:       gansi.StylePrimitive{Color: sp("#cdcd00")},
				NameFunction:        gansi.StylePrimitive{Color: sp("#0000ee")},
				LiteralNumber:       gansi.StylePrimitive{Color: sp("#cdcd00")},
				LiteralString:       gansi.StylePrimitive{Color: sp("#00cd00")},
				LiteralStringEscape: gansi.StylePrimitive{Color: sp("#00cdcd")},
				GenericDeleted:      gansi.StylePrimitive{Color: sp("#cd0000")},
				GenericEmph:         gansi.StylePrimitive{Italic: bp(true)},
				GenericInserted:     gansi.StylePrimitive{Color: sp("#00cd00")},
				GenericStrong:       gansi.StylePrimitive{Bold: bp(true)},
				GenericSubheading:   gansi.StylePrimitive{Color: sp("#00cdcd")},
			},
		},
		Table: gansi.StyleTable{
			CenterSeparator: sp("┼"), ColumnSeparator: sp("│"), RowSeparator: sp("─"),
		},
		DefinitionDescription: gansi.StylePrimitive{BlockPrefix: "\n→ "},
	}
}

var (
	mdMu        sync.Mutex
	mdRenderers = map[int]*glamour.TermRenderer{}
)

// markdown renders GitHub markdown at width. Chroma's terminal16 formatter
// snaps code colors to the 16 ANSI slots.
func markdown(src string, width int) []string {
	src = strings.TrimSpace(strings.ReplaceAll(src, "\r\n", "\n"))
	if src == "" {
		return []string{T.Dim.Render("No description provided.")}
	}
	mdMu.Lock()
	r, ok := mdRenderers[width]
	if !ok {
		var err error
		r, err = glamour.NewTermRenderer(
			glamour.WithStyles(mdStyle()),
			glamour.WithWordWrap(width),
			glamour.WithChromaFormatter("terminal16"),
			glamour.WithEmoji(),
		)
		if err != nil {
			mdMu.Unlock()
			return wrap(src, width)
		}
		mdRenderers[width] = r
	}
	out, err := r.Render(src)
	mdMu.Unlock()
	if err != nil {
		return wrap(src, width)
	}
	lines := strings.Split(strings.Trim(out, "\n"), "\n")
	// Glamour pads every line to the wrap width; trailing spaces would only
	// fight the panel's own padding.
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}
	return trimBlankRuns(lines)
}

// trimBlankRuns collapses runs of blank lines to one.
func trimBlankRuns(lines []string) []string {
	out := lines[:0]
	blank := false
	for _, l := range lines {
		isBlank := strings.TrimSpace(ansi.Strip(l)) == ""
		if isBlank && blank {
			continue
		}
		blank = isBlank
		out = append(out, l)
	}
	return out
}
