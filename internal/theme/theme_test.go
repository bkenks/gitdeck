package theme

import (
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestColorNames(t *testing.T) {
	if Color("green") != lipgloss.Green || Color("bright-blue") != lipgloss.BrightBlue || Color("Bright_Red") != lipgloss.BrightRed {
		t.Error("ANSI names should map to basic colors")
	}
	if Color("nope") != nil {
		t.Error("unknown names should be nil")
	}
	if Color("#ff0000") == nil || Color("208") == nil {
		t.Error("hex and indexed colors should parse")
	}
}

func TestDefaultsAreANSIOnly(t *testing.T) {
	th := Build(Defaults())
	for name, st := range map[string]lipgloss.Style{
		"active": th.ActiveBorder, "options": th.Options, "unstaged": th.Unstaged,
	} {
		if _, ok := st.GetForeground().(ansi.BasicColor); !ok {
			t.Errorf("%s border isn't a basic ANSI color: %T", name, st.GetForeground())
		}
	}
	if _, ok := th.Selected.GetBackground().(ansi.BasicColor); !ok {
		t.Errorf("selection background isn't a basic ANSI color")
	}
}

func TestMergeKeepsUnsetFields(t *testing.T) {
	c := Defaults().Merge(Config{ActiveBorderColor: []string{"magenta"}, Border: "single"})
	if c.ActiveBorderColor[0] != "magenta" || c.Border != "single" {
		t.Error("set fields should override")
	}
	if c.SelectedLineBgColor[0] != "blue" {
		t.Error("unset fields should keep defaults")
	}
}

func TestOverUsesDefaultForegroundOnSelection(t *testing.T) {
	sel := lipgloss.NewStyle().Background(lipgloss.Blue)
	got := Over(lipgloss.NewStyle().Foreground(lipgloss.Green).Faint(true), sel)
	if fg := got.GetForeground(); fg != nil {
		if _, none := fg.(lipgloss.NoColor); !none {
			t.Errorf("selected text should use the default foreground, got %v", fg)
		}
	}
	if got.GetBackground() != lipgloss.Blue || got.GetFaint() {
		t.Error("selection background should apply and faint should lift")
	}
	// A bold-only selection (inactive panels) keeps the row's colors.
	kept := Over(lipgloss.NewStyle().Foreground(lipgloss.Green), lipgloss.NewStyle().Bold(true))
	if kept.GetForeground() != lipgloss.Green || !kept.GetBold() {
		t.Error("bold-only selection should keep colors")
	}
}

func TestHue(t *testing.T) {
	th := Build(Defaults())
	cases := map[string]lipgloss.Style{
		"d73a4a": th.Red, "0e8a16": th.Green, "1d76db": th.Blue, "fbca04": th.Yellow,
		"ededed": th.Fg, "5319e7": th.Magenta,
	}
	for hex, want := range cases {
		if got := th.Hue(hex); got.GetForeground() != want.GetForeground() {
			t.Errorf("Hue(%s) = %v", hex, got.GetForeground())
		}
	}
}
