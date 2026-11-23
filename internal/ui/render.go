package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	. "github.com/bkenks/gitdeck/internal/theme"
)

// Seg is a run of text in one style. Rows are built from segs rather than
// pre-styled strings so a selection background can run unbroken across the
// whole row.
type Seg struct {
	Text  string
	Style lipgloss.Style
}

func S(text string, st lipgloss.Style) Seg { return Seg{text, st} }
func P(text string) Seg                    { return Seg{text, T.Fg} }

// Row is a list line: Left is truncated to fit, Right stays flush right.
type Row struct {
	Left, Right []Seg
}

func segsWidth(segs []Seg) int {
	w := 0
	for _, s := range segs {
		w += ansi.StringWidth(s.Text)
	}
	return w
}

// renderSegs draws segs into exactly width cells, truncating with an
// ellipsis. sel, when non-nil, is layered over every seg and the padding.
func renderSegs(segs []Seg, width int, sel *lipgloss.Style) string {
	var b strings.Builder
	left := width
	for i, s := range segs {
		if left <= 0 {
			break
		}
		text := s.Text
		w := ansi.StringWidth(text)
		if w > left || (w == left && i < len(segs)-1 && segsWidth(segs[i+1:]) > 0) {
			text = ansi.Truncate(text, left, "…")
			w = ansi.StringWidth(text)
		}
		st := s.Style
		if sel != nil {
			st = Over(st, *sel)
		}
		b.WriteString(st.Render(text))
		left -= w
	}
	if left > 0 {
		pad := lipgloss.NewStyle()
		if sel != nil {
			pad = Over(pad, *sel)
		}
		b.WriteString(pad.Render(strings.Repeat(" ", left)))
	}
	return b.String()
}

// renderRow draws a Row at width, with Right kept whole when there's room.
func renderRow(r Row, width int, sel *lipgloss.Style) string {
	rw := segsWidth(r.Right)
	if len(r.Right) == 0 || width < 16 {
		return renderSegs(r.Left, width, sel)
	}
	rw = min(rw, width*45/100)
	lw := width - rw - 1
	return renderSegs(r.Left, lw, sel) + renderSegs(append([]Seg{S(" ", T.Fg)}, r.Right...), rw+1, sel)
}

// fit pads or truncates an already-styled line to exactly width cells.
func fit(line string, width int) string {
	w := ansi.StringWidth(line)
	if w > width {
		return ansi.Truncate(line, width, "")
	}
	return line + strings.Repeat(" ", width-w)
}

// ─── Panels ─────────────────────────────────────────────────────────────────

// Panel is a lazygit-style box: index and tabs in the top border, a counter
// in the bottom border, and a scrollbar on the right edge.
type Panel struct {
	W, H      int
	Index     string // "1"; empty for none
	Tabs      []string
	Tab       int
	Title     string // used when there are no tabs
	Footer    string // bottom-right, e.g. "3 of 12"
	Status    string // bottom-left, e.g. the filter
	Focused   bool
	Searching bool
	Accent    *lipgloss.Style // overrides the focused border color
	Body      []string        // inner lines; fitted here

	Total, Offset int // for the scrollbar; Total 0 hides it
}

func (p Panel) Render() string {
	if p.W < 4 || p.H < 2 {
		return ""
	}
	b := T.Border
	active := T.ActiveBorder
	if p.Accent != nil {
		active = *p.Accent
	}
	border := T.InactiveBorder
	switch {
	case p.Searching:
		border = T.SearchingBorder
	case p.Focused:
		border = active
	}
	inner := p.W - 2
	rows := p.H - 2

	// Top: ╭─[1]─Tab - Tab────╮
	var title []Seg
	if p.Index != "" {
		title = append(title, S("["+p.Index+"]", border), S(b.Top, border))
	}
	if len(p.Tabs) > 0 {
		for i, t := range p.Tabs {
			if i > 0 {
				title = append(title, S(" - ", T.Fg))
			}
			st := T.Fg
			if i == p.Tab {
				if p.Focused {
					st = active
				} else {
					st = T.Bold
				}
			} else if len(p.Tabs) > 1 {
				st = T.Dim
			}
			title = append(title, S(t, st))
		}
	} else if p.Title != "" {
		st := T.Fg
		if p.Focused {
			st = active
		}
		title = append(title, S(p.Title, st))
	}
	var out strings.Builder
	out.WriteString(border.Render(b.TopLeft + b.Top))
	tw := min(segsWidth(title), inner-1)
	if tw > 0 {
		out.WriteString(renderSegs(title, tw, nil))
	}
	out.WriteString(border.Render(strings.Repeat(b.Top, max(0, inner-1-tw)) + b.TopRight))
	out.WriteByte('\n')

	// Scrollbar thumb.
	thumbStart, thumbLen := -1, 0
	if p.Total > rows && rows > 0 {
		thumbLen = max(1, rows*rows/p.Total)
		maxOff := p.Total - rows
		thumbStart = (rows - thumbLen) * min(p.Offset, maxOff) / max(1, maxOff)
	}
	for i := 0; i < rows; i++ {
		line := ""
		if i < len(p.Body) {
			line = p.Body[i]
		}
		right := b.Right
		if thumbStart >= 0 && i >= thumbStart && i < thumbStart+thumbLen {
			right = "▐"
		}
		out.WriteString(border.Render(b.Left))
		out.WriteString(fit(line, inner))
		out.WriteString(border.Render(right))
		out.WriteByte('\n')
	}

	// Bottom: ╰─ /filter ──── 3 of 12─╯
	status := ""
	if p.Status != "" {
		status = " " + p.Status + " "
	}
	footer := p.Footer
	sw, fw := ansi.StringWidth(status), ansi.StringWidth(footer)
	if sw+fw+2 > inner {
		footer, fw = "", 0
	}
	if sw+2 > inner {
		status, sw = ansi.Truncate(status, max(0, inner-2), "…"), min(sw, max(0, inner-2))
	}
	out.WriteString(border.Render(b.BottomLeft + b.Bottom))
	out.WriteString(status)
	out.WriteString(border.Render(strings.Repeat(b.Bottom, max(0, inner-1-sw-fw-1))))
	out.WriteString(border.Render(footer))
	out.WriteString(border.Render(b.Bottom + b.BottomRight))
	return out.String()
}

