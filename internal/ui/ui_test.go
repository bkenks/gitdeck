package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/bkenks/gitdeck/internal/git"
	. "github.com/bkenks/gitdeck/internal/theme"
)

func TestPanelIsExactlyItsSize(t *testing.T) {
	for _, w := range []int{10, 33, 80} {
		for _, h := range []int{3, 7, 20} {
			p := Panel{
				W: w, H: h, Index: "3", Tabs: []string{"Local branches", "Remotes", "Tags"},
				Footer: "12 of 340", Status: "/filter", Focused: true,
				Body:  []string{"a short line", strings.Repeat("too long ", 30), T.Red.Render("styled")},
				Total: 100, Offset: 40,
			}
			lines := strings.Split(p.Render(), "\n")
			if len(lines) != h {
				t.Fatalf("%dx%d: got %d lines", w, h, len(lines))
			}
			for i, l := range lines {
				if got := ansi.StringWidth(l); got != w {
					t.Errorf("%dx%d line %d is %d wide: %q", w, h, i, got, ansi.Strip(l))
				}
			}
		}
	}
}

func TestRenderRowFits(t *testing.T) {
	row := Row{
		Left:  []Seg{P("bkenks/"), P(strings.Repeat("long-repo-name", 5))},
		Right: []Seg{P("~3 "), P("feature/some-very-long-branch-name")},
	}
	for _, w := range []int{5, 20, 60, 200} {
		sel := T.Selected
		for _, s := range []*Seg{nil, {}} {
			var st = &sel
			if s == nil {
				st = nil
			}
			if got := ansi.StringWidth(renderRow(row, w, st)); got != w {
				t.Errorf("width %d: row is %d wide", w, got)
			}
		}
	}
}

func TestListWindowKeepsCursorVisible(t *testing.T) {
	var l list
	l.refilter(100, func(i int) string { return itoa(i) })
	for _, c := range []int{0, 5, 50, 99, 3} {
		l.cursor = c
		from, to := l.window(10)
		if c < from || c >= to || to-from != 10 {
			t.Errorf("cursor %d outside window [%d,%d)", c, from, to)
		}
	}
}

func TestRefilterKeepsSelection(t *testing.T) {
	items := []string{"alpha", "beta", "gamma", "delta"}
	var l list
	text := func(i int) string { return items[i] }
	l.refilter(len(items), text)
	l.cursor = 2 // gamma
	l.filter = "a"
	l.refilter(len(items), text)
	if items[l.sel()] != "gamma" {
		t.Errorf("selection moved to %q", items[l.sel()])
	}
	l.filter = "zzz"
	l.refilter(len(items), text)
	if l.sel() != -1 {
		t.Errorf("empty view should select nothing, got %d", l.sel())
	}
}

func TestFuzzy(t *testing.T) {
	for _, c := range []struct {
		s, q string
		want bool
	}{
		{"bkenks/gitdeck", "gdk", true},
		{"bkenks/gitdeck", "deck", true},
		{"bkenks/gitdeck", "kced", false},
		{"anything", "", true},
	} {
		if got := fuzzy(c.s, c.q); got != c.want {
			t.Errorf("fuzzy(%q, %q) = %v", c.s, c.q, got)
		}
	}
}

func TestDistribute(t *testing.T) {
	for _, total := range []int{15, 40, 61} {
		got := distribute(total, []int{3, 3, 2, 3, 0}, []int{3, 3, 3, 3, 3})
		sum := 0
		for _, n := range got {
			sum += n
		}
		if sum != total {
			t.Errorf("distribute(%d) = %v sums to %d", total, got, sum)
		}
		if got[4] != 3 {
			t.Errorf("zero-weight panel should stay at its minimum, got %d", got[4])
		}
	}
}

func TestBuildTreeFoldsSingleChildDirs(t *testing.T) {
	files := []git.File{
		{Path: ".claude/skills/a.json", X: '?', Y: '?'},
		{Path: ".claude/skills/b", X: '?', Y: '?'},
		{Path: "README.md", X: ' ', Y: 'M'},
		{Path: "src/x/y.go", X: 'A', Y: ' '},
	}
	rows := buildTree(files, map[string]bool{})
	var got []string
	for _, r := range rows {
		got = append(got, strings.Repeat(" ", r.depth)+r.name)
	}
	want := []string{".claude/skills", " a.json", " b", "src/x", " y.go", "README.md"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("tree:\n got %q\nwant %q", got, want)
	}
	if len(rows[0].files) != 2 {
		t.Errorf("dir row should own its 2 files, has %d", len(rows[0].files))
	}
	collapsed := buildTree(files, map[string]bool{".claude/skills": true})
	if len(collapsed) != 4 || len(collapsed[0].files) != 2 {
		t.Errorf("collapsed tree: %d rows, dir owns %d files", len(collapsed), len(collapsed[0].files))
	}
}

func TestNormalizeKey(t *testing.T) {
	for in, want := range map[string]string{
		"ctrl + g":    "ctrl+g",
		"cmd+shift+r": "super+shift+r",
		"shift+g":     "G",
		"alt+return":  "alt+enter",
		"F5":          "f5",
	} {
		if got := normalizeKey(in); got != want {
			t.Errorf("normalizeKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWebURL(t *testing.T) {
	for in, want := range map[string]string{
		"git@gitlab.com:group/proj.git":         "https://gitlab.com/group/proj",
		"ssh://git@codeberg.org/me/thing.git":   "https://codeberg.org/me/thing",
		"https://github.com/bkenks/gitdeck.git": "https://github.com/bkenks/gitdeck",
		"/local/path":                           "",
	} {
		if got := webURL(in); got != want {
			t.Errorf("webURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestColorDiffUsesOnlyANSIColors(t *testing.T) {
	diff := "diff --git a/x b/x\nindex 1..2 100644\n--- a/x\n+++ b/x\n@@ -1 +1 @@ ctx\n-old\n+new\n x | 2 +-\n"
	for _, l := range colorDiff(diff) {
		// 38;5 / 38;2 would be fixed 256/true colors that ignore the theme.
		if strings.Contains(l, "38;5;") || strings.Contains(l, "38;2;") || strings.Contains(l, "48;") {
			t.Errorf("non-ANSI color in %q", l)
		}
	}
}

func TestMarkdownUsesOnlyANSIColors(t *testing.T) {
	src := "# Title\n\nSome **bold** and `code` with a [link](https://x.y).\n\n```go\nfunc main() { fmt.Println(\"hi\") }\n```\n\n> quote\n\n- [x] done\n"
	for _, l := range markdown(src, 60) {
		if strings.Contains(l, "38;2;") || strings.Contains(l, "48;2;") || strings.Contains(l, "48;5;") {
			t.Errorf("true-color or background escape in %q", l)
		}
	}
}
