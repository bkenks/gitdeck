package ui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	. "github.com/bkenks/gitdeck/internal/theme"
)

// modal is a popup drawn over the layout. update returns done=true to close.
type modal interface {
	update(msg tea.Msg, a *App) (done bool, cmd tea.Cmd)
	view(a *App) string
}

func modalWidth(a *App, want int) int {
	return max(30, min(want, a.w-6))
}

// box draws a modal frame in the active border color with a key hint
// under the content.
func box(w int, title string, body []string, hint string, accent *lipgloss.Style) string {
	inner := w - 4
	lines := []string{""}
	for _, l := range body {
		for _, wl := range wrap(l, inner) {
			lines = append(lines, "  "+wl)
		}
	}
	lines = append(lines, "")
	if hint != "" {
		lines = append(lines, "  "+hint)
	}
	return Panel{W: w, H: len(lines) + 2, Title: title, Focused: true, Body: lines, Accent: accent}.Render()
}

func keyHint(pairs ...string) string {
	var parts []string
	for i := 0; i+1 < len(pairs); i += 2 {
		parts = append(parts, T.Options.Render(pairs[i+1]+": ")+pairs[i])
	}
	return strings.Join(parts, T.Dim.Render(" | "))
}

// ─── Confirm ────────────────────────────────────────────────────────────────

type confirmModal struct {
	title  string
	body   []string
	onYes  tea.Cmd
	danger bool
}

func (a *App) confirm(title string, body []string, onYes tea.Cmd) {
	a.modal = &confirmModal{title: title, body: body, onYes: onYes}
}

// confirmDanger is confirm in the red of something that can't be undone.
func (a *App) confirmDanger(title string, body []string, onYes tea.Cmd) {
	a.modal = &confirmModal{title: title, body: body, onYes: onYes, danger: true}
}

func (m *confirmModal) update(msg tea.Msg, a *App) (bool, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return false, nil
	}
	switch k.String() {
	case "y", "Y", "enter":
		return true, m.onYes
	case "n", "N", "esc", "q", "ctrl+c":
		return true, nil
	}
	return false, nil
}

func (m *confirmModal) view(a *App) string {
	var accent *lipgloss.Style
	if m.danger {
		s := T.Red.Bold(true)
		accent = &s
	}
	return box(modalWidth(a, 60), m.title, m.body, keyHint("y", "Confirm", "esc", "Cancel"), accent)
}

// ─── Message ────────────────────────────────────────────────────────────────

type messageModal struct {
	title string
	body  []string
}

func newMessage(title string, body []string) *messageModal { return &messageModal{title, body} }

func (m *messageModal) update(msg tea.Msg, a *App) (bool, tea.Cmd) {
	_, ok := msg.(tea.KeyPressMsg)
	return ok, nil
}

func (m *messageModal) view(a *App) string {
	return box(modalWidth(a, 60), m.title, m.body, keyHint("any key", "Close"), nil)
}

// ─── Input ──────────────────────────────────────────────────────────────────

func inputStyles() textinput.Styles {
	st := textinput.Styles{
		Focused: textinput.StyleState{
			Prompt:      T.ActiveBorder,
			Text:        T.Fg,
			Placeholder: T.Dim,
			Suggestion:  T.Dim,
		},
		Cursor: textinput.CursorStyle{Color: lipgloss.NoColor{}, Shape: tea.CursorBlock, Blink: true},
	}
	st.Blurred = st.Focused
	return st
}

type inputModal struct {
	title, help string
	in          textinput.Model
	submit      func(string) tea.Cmd
}

func newInput(title, help, value string, submit func(string) tea.Cmd) *inputModal {
	in := textinput.New()
	in.Prompt = "❯ "
	in.SetStyles(inputStyles())
	in.SetVirtualCursor(true)
	in.SetValue(value)
	in.CursorEnd()
	in.Focus()
	return &inputModal{title: title, help: help, in: in, submit: submit}
}

func (m *inputModal) update(msg tea.Msg, a *App) (bool, tea.Cmd) {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		switch k.String() {
		case "enter":
			return true, m.submit(m.in.Value())
		case "esc", "ctrl+c":
			return true, nil
		}
	}
	var c tea.Cmd
	m.in, c = m.in.Update(msg)
	return false, c
}

func (m *inputModal) view(a *App) string {
	w := modalWidth(a, 72)
	m.in.SetWidth(w - 8)
	body := []string{}
	if m.help != "" {
		body = append(body, T.Dim.Render(m.help), "")
	}
	body = append(body, m.in.View())
	return box(w, m.title, body, keyHint("enter", "Confirm", "esc", "Cancel"), nil)
}

