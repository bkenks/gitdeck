package ui

import (
	. "github.com/bkenks/gitdeck/internal/theme"
)

// hint is one keybinding. footer ones also show in the options bar.
type hint struct {
	key, desc string
	footer    bool
}

func h(key, desc string) hint  { return hint{key, desc, true} }
func hx(key, desc string) hint { return hint{key, desc, false} }

func (a *App) paneHints(p pane) (string, []hint) {
	switch p {
	case pRepos:
		return "Repos", []hint{
			h("L", "Lazygit"), h("s", "Shell"), h("c", "Clone"), h("U", "Pull all"), h("/", "Filter"),
			hx("enter", "Go to files"), hx("p", "Pull"), hx("P", "Push"), hx("f", "Fetch"),
			hx("S", "Cycle sort"), hx("e", "Open in editor"), hx("o", "Open on GitHub"),
			hx("y", "Copy path"), hx("d", "Delete repo"),
		}
	case pFiles:
		return "Files", []hint{
			h("<space>", "Stage"), h("a", "Stage all"), h("c", "Commit"), h("d", "Discard"), h("s", "Stash"), h("L", "Lazygit"),
			hx("enter", "Fold dir / view diff"), hx("e", "Edit file"), hx("y", "Copy path"),
			hx("p", "Pull"), hx("P", "Push"), hx("f", "Fetch"),
		}
	case pBranches:
		return "Branches", []hint{
			h("<space>", "Checkout"), h("n", "New branch"), h("d", "Delete"), h("o", "Open PR"), h("[ ]", "Tabs"), h("L", "Lazygit"),
			hx("y", "Copy name"), hx("enter", "View log"), hx("p", "Pull"), hx("P", "Push"), hx("f", "Fetch"),
		}
	case pCommits:
		return "Commits", []hint{
			h("enter", "View patch"), h("y", "Copy hash"), h("o", "Open on GitHub"), h("L", "Lazygit"),
			hx("p", "Pull"), hx("P", "Push"), hx("f", "Fetch"),
		}
	case pStash:
		return "Stash", []hint{
			h("<space>", "Apply"), h("g", "Pop"), h("d", "Drop"), h("L", "Lazygit"),
		}
	case pMain:
		return "Main panel", []hint{
			h("j/k", "Scroll"), h("ctrl+d/u", "Page"), h("g/G", "Top/bottom"), h("esc", "Back"),
		}
	case pPRs:
		return "Pull requests", []hint{
			h("<space>", "Checkout"), h("L", "Checkout + lazygit"), h("o", "Open"), h("d", "Diff"),
			h("a", "Approve"), h("m", "Merge"), h("[ ]", "Sections"),
			hx("c", "Comment"), hx("x", "Close"), hx("X", "Reopen"), hx("r", "Mark ready"),
			hx("y", "Copy URL"), hx("s", "Search GitHub"), hx("/", "Filter"), hx("enter", "Read preview"),
		}
	case pIssues:
		return "Issues", []hint{
			h("o", "Open"), h("c", "Comment"), h("x", "Close"), h("[ ]", "Sections"), h("s", "Search"),
			hx("X", "Reopen"), hx("y", "Copy URL"), hx("/", "Filter"), hx("enter", "Read preview"),
		}
	case pPreview:
		return "Preview", []hint{
			h("j/k", "Scroll"), h("ctrl+d/u", "Page"), h("d", "Toggle diff"), h("esc", "Back"),
		}
	}
	return "", nil
}

func (a *App) hints() []hint {
	_, hs := a.paneHints(a.focus)
	return hs
}

func globalHints(m mode) []hint {
	hs := []hint{
		hx("tab", "Switch Local / GitHub"),
	}
	if m == modeLocal {
		hs = append(hs, hx("1-5", "Jump to panel"), hx("0", "Main panel"), hx("h/l", "Previous / next panel"))
	} else {
		hs = append(hs, hx("1 / 2", "PRs / issues"), hx("0", "Preview"), hx("h/l", "Previous / next panel"))
	}
	return append(hs,
		hx("j/k", "Move"), hx("g/G", "Top / bottom"), hx("/", "Filter panel"),
		hx("R", "Refresh"), hx("@", "Toggle command log"), hx("?", "This help"), hx("q", "Quit"),
	)
}

func (a *App) openHelp() {
	var lines []string
	section := func(title string, hs []hint) {
		lines = append(lines, "", " "+T.ActiveBorder.Render(title))
		for _, x := range hs {
			lines = append(lines, "   "+T.Options.Render(fit(x.key, 12))+x.desc)
		}
	}
	name, hs := a.paneHints(a.focus)
	section(name, hs)
	section("Everywhere", globalHints(a.mode))
	if len(a.cfg.Keybinds) > 0 {
		var custom []hint
		for _, kb := range a.cfg.Keybinds {
			custom = append(custom, hx(normalizeKey(kb.Keys), kb.Name+T.Dim.Render("  "+kb.Command)))
		}
		section("Your keybinds", custom)
	}
	lines = append(lines, "")
	a.modal = &helpModal{lines: lines}
}
