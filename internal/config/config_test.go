package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLazygitThemeIsAdopted(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("LG_CONFIG_FILE", "")
	t.Setenv("CONFIG_DIR", "")
	yml := "gui:\n  border: single\n  theme:\n    activeBorderColor:\n      - magenta\n      - bold\n    selectedLineBgColor: [reverse]\n"
	if err := os.MkdirAll(filepath.Join(dir, "lazygit"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "lazygit", "config.yml"), []byte(yml), 0o644); err != nil {
		t.Fatal(err)
	}
	th := LazygitTheme()
	if len(th.ActiveBorderColor) != 2 || th.ActiveBorderColor[0] != "magenta" {
		t.Errorf("activeBorderColor = %v", th.ActiveBorderColor)
	}
	if th.Border != "single" || len(th.SelectedLineBgColor) != 1 || th.SelectedLineBgColor[0] != "reverse" {
		t.Errorf("theme = %+v", th)
	}
}

func TestFirstRunAdoptsGitkeeper(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("GITDECK_CONFIG", "")
	t.Setenv("GITKEEPER_CONFIG", "")
	t.Setenv("GITKEEPER_REPOS", "")
	gk := `{"reposDir": "/somewhere/repos", "keybinds": [{"name": "lazygit", "keys": "ctrl+g", "command": "lazygit", "returnOnExit": true}]}`
	if err := os.MkdirAll(filepath.Join(dir, "gitkeeper"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gitkeeper", "config.json"), []byte(gk), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ReposDir != "/somewhere/repos" || len(cfg.Keybinds) != 1 {
		t.Errorf("didn't adopt gitkeeper: %+v", cfg)
	}
	if _, err := os.Stat(filepath.Join(dir, "gitdeck", "config.json")); err != nil {
		t.Error("first run should write the config")
	}
	again, err := Load()
	if err != nil || again.ReposDir != cfg.ReposDir || len(again.GitHub.PRSections) == 0 {
		t.Errorf("reload: %+v %v", again, err)
	}
}
