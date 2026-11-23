// Package theme builds every style gitdeck draws with.
//
// Nothing here picks an RGB value. Colors are the terminal's own 16 ANSI
// slots (red, green, blue, ...) plus its default foreground and background,
// so the terminal's theme decides what they look like. Switch the terminal
// between light and dark and gitdeck follows instantly, the same way lazygit
// does. Hex colors are still accepted in the config for people who want them.
package theme

import (
	"image/color"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
)

// Config mirrors lazygit's gui.theme block, so a lazygit theme can be pasted
// into gitdeck's config as-is. Each entry is a list of color names and
// attributes, e.g. ["green", "bold"].
type Config struct {
	ActiveBorderColor               []string `json:"activeBorderColor,omitempty" yaml:"activeBorderColor"`
	InactiveBorderColor             []string `json:"inactiveBorderColor,omitempty" yaml:"inactiveBorderColor"`
	SearchingActiveBorderColor      []string `json:"searchingActiveBorderColor,omitempty" yaml:"searchingActiveBorderColor"`
	OptionsTextColor                []string `json:"optionsTextColor,omitempty" yaml:"optionsTextColor"`
	SelectedLineBgColor             []string `json:"selectedLineBgColor,omitempty" yaml:"selectedLineBgColor"`
	InactiveViewSelectedLineBgColor []string `json:"inactiveViewSelectedLineBgColor,omitempty" yaml:"inactiveViewSelectedLineBgColor"`
	UnstagedChangesColor            []string `json:"unstagedChangesColor,omitempty" yaml:"unstagedChangesColor"`
	DefaultFgColor                  []string `json:"defaultFgColor,omitempty" yaml:"defaultFgColor"`
	// Border is the panel border: rounded, single, double, thick or hidden.
	Border string `json:"border,omitempty" yaml:"-"`
}

// Defaults are lazygit's own theme defaults.
func Defaults() Config {
	return Config{
		ActiveBorderColor:               []string{"green", "bold"},
		InactiveBorderColor:             []string{"default"},
		SearchingActiveBorderColor:      []string{"cyan", "bold"},
		OptionsTextColor:                []string{"blue"},
		SelectedLineBgColor:             []string{"blue"},
		InactiveViewSelectedLineBgColor: []string{"bold"},
		UnstagedChangesColor:            []string{"red"},
		DefaultFgColor:                  []string{"default"},
		Border:                          "rounded",
	}
}

// Merge overlays every field set in o onto c.
func (c Config) Merge(o Config) Config {
	pick := func(dst *[]string, src []string) {
		if len(src) > 0 {
			*dst = src
		}
	}
	pick(&c.ActiveBorderColor, o.ActiveBorderColor)
	pick(&c.InactiveBorderColor, o.InactiveBorderColor)
	pick(&c.SearchingActiveBorderColor, o.SearchingActiveBorderColor)
	pick(&c.OptionsTextColor, o.OptionsTextColor)
	pick(&c.SelectedLineBgColor, o.SelectedLineBgColor)
	pick(&c.InactiveViewSelectedLineBgColor, o.InactiveViewSelectedLineBgColor)
	pick(&c.UnstagedChangesColor, o.UnstagedChangesColor)
	pick(&c.DefaultFgColor, o.DefaultFgColor)
	if o.Border != "" {
		c.Border = o.Border
	}
	return c
}

// Theme is the resolved set of styles.
type Theme struct {
	ActiveBorder    lipgloss.Style
	InactiveBorder  lipgloss.Style
	SearchingBorder lipgloss.Style
	Options         lipgloss.Style
	Selected        lipgloss.Style // applied over a row in the focused panel
	InactiveSel     lipgloss.Style // applied over a row in other panels
	Unstaged        lipgloss.Style
	Fg              lipgloss.Style
	Border          lipgloss.Border

	// Semantic colors. Plain ANSI, so they follow the terminal theme.
	Red, Green, Yellow, Blue, Magenta, Cyan lipgloss.Style
	Bold, Dim, Italic                       lipgloss.Style

	// Authors is the rotation used to tell commit authors apart.
	Authors []lipgloss.Style
}

// T is the active theme. Set once at startup by Load.
var T = Build(Defaults())

// Load resolves cfg and installs it as T.
func Load(cfg Config) { T = Build(cfg) }

// Build resolves a Config into styles.
func Build(cfg Config) Theme {
	plain := lipgloss.NewStyle()
	fg := func(c color.Color) lipgloss.Style { return plain.Foreground(c) }
	t := Theme{
		ActiveBorder:    Style(cfg.ActiveBorderColor, false),
		InactiveBorder:  Style(cfg.InactiveBorderColor, false),
		SearchingBorder: Style(cfg.SearchingActiveBorderColor, false),
		Options:         Style(cfg.OptionsTextColor, false),
		Selected:        Style(cfg.SelectedLineBgColor, true),
		InactiveSel:     Style(cfg.InactiveViewSelectedLineBgColor, true),
		Unstaged:        Style(cfg.UnstagedChangesColor, false),
		Fg:              Style(cfg.DefaultFgColor, false),
		Border:          borderFor(cfg.Border),
		Red:             fg(lipgloss.Red),
		Green:           fg(lipgloss.Green),
		Yellow:          fg(lipgloss.Yellow),
		Blue:            fg(lipgloss.Blue),
		Magenta:         fg(lipgloss.Magenta),
		Cyan:            fg(lipgloss.Cyan),
		Bold:            plain.Bold(true),
		Dim:             plain.Faint(true),
		Italic:          plain.Italic(true),
	}
	t.Authors = []lipgloss.Style{t.Cyan, t.Magenta, t.Yellow, t.Green, t.Blue, t.Red,
		fg(lipgloss.BrightCyan), fg(lipgloss.BrightMagenta), fg(lipgloss.BrightYellow),
		fg(lipgloss.BrightGreen), fg(lipgloss.BrightBlue), fg(lipgloss.BrightRed)}
	return t
}

