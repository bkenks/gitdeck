package ui

import (
	"slices"
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
	left, right, thumb := border.Render(b.Left), border.Render(b.Right), border.Render("▐")
	for i := 0; i < rows; i++ {
		line := ""
		if i < len(p.Body) {
			line = p.Body[i]
		}
		out.WriteString(left)
		out.WriteString(fit(line, inner))
		if thumbStart >= 0 && i >= thumbStart && i < thumbStart+thumbLen {
			out.WriteString(thumb)
		} else {
			out.WriteString(right)
		}
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

// item is one entry behind a list: id keeps the cursor on it across
// reloads and re-sorts, and text is what the filter matches.
type item struct {
	id, text string
}

// list is the cursor, scroll and filter state of a selectable panel over
// the items source returns. view holds the indices of the items that pass
// the filter, and ids their ids, as of the last refilter.
type list struct {
	source func() []item
	// prefiltered lists show every item because source applies the filter
	// itself: the files tree filters paths before it builds its rows.
	prefiltered    bool
	cursor, offset int
	filter         string
	view           []int
	ids            []string
}

// matches reports whether text passes the filter.
func (l *list) matches(text string) bool {
	q := strings.ToLower(strings.TrimSpace(l.filter))
	return q == "" || fuzzy(strings.ToLower(text), q)
}

// refilter rebuilds view from source. The cursor stays on the same item
// when it survives, and holds its place when it doesn't.
func (l *list) refilter() {
	prev := l.selectedID()
	l.view, l.ids = l.view[:0], l.ids[:0]
	for i, it := range l.source() {
		if l.prefiltered || l.matches(it.text) {
			l.view = append(l.view, i)
			l.ids = append(l.ids, it.id)
		}
	}
	if prev != "" {
		l.selectID(prev)
	}
	l.clamp()
}

// setFilter replaces the filter and refilters from the top: the cursor
// stays on the first item showing before, if it still matches.
func (l *list) setFilter(q string) {
	l.filter, l.cursor = q, 0
	l.refilter()
}

// sel is the underlying index under the cursor, or -1.
func (l *list) sel() int {
	if l.cursor < 0 || l.cursor >= len(l.view) {
		return -1
	}
	return l.view[l.cursor]
}

// selectedID is the id of the item under the cursor, or "".
func (l *list) selectedID() string {
	if l.cursor < 0 || l.cursor >= len(l.ids) {
		return ""
	}
	return l.ids[l.cursor]
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

// reset starts the list over: unfiltered, at the top, with no selection
// for the next refilter to keep.
func (l *list) reset() {
	l.cursor, l.offset, l.filter = 0, 0, ""
	l.view, l.ids = l.view[:0], l.ids[:0]
}

// selectID moves the cursor to the item with id, if it's showing.
func (l *list) selectID(id string) bool {
	i := slices.Index(l.ids, id)
	if i >= 0 {
		l.cursor = i
	}
	return i >= 0
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

// listChrome fills a panel's counter, filter status and scrollbar from l.
func (a *App) listChrome(pn *Panel, p pane, l *list) {
	pn.Footer, pn.Status = l.footer(), a.filterStatus(p, l)
	pn.Total, pn.Offset = l.len(), l.offset
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

// percent is the "42%" scroll position once the lines overflow h rows.
func (p *pager) percent(h int) string {
	if len(p.lines) <= h || h <= 0 {
		return ""
	}
	return itoa(min(100, (p.offset+h)*100/len(p.lines))) + "%"
}

// indent prefixes a copy of lines with one space each.
func indent(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = " " + l
	}
	return out
}

func (p *pager) view(h int) []string {
	p.scroll(0, h)
	end := min(len(p.lines), p.offset+h)
	if p.offset >= end {
		return nil
	}
	return p.lines[p.offset:end]
}