// ─── Text area ──────────────────────────────────────────────────────────────

type textAreaModal struct {
	title, help string
	ta          textarea.Model
	submit      func(string) tea.Cmd
}

func newTextArea(title, help, value string, submit func(string) tea.Cmd) *textAreaModal {
	ta := textarea.New()
	ta.ShowLineNumbers = false
	ta.Prompt = "┃ "
	ta.CharLimit = 0
	ta.SetHeight(6)
	ta.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("shift+enter", "alt+enter", "ctrl+j"))
	ta.SetVirtualCursor(true)
	st := textarea.StyleState{
		Base:        lipgloss.NewStyle(),
		Text:        T.Fg,
		Placeholder: T.Dim,
		Prompt:      T.ActiveBorder,
		EndOfBuffer: T.Dim,
		CursorLine:  lipgloss.NewStyle(),
	}
	ta.SetStyles(textarea.Styles{
		Focused: st, Blurred: st,
		Cursor: textarea.CursorStyle{Color: lipgloss.NoColor{}, Shape: tea.CursorBlock, Blink: true},
	})
	ta.SetValue(value)
	ta.Focus()
	return &textAreaModal{title: title, help: help, ta: ta, submit: submit}
}

func (m *textAreaModal) update(msg tea.Msg, a *App) (bool, tea.Cmd) {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		switch k.String() {
		case "enter", "ctrl+s":
			return true, m.submit(m.ta.Value())
		case "esc", "ctrl+c":
			return true, nil
		}
	}
	var c tea.Cmd
	m.ta, c = m.ta.Update(msg)
	return false, c
}

func (m *textAreaModal) view(a *App) string {
	w := modalWidth(a, 76)
	m.ta.SetWidth(w - 6)
	body := []string{}
	if m.help != "" {
		body = append(body, T.Dim.Render(m.help), "")
	}
	body = append(body, strings.Split(m.ta.View(), "\n")...)
	summary := strings.SplitN(m.ta.Value(), "\n", 2)[0]
	title := m.title
	if n := len([]rune(summary)); n > 0 {
		st := T.Dim
		if n > 72 {
			st = T.Yellow
		}
		title += " " + st.Render(itoa(n))
	}
	return box(w, title, body, keyHint("enter", "Confirm", "shift+enter", "New line", "esc", "Cancel"), nil)
}

// ─── Menu ───────────────────────────────────────────────────────────────────

type menuItem struct {
	key, label string
	run        func() tea.Cmd
}

type menuModal struct {
	title  string
	items  []menuItem
	cursor int
}

func (m *menuModal) update(msg tea.Msg, a *App) (bool, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return false, nil
	}
	switch k.String() {
	case "j", "down":
		m.cursor = min(m.cursor+1, len(m.items)-1)
	case "k", "up":
		m.cursor = max(m.cursor-1, 0)
	case "enter":
		return true, m.items[m.cursor].run()
	case "esc", "q", "ctrl+c":
		return true, nil
	default:
		for _, it := range m.items {
			if it.key == k.String() {
				return true, it.run()
			}
		}
	}
	return false, nil
}

func (m *menuModal) view(a *App) string {
	w := modalWidth(a, 56)
	var body []string
	for i, it := range m.items {
		row := Row{Left: []Seg{S(it.label, T.Fg)}, Right: []Seg{S(it.key, T.Options)}}
		var sel *lipgloss.Style
		if i == m.cursor {
			sel = &T.Selected
		}
		body = append(body, renderRow(row, w-4, sel))
	}
	lines := append([]string{""}, body...)
	lines = indent(append(lines, "", keyHint("enter", "Run", "esc", "Cancel")))
	return Panel{W: w, H: len(lines) + 2, Title: m.title, Focused: true, Body: lines}.Render()
}

// ─── Help ───────────────────────────────────────────────────────────────────

type helpModal struct {
	lines []string
	p     pager
}

func (m *helpModal) update(msg tea.Msg, a *App) (bool, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return false, nil
	}
	switch k.String() {
	case "esc", "q", "?", "ctrl+c":
		return true, nil
	}
	pagerKey(k.String(), &m.p, a.h-8)
	return false, nil
}

func (m *helpModal) view(a *App) string {
	w := modalWidth(a, 70)
	h := min(len(m.lines)+2, a.h-6)
	m.p.lines = m.lines
	return Panel{W: w, H: h, Title: "Keybindings", Focused: true, Body: m.p.view(h - 2),
		Total: len(m.lines), Offset: m.p.offset, Footer: "esc"}.Render()
}