func borderFor(name string) lipgloss.Border {
	switch name {
	case "single":
		return lipgloss.NormalBorder()
	case "double":
		return lipgloss.DoubleBorder()
	case "thick", "bold":
		return lipgloss.ThickBorder()
	case "hidden":
		return lipgloss.HiddenBorder()
	default:
		return lipgloss.RoundedBorder()
	}
}

// Style turns a lazygit-style attribute list into a style. When asBg is set
// the color goes to the background, as with selectedLineBgColor.
func Style(attrs []string, asBg bool) lipgloss.Style {
	s := lipgloss.NewStyle()
	for _, a := range attrs {
		a = strings.ToLower(strings.TrimSpace(a))
		switch a {
		case "bold":
			s = s.Bold(true)
		case "underline":
			s = s.Underline(true)
		case "reverse":
			s = s.Reverse(true)
		case "italic":
			s = s.Italic(true)
		case "faint", "dim":
			s = s.Faint(true)
		case "default", "":
		default:
			c := Color(a)
			if c == nil {
				continue
			}
			if asBg {
				s = s.Background(c)
			} else {
				s = s.Foreground(c)
			}
		}
	}
	return s
}

var named = map[string]color.Color{
	"black": lipgloss.Black, "red": lipgloss.Red, "green": lipgloss.Green,
	"yellow": lipgloss.Yellow, "blue": lipgloss.Blue, "magenta": lipgloss.Magenta,
	"cyan": lipgloss.Cyan, "white": lipgloss.White,
	"brightblack": lipgloss.BrightBlack, "gray": lipgloss.BrightBlack, "grey": lipgloss.BrightBlack,
	"brightred": lipgloss.BrightRed, "brightgreen": lipgloss.BrightGreen,
	"brightyellow": lipgloss.BrightYellow, "brightblue": lipgloss.BrightBlue,
	"brightmagenta": lipgloss.BrightMagenta, "brightcyan": lipgloss.BrightCyan,
	"brightwhite": lipgloss.BrightWhite,
}

// Color parses an ANSI name ("green", "brightblue"), an ANSI index ("2"),
// or a hex value ("#a6e3a1"). Unknown values return nil.
func Color(s string) color.Color {
	s = strings.ReplaceAll(strings.ToLower(s), "_", "")
	s = strings.ReplaceAll(s, "-", "")
	if c, ok := named[s]; ok {
		return c
	}
	if strings.HasPrefix(s, "#") {
		return lipgloss.Color(s)
	}
	if n, err := strconv.Atoi(s); err == nil && n >= 0 && n < 256 {
		return lipgloss.Color(s)
	}
	return nil
}

// Over layers a selection style (background and attributes) over a cell
// style. On a colored selection bar the cell's own foreground is dropped:
// which ANSI colors read well on which is up to the terminal theme, and the
// default foreground is the one color every theme makes readable.
func Over(cell, sel lipgloss.Style) lipgloss.Style {
	bg := sel.GetBackground()
	if _, none := bg.(lipgloss.NoColor); bg != nil && !none {
		// Faint text washes out on a colored bar too.
		cell = cell.UnsetForeground().Background(bg).Faint(false)
	}
	if sel.GetBold() {
		cell = cell.Bold(true)
	}
	if sel.GetReverse() {
		cell = cell.UnsetForeground().Faint(false).Reverse(true)
	}
	if sel.GetUnderline() {
		cell = cell.Underline(true)
	}
	if sel.GetItalic() {
		cell = cell.Italic(true)
	}
	if fg := sel.GetForeground(); fg != nil {
		if _, none := fg.(lipgloss.NoColor); !none {
			cell = cell.Foreground(fg)
		}
	}
	return cell
}

// Author picks a stable color for a commit author.
func (t Theme) Author(name string) lipgloss.Style {
	var h uint32 = 2166136261
	for i := 0; i < len(name); i++ {
		h ^= uint32(name[i])
		h *= 16777619
	}
	return t.Authors[int(h%uint32(len(t.Authors)))]
}

// Hue maps an arbitrary hex color (a GitHub label, say) onto the nearest
// ANSI hue, so data colors still follow the terminal theme. Greys map to the
// default foreground.
func (t Theme) Hue(hex string) lipgloss.Style {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return t.Fg
	}
	v, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		return t.Fg
	}
	r := float64(v>>16&0xff) / 255
	g := float64(v>>8&0xff) / 255
	b := float64(v&0xff) / 255
	mx, mn := max(r, g, b), min(r, g, b)
	d := mx - mn
	if d < 0.12 {
		return t.Fg
	}
	var h float64
	switch mx {
	case r:
		h = 60 * mod((g-b)/d, 6)
	case g:
		h = 60 * ((b-r)/d + 2)
	default:
		h = 60 * ((r-g)/d + 4)
	}
	switch {
	case h < 20 || h >= 335:
		return t.Red
	case h < 70:
		return t.Yellow
	case h < 160:
		return t.Green
	case h < 200:
		return t.Cyan
	case h < 250:
		return t.Blue
	default:
		return t.Magenta
	}
}

func mod(a, b float64) float64 {
	for a < 0 {
		a += b
	}
	for a >= b {
		a -= b
	}
	return a
}