// ─── Lists ──────────────────────────────────────────────────────────────────

// list is the cursor, scroll and filter state of a selectable panel. view
// holds the indices of the underlying items that pass the filter.
type list struct {
	cursor, offset int
	filter         string
	view           []int
}

// refilter rebuilds view for n items. text gives each item's searchable text.
// The cursor stays on the same underlying item when it survives.
func (l *list) refilter(n int, text func(i int) string) {
	prev := l.sel()
	l.view = l.view[:0]
	q := strings.ToLower(strings.TrimSpace(l.filter))
	for i := 0; i < n; i++ {
		if q == "" || fuzzy(strings.ToLower(text(i)), q) {
			l.view = append(l.view, i)
		}
	}
	if prev >= 0 {
		for vi, i := range l.view {
			if i == prev {
				l.cursor = vi
				break
			}
		}
	}
	l.clamp()
}

// sel is the underlying index under the cursor, or -1.
func (l *list) sel() int {
	if l.cursor < 0 || l.cursor >= len(l.view) {
		return -1
	}
	return l.view[l.cursor]
}

func (l *list) len() int { return len(l.view) }

func (l *list) clamp() {
	if l.cursor >= len(l.view) {
		l.cursor = len(l.view) - 1
	}
	if l.cursor < 0 {
		l.cursor = 0
	}
}

func (l *list) move(d int) {
	l.cursor += d
	l.clamp()
}

func (l *list) reset() { l.cursor, l.offset = 0, 0 }

// selectIndex moves the cursor to underlying item i, if visible.
func (l *list) selectIndex(i int) bool {
	for vi, x := range l.view {
		if x == i {
			l.cursor = vi
			return true
		}
	}
	return false
}

// window scrolls so the cursor is visible in h rows and returns the visible
// range of view positions.
func (l *list) window(h int) (from, to int) {
	if h <= 0 {
		return 0, 0
	}
	if l.cursor < l.offset {
		l.offset = l.cursor
	}
	if l.cursor >= l.offset+h {
		l.offset = l.cursor - h + 1
	}
	if maxOff := len(l.view) - h; l.offset > maxOff {
		l.offset = max(0, maxOff)
	}
	return l.offset, min(len(l.view), l.offset+h)
}

// footer is lazygit's "3 of 12".
func (l *list) footer() string {
	if len(l.view) == 0 {
		return "0 of 0"
	}
	return itoa(l.cursor+1) + " of " + itoa(len(l.view))
}

// fuzzy matches when every rune of q appears in s, in order.
func fuzzy(s, q string) bool {
	if strings.Contains(s, q) {
		return true
	}
	qi := []rune(q)
	j := 0
	for _, r := range s {
		if j < len(qi) && r == qi[j] {
			j++
		}
	}
	return j == len(qi)
}

// renderList draws the visible rows of l at width × h.
func renderList(l *list, width, h int, focused bool, row func(i int) Row) []string {
	from, to := l.window(h)
	lines := make([]string, 0, h)
	for vi := from; vi < to; vi++ {
		var sel *lipgloss.Style
		if vi == l.cursor {
			if focused {
				sel = &T.Selected
			} else {
				sel = &T.InactiveSel
			}
		}
		lines = append(lines, renderRow(row(l.view[vi]), width, sel))
	}
	return lines
}

// ─── Scrollable text ────────────────────────────────────────────────────────

// pager is the main panel's scroll state over pre-rendered lines.
type pager struct {
	lines  []string
	offset int
}

func (p *pager) set(lines []string, keepOffset bool) {
	p.lines = lines
	if !keepOffset {
		p.offset = 0
	}
}

func (p *pager) scroll(d, h int) {
	p.offset += d
	if maxOff := len(p.lines) - h; p.offset > maxOff {
		p.offset = max(0, maxOff)
	}
	if p.offset < 0 {
		p.offset = 0
	}
}

func (p *pager) view(h int) []string {
	p.scroll(0, h)
	end := min(len(p.lines), p.offset+h)
	if p.offset >= end {
		return nil
	}
	return p.lines[p.offset:end]
}
