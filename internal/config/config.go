// Package config loads and saves gitdeck's JSON config, and borrows what it
// can from tools you already use: gitkeeper's repo dir and keybinds, and
// lazygit's theme.
package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/bkenks/gitdeck/internal/theme"
	"gopkg.in/yaml.v3"
)

// Section is one tab of GitHub search results, in gh-dash's style: a title
// and a GitHub search query. "is:pr" / "is:issue" is added for you.
type Section struct {
	Title   string `json:"title"`
	Filters string `json:"filters"`
}

// Keybind runs a shell command in the selected repo.
type Keybind struct {
	Name         string `json:"name"`
	Keys         string `json:"keys"`
	Command      string `json:"command"`
	ReturnOnExit bool   `json:"returnOnExit"`
}

type GitHub struct {
	PRSections    []Section `json:"prSections"`
	IssueSections []Section `json:"issueSections"`
	Limit         int       `json:"limit,omitempty"`
}

type Lazygit struct {
	Command string   `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`
}

type Config struct {
	ReposDir string       `json:"reposDir"`
	Editor   string       `json:"editor,omitempty"`
	Shell    string       `json:"shell,omitempty"`
	SortMode string       `json:"sortMode,omitempty"`
	Lazygit  Lazygit      `json:"lazygit"`
	Theme    theme.Config `json:"theme"`
	GitHub   GitHub       `json:"github"`
	Keybinds []Keybind    `json:"keybinds"`

	path string
}

func Default() Config {
	return Config{
		SortMode: "recent",
		Lazygit:  Lazygit{Command: "lazygit"},
		GitHub: GitHub{
			Limit: 30,
			PRSections: []Section{
				{Title: "My PRs", Filters: "is:open author:@me"},
				{Title: "Review requested", Filters: "is:open review-requested:@me"},
				{Title: "Involved", Filters: "is:open involves:@me -author:@me"},
				{Title: "Recent", Filters: "author:@me"},
			},
			IssueSections: []Section{
				{Title: "Assigned", Filters: "is:open assignee:@me"},
				{Title: "Created", Filters: "is:open author:@me"},
				{Title: "Involved", Filters: "is:open involves:@me -author:@me"},
			},
		},
		Keybinds: []Keybind{},
	}
}

func configHome() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config")
}

func dataHome() string {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share")
}

// Path is where the config lives: $GITDECK_CONFIG, else
// $XDG_CONFIG_HOME/gitdeck/config.json.
func Path() string {
	if p := os.Getenv("GITDECK_CONFIG"); p != "" {
		return p
	}
	return filepath.Join(configHome(), "gitdeck", "config.json")
}

// Load reads the config, creating it on first run. A fresh config adopts
// gitkeeper's repo dir and keybinds when gitkeeper is set up.
func Load() (Config, error) {
	cfg := Default()
	cfg.path = Path()
	data, err := os.ReadFile(cfg.path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		adoptGitkeeper(&cfg)
		if cfg.ReposDir == "" {
			cfg.ReposDir = guessReposDir()
		}
		return cfg, cfg.Save()
	case err != nil:
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	if cfg.GitHub.Limit <= 0 {
		cfg.GitHub.Limit = 30
	}
	if cfg.Lazygit.Command == "" {
		cfg.Lazygit.Command = "lazygit"
	}
	return cfg, nil
}

func (c Config) Save() error {
	if c.path == "" {
		c.path = Path()
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(c.path, append(data, '\n'), 0o644)
}

func (c Config) File() string { return c.path }

// RepoRoot is the repo dir with ~ expanded. $GITDECK_REPOS wins.
func (c Config) RepoRoot() string {
	dir := c.ReposDir
	if env := os.Getenv("GITDECK_REPOS"); env != "" {
		dir = env
	}
	return ExpandHome(dir)
}

func ExpandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	return p
}

// TildePath shortens a path under $HOME to ~/...
func TildePath(p string) string {
	home, _ := os.UserHomeDir()
	if home != "" && (p == home || strings.HasPrefix(p, home+string(filepath.Separator))) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}

func adoptGitkeeper(cfg *Config) {
	p := os.Getenv("GITKEEPER_CONFIG")
	if p == "" {
		p = filepath.Join(configHome(), "gitkeeper", "config.json")
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return
	}
	var gk struct {
		ReposDir string    `json:"reposDir"`
		Keybinds []Keybind `json:"keybinds"`
		UI       struct {
			SortMode string `json:"sortMode"`
		} `json:"ui"`
	}
	if json.Unmarshal(data, &gk) != nil {
		return
	}
	cfg.ReposDir = gk.ReposDir
	if env := os.Getenv("GITKEEPER_REPOS"); env != "" {
		cfg.ReposDir = env
	}
	cfg.Keybinds = gk.Keybinds
}

func guessReposDir() string {
	home, _ := os.UserHomeDir()
	for _, d := range []string{"Development", "dev", "src", "code", "projects", "repos", "git", "Developer"} {
		if fi, err := os.Stat(filepath.Join(home, d)); err == nil && fi.IsDir() {
			return filepath.Join("~", d)
		}
	}
	return ""
}

// LazygitTheme reads gui.theme and gui.border from lazygit's config so
// gitdeck matches it. Missing or unreadable configs give an empty Config.
func LazygitTheme() theme.Config {
	var out theme.Config
	for _, p := range lazygitConfigPaths() {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var lg struct {
			GUI struct {
				Theme  theme.Config `yaml:"theme"`
				Border string       `yaml:"border"`
			} `yaml:"gui"`
		}
		if yaml.Unmarshal(data, &lg) != nil {
			continue
		}
		lg.GUI.Theme.Border = lg.GUI.Border
		out = out.Merge(lg.GUI.Theme)
	}
	return out
}

func lazygitConfigPaths() []string {
	if env := os.Getenv("LG_CONFIG_FILE"); env != "" {
		return strings.Split(env, ",")
	}
	if dir := os.Getenv("CONFIG_DIR"); dir != "" {
		return []string{filepath.Join(dir, "config.yml")}
	}
	paths := []string{filepath.Join(configHome(), "lazygit", "config.yml")}
	if runtime.GOOS == "darwin" && os.Getenv("XDG_CONFIG_HOME") == "" {
		home, _ := os.UserHomeDir()
		paths = append(paths, filepath.Join(home, "Library", "Application Support", "lazygit", "config.yml"))
	}
	return paths
}

// State is small bits of usage history kept between runs.
type State struct {
	LastUsed map[string]time.Time `json:"lastUsed"`
}

func statePath() string { return filepath.Join(dataHome(), "gitdeck", "state.json") }

func LoadState() State {
	s := State{LastUsed: map[string]time.Time{}}
	if data, err := os.ReadFile(statePath()); err == nil {
		_ = json.Unmarshal(data, &s)
	}
	if s.LastUsed == nil {
		s.LastUsed = map[string]time.Time{}
	}
	return s
}

func (s State) Save() {
	p := statePath()
	if os.MkdirAll(filepath.Dir(p), 0o755) != nil {
		return
	}
	if data, err := json.Marshal(s); err == nil {
		_ = os.WriteFile(p, data, 0o644)
	}
}
