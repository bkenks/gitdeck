package ui

import (
	"slices"

	tea "charm.land/bubbletea/v2"
)

// paneNav is how a mode's panes answer the navigation keys every mode
// shares. The detail pane is 0 in both.
type paneNav struct {
	lists []pane // focused by 1, 2, … and cycled through by h/l, in order
	// enterOpensDetail makes enter on a list focus the detail pane. Local
	// lists have enter actions of their own.
	enterOpensDetail bool
}

var (
	localNav = paneNav{lists: []pane{pRepos, pFiles, pBranches, pCommits, pStash}}
	ghNav    = paneNav{lists: []pane{pPRs, pIssues}, enterOpensDetail: true}
)

func (a *App) paneNav() paneNav {
	if a.mode == modeGitHub {
		return ghNav
	}
	return localNav
}

// paneKey handles a key meant for the focused pane: the shared navigation
// first, then the mode's own actions, then scrolling whatever has focus.
func (a *App) paneKey(key string) (bool, tea.Cmd) {
	if handled, c := a.navigate(key); handled {
		return true, c
	}
	var handled bool
	var c tea.Cmd
	if a.mode == modeLocal {
		handled, c = a.localKey(key)
	} else {
		handled, c = a.ghKey(key)
	}
	if handled {
		return true, c
	}
	return a.scrollKey(key)
}

// navigate handles the focus and filter keys every mode shares.
func (a *App) navigate(key string) (bool, tea.Cmd) {
	lists := a.paneNav().lists
	switch key {
	case "0":
		a.setFocus(pDetail)
		return true, nil
	case "enter":
		return a.enterDetail()
	case "h", "left":
		return true, a.cycleFocus(lists, -1)
	case "l", "right":
		return true, a.cycleFocus(lists, 1)
	case "/":
		a.startFilter()
		return true, nil
	case "esc":
		return true, a.escape()
	}
	return a.focusDigit(key, lists)
}

// enterDetail focuses the detail pane from a list whose mode lets enter do
// that.
func (a *App) enterDetail() (bool, tea.Cmd) {
	if !a.paneNav().enterOpensDetail || a.focus == pDetail {
		return false, nil
	}
	a.setFocus(pDetail)
	return true, nil
}

// focusDigit focuses the list a digit key names, 1 being the first.
func (a *App) focusDigit(key string, lists []pane) (bool, tea.Cmd) {
	if len(key) != 1 || key[0] < '1' || int(key[0]-'1') >= len(lists) {
		return false, nil
	}
	a.setFocus(lists[key[0]-'1'])
	return true, a.afterMove(a.focus)
}

// cycleFocus moves focus d lists along, wrapping around. From the detail
// pane it goes back to the list the detail shows instead, and from a pane
// that isn't in lists to the first list.
func (a *App) cycleFocus(lists []pane, d int) tea.Cmd {
	switch i := slices.Index(lists, a.focus); {
	case a.focus == pDetail:
		a.focus = a.back
	case i < 0:
		a.focus = lists[0]
	default:
		a.focus = lists[(i+d+len(lists))%len(lists)]
	}
	return a.afterMove(a.focus)
}

// escape leaves the detail pane, or clears the focused list's filter.
func (a *App) escape() tea.Cmd {
	if a.focus == pDetail {
		a.focus = a.back
		return nil
	}
	if a.filterText() != "" {
		a.setFilter("")
	}
	return a.afterMove(a.focus)
}

// scrollKey scrolls the detail pane or moves the focused list's cursor.
func (a *App) scrollKey(key string) (bool, tea.Cmd) {
	if a.focus == pDetail {
		return pagerKey(key, a.detailPager(), a.panelRows(pDetail)), nil
	}
	if l := a.listFor(a.focus); l != nil && navKey(key, l, a.panelRows(a.focus)) {
		return true, a.afterMove(a.focus)
	}
	return false, nil
}

// panelRows is how many rows of content pane p had in the last render.
func (a *App) panelRows(p pane) int {
	for _, r := range a.rects {
		if r.p == p {
			return r.h - 2
		}
	}
	return 10
}

// navKey handles movement shared by every list panel.
func navKey(key string, l *list, h int) bool {
	page := max(1, h-2)
	switch key {
	case "j", "down":
		l.move(1)
	case "k", "up":
		l.move(-1)
	case "ctrl+d", "pgdown":
		l.move(page / 2)
	case "ctrl+u", "pgup":
		l.move(-page / 2)
	case "g", "home":
		l.cursor = 0
	case "G", "end":
		l.cursor = l.len() - 1
		l.clamp()
	default:
		return false
	}
	return true
}

// pagerKey scrolls a pager h rows tall.
func pagerKey(key string, p *pager, h int) bool {
	switch key {
	case "j", "down":
		p.scroll(1, h)
	case "k", "up":
		p.scroll(-1, h)
	case "ctrl+d", "pgdown", "space":
		p.scroll(h/2, h)
	case "ctrl+u", "pgup":
		p.scroll(-h/2, h)
	case "g", "home":
		p.offset = 0
	case "G", "end":
		p.scroll(len(p.lines), h)
	default:
		return false
	}
	return true
}
