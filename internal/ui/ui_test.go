package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/bkenks/gitdeck/internal/git"
	. "github.com/bkenks/gitdeck/internal/theme"
)

// P is a plain-text seg.
func P(text string) Seg { return S(text, T.Fg) }

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

// listOf is a list over items whose ids are their text.
func listOf(texts *[]string) list {
	return list{source: func() []item {
		items := make([]item, len(*texts))
		for i, t := range *texts {
			items[i] = item{id: t, text: t}
		}
		return items
	}}
}

func TestListWindowKeepsCursorVisible(t *testing.T) {
	var texts []string
	for i := range 100 {
		texts = append(texts, itoa(i))
	}
	l := listOf(&texts)
	l.refilter()
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
	l := listOf(&items)
	l.refilter()
	l.cursor = 2
	l.filter = "a"
	l.refilter()
	if l.selectedID() != "gamma" {
		t.Errorf("filtering moved the selection to %q", l.selectedID())
	}

	l.filter = ""
	items = []string{"delta", "gamma", "alpha"}
	l.refilter()
	if l.selectedID() != "gamma" || l.cursor != 1 {
		t.Errorf("reordering moved the selection to %q at %d", l.selectedID(), l.cursor)
	}

	items = []string{"delta", "alpha"}
	l.refilter()
	if l.selectedID() != "alpha" || l.cursor != 1 {
		t.Errorf("a removed item should leave the cursor in place, got %q at %d", l.selectedID(), l.cursor)
	}

	l.filter = "zzz"
	l.refilter()
	if l.sel() != -1 || l.selectedID() != "" {
		t.Errorf("empty view should select nothing, got %d %q", l.sel(), l.selectedID())
	}
}

func TestListFilterIgnoresCaseAndOuterSpace(t *testing.T) {
	items := []string{"Alpha", "beta"}
	l := listOf(&items)
	l.setFilter("  AL ")
	if l.len() != 1 || l.selectedID() != "Alpha" {
		t.Errorf("filter %q shows %d, selects %q", l.filter, l.len(), l.selectedID())
	}
}

func TestListResetForgetsSelection(t *testing.T) {
	items := []string{"alpha", "beta", "gamma"}
	l := listOf(&items)
	l.refilter()
	l.setFilter("a")
	l.cursor, l.offset = 2, 1
	l.reset()
	if l.filter != "" || l.cursor != 0 || l.offset != 0 || l.len() != 0 {
		t.Errorf("reset left filter %q cursor %d offset %d len %d", l.filter, l.cursor, l.offset, l.len())
	}
	l.refilter()
	if l.selectedID() != "alpha" || l.len() != 3 {
		t.Errorf("after reset: %q of %d", l.selectedID(), l.len())
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
