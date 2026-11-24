package ui

import (
	"os"
	"os/exec"
	"runtime"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/bkenks/gitdeck/internal/git"
	"github.com/bkenks/gitdeck/internal/proc"
)

// ─── Handing the terminal to other programs ─────────────────────────────────

// openLazygit suspends gitdeck, runs lazygit in dir on the matching panel,
// and refreshes everything when lazygit quits.
func (a *App) openLazygit(dir string, args ...string) tea.Cmd {
	if !a.hasLazygit {
		return a.flash("lazygit isn't installed: https://github.com/jesseduffield/lazygit#installation", true)
	}
	a.touch(dir)
	argv := append(append([]string{}, a.cfg.Lazygit.Args...), args...)
	cmd := exec.Command(a.cfg.Lazygit.Command, argv...)
	cmd.Dir = dir
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return execDoneMsg{err: err, dir: dir, what: proc.Display("lazygit", args...)}
	})
}

func (a *App) shell() string {
	if a.cfg.Shell != "" {
		return a.cfg.Shell
	}
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	if runtime.GOOS == "windows" {
		return "powershell.exe"
	}
	return "/bin/sh"
}

func (a *App) openShell(dir string) tea.Cmd {
	a.touch(dir)
	cmd := exec.Command(a.shell())
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GITDECK=1")
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return execDoneMsg{err: err, dir: dir, what: "shell"}
	})
}

func (a *App) openEditor(dir, path string) tea.Cmd {
	editor := a.cfg.Editor
	for _, env := range []string{"VISUAL", "EDITOR"} {
		if editor == "" {
			editor = os.Getenv(env)
		}
	}
	if editor == "" {
		editor = "vi"
	}
	f := strings.Fields(editor)
	cmd := exec.Command(f[0], append(f[1:], path)...)
	cmd.Dir = dir
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return execDoneMsg{err: err, dir: dir, what: f[0] + " " + path}
	})
}

// customKeybind runs a user keybind from the config, if one matches.
func (a *App) customKeybind(key string) tea.Cmd {
	repo := a.selectedRepo()
	if repo == nil {
		return nil
	}
	for _, kb := range a.cfg.Keybinds {
		if normalizeKey(kb.Keys) != key {
			continue
		}
		a.touch(repo.Path)
		script := kb.Command
		if !kb.ReturnOnExit {
			script += "\nprintf '\\n\\033[2m[enter: back to gitdeck]\\033[0m'; read _"
		}
		cmd := exec.Command(a.shell(), "-c", script)
		cmd.Dir = repo.Path
		cmd.Env = append(os.Environ(), "GITDECK=1", "GITDECK_REPO="+repo.Path)
		dir, name := repo.Path, kb.Name
		return tea.ExecProcess(cmd, func(err error) tea.Msg {
			return execDoneMsg{err: err, dir: dir, what: name}
		})
	}
	return nil
}

// normalizeKey turns gitkeeper-style "ctrl + g" / "cmd+shift+r" into Bubble
// Tea's key strings.
func normalizeKey(k string) string {
	k = strings.ToLower(strings.ReplaceAll(k, " ", ""))
	parts := strings.Split(k, "+")
	for i, p := range parts {
		switch p {
		case "cmd", "command":
			parts[i] = "super"
		case "option", "opt":
			parts[i] = "alt"
		case "return":
			parts[i] = "enter"
		case "del":
			parts[i] = "delete"
		case "escape":
			parts[i] = "esc"
		}
	}
	// A lone shifted letter arrives as the capital: "shift+g" is "G".
	if len(parts) == 2 && parts[0] == "shift" && len(parts[1]) == 1 {
		return strings.ToUpper(parts[1])
	}
	return strings.Join(parts, "+")
}

// ─── Clipboard and browser ─────────────────────────────────────────────────

// copy puts text on the clipboard with OSC 52 (works over ssh) and the
// platform's clipboard tool when there is one.
func (a *App) copy(text string) tea.Cmd {
	native := func() tea.Msg {
		for _, c := range [][]string{{"pbcopy"}, {"wl-copy"}, {"xclip", "-selection", "clipboard"}, {"xsel", "-b", "-i"}, {"clip.exe"}} {
			if _, err := exec.LookPath(c[0]); err != nil {
				continue
			}
			cmd := exec.Command(c[0], c[1:]...)
			cmd.Stdin = strings.NewReader(text)
			proc.Detach(cmd)
			if cmd.Run() == nil {
				break
			}
		}
		return nil
	}
	return tea.Batch(tea.SetClipboard(text), native, a.flash("Copied "+truncate(text, 60), false))
}

func openURL(url string) tea.Cmd {
	return func() tea.Msg {
		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "darwin":
			cmd = exec.Command("open", url)
		case "windows":
			cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
		default:
			cmd = exec.Command("xdg-open", url)
		}
		proc.Detach(cmd)
		if err := cmd.Start(); err != nil {
			return resultMsg{res: proc.Result{Cmd: "open " + url, Err: err}}
		}
		go func() { _ = cmd.Wait() }()
		return nil
	}
}

func (a *App) openRepoWeb(r *git.Repo, sub string) tea.Cmd {
	base := ""
	if r.Stats.Slug != "" {
		base = "https://github.com/" + r.Stats.Slug
	} else {
		base = git.ParseRemote(r.Stats.RemoteURL).WebURL()
	}
	if base == "" {
		return a.flash("This repo has no web remote", true)
	}
	if sub != "" {
		base += "/" + sub
	}
	return openURL(base)
}
