// gitdeck: every repo on your machine in one TUI, lazygit-style, with your
// GitHub dashboard built in.
package main

import (
	"fmt"
	"os"
	"runtime/debug"

	tea "charm.land/bubbletea/v2"

	"github.com/bkenks/gitdeck/internal/config"
	"github.com/bkenks/gitdeck/internal/git"
	"github.com/bkenks/gitdeck/internal/theme"
	"github.com/bkenks/gitdeck/internal/ui"
)

// version is set at release time with -ldflags "-X main.version=v1.2.3".
var version = ""

func resolveVersion() string {
	if version != "" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return "dev"
}

const usage = `gitdeck: every repo on your machine, lazygit-style, with GitHub built in.

Usage:
  gitdeck              open the local view (the repo you're in is preselected)
  gitdeck gh           open the GitHub dashboard
  gitdeck --version
  gitdeck --help

Config: %s
  reposDir     where your repos live ($GITDECK_REPOS overrides)
  theme        lazygit's gui.theme keys; your lazygit theme is read automatically
  github       prSections / issueSections, gh-dash style search filters
  keybinds     {"name","keys","command","returnOnExit"}, same as gitkeeper

Needs git. GitHub features need gh (https://cli.github.com), lazygit integration
needs lazygit (https://github.com/jesseduffield/lazygit).
`

func main() {
	opts := ui.Options{Version: resolveVersion()}
	for _, arg := range os.Args[1:] {
		switch arg {
		case "-v", "--version", "version":
			fmt.Println("gitdeck", opts.Version)
			return
		case "-h", "--help", "help":
			fmt.Printf(usage, config.TildePath(config.Path()))
			return
		case "gh", "github", "-g", "--github":
			opts.GitHub = true
		default:
			fmt.Fprintf(os.Stderr, "gitdeck: unknown argument %q (try --help)\n", arg)
			os.Exit(2)
		}
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "gitdeck: config %s: %v\n", config.Path(), err)
		os.Exit(1)
	}
	// Theme precedence: lazygit's defaults, then your lazygit theme, then
	// anything set in gitdeck's own config.
	theme.Load(theme.Defaults().Merge(config.LazygitTheme()).Merge(cfg.Theme))

	if wd, err := os.Getwd(); err == nil {
		opts.Cwd = git.Toplevel(wd)
	}

	p := tea.NewProgram(ui.New(cfg, opts))
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "gitdeck:", err)
		os.Exit(1)
	}
}
