# gitdeck

> Every repo on your machine in one TUI: lazygit-style panels for local work, a gh-dash-style
> dashboard for GitHub, and lazygit itself one key away.

gitdeck is the successor to [gitkeeper](https://github.com/bkenks/gitkeeper). It keeps the repo
list and adds two things: a lazygit-style view of whichever repo is selected, and your GitHub PRs
and issues next to it.

## Contents

- [What it does](#what-it-does)
- [Colors follow your terminal](#colors-follow-your-terminal)
- [Install](#install)
- [Keys](#keys)
- [Lazygit integration](#lazygit-integration)
- [GitHub integration](#github-integration)
- [Configuration](#configuration)
- [How it works](#how-it-works)

## What it does

**Local** (`tab` switches to GitHub):

- **[1] Repos**: every repo under your repos dir with branch, dirty count and ahead/behind, plus
  the repo you launched from. Clone, pull all, delete, open a shell or your editor.
- **[2] Files**: a lazygit-style file tree. Stage, unstage, commit, discard, stash.
- **[3] Branches**: local, remote and tag tabs. Checkout, create, delete. Branches that have a pull
  request show its number (`#42`), colored by state.
- **[4] Commits** and **[5] Stash**, with the patch in the main panel.
- **Command log**: every git command gitdeck runs, so nothing happens behind your back.

**GitHub**:

- **[1] Pull requests** and **[2] Issues**: gh-dash-style sections (My PRs, Review requested,
  Involved, Recent) with CI status, review state and diff size. A section for the selected
  local repo is added automatically.
- **[0] Preview**: description rendered as markdown, failing checks, reviews and comments, or the
  full diff.
- Check out, approve, merge, comment, close and reopen without leaving the terminal.

## Colors follow your terminal

gitdeck never picks an RGB color. Everything is drawn with your terminal's 16 ANSI colors and its
default foreground and background, the same way lazygit does. Your terminal theme decides what
"green" and "blue" look like, so:

- switching the terminal between light and dark updates gitdeck right away, with no restart and no
  setting to change
- gitdeck matches lazygit, your shell and everything else in the same terminal
- markdown in PR descriptions, including syntax-highlighted code blocks, uses the same 16 colors

gitdeck also **reads your lazygit theme** (`gui.theme` and `gui.border` in lazygit's
`config.yml`), so if you've customized lazygit, gitdeck already matches it.

## Install

```bash
go install github.com/bkenks/gitdeck@latest
```

Or from source:

```bash
git clone https://github.com/bkenks/gitdeck.git && cd gitdeck
mise install        # Go 1.26
go build -o ~/.local/bin/gitdeck .
```

Requires [git](https://git-scm.com/). The GitHub view needs [gh](https://cli.github.com), logged
in with `gh auth login`. Lazygit integration needs
[lazygit](https://github.com/jesseduffield/lazygit).

```bash
gitdeck          # local view, with the repo you're standing in preselected
gitdeck gh       # straight to the GitHub dashboard
gitdeck --help
```

## Keys

The footer always shows the keys for the panel you're in, and `?` lists all of them.

### Everywhere

| Key | Action |
|---|---|
| `tab` | Switch Local / GitHub |
| `1`–`5`, `0` | Jump to a panel (`0` is the main / preview panel) |
| `h` `l` / `←` `→` | Previous / next panel |
| `j` `k` / `↑` `↓` | Move. `g` / `G` top and bottom, `ctrl+d` / `ctrl+u` half a page |
| `/` | Filter the panel. `enter` keeps the filter, `esc` clears it |
| `L` / `ctrl+g` | Open **lazygit** on the matching panel |
| `R` | Refresh |
| `@` | Show or hide the command log |
| `?` | Keybindings |
| `q` / `ctrl+c` | Quit |

### Local

| Panel | Keys |
|---|---|
| Repos | `enter` files · `s` shell · `c` clone · `U` pull all · `p` / `P` / `f` pull / push / fetch · `S` sort · `e` editor · `o` open on GitHub · `y` copy path · `d` delete |
| Files | `space` stage / unstage · `a` stage all · `c` commit · `d` discard · `s` stash · `enter` fold dir or read diff · `e` edit |
| Branches | `space` checkout · `n` new · `d` delete · `o` open PR (or the branch) · `[` `]` tabs · `y` copy |
| Commits | `enter` read patch · `y` copy hash · `o` open on GitHub |
| Stash | `space` apply · `g` pop · `d` drop |

### GitHub

| Key | Action |
|---|---|
| `space` | Check out the PR locally (clones the repo first if you don't have it) |
| `L` | Check out, then open lazygit on it |
| `o` / `y` | Open in browser / copy URL |
| `d` | Toggle the diff in the preview |
| `a` / `m` | Approve / merge (squash, merge commit or rebase) |
| `c` | Comment |
| `x` / `X` | Close / reopen |
| `r` | Mark a draft ready for review |
| `[` `]` | Previous / next section |
| `s` | Run a one-off GitHub search as its own section |

## Lazygit integration

`L` (or `ctrl+g`, if you used that in gitkeeper) hands the terminal to lazygit in the selected
repo and opens it **on the panel you were looking at**: Branches opens lazygit's branches, Commits
its log, Stash its stash. Quit lazygit and you're back in gitdeck with everything refreshed.

From the GitHub view, `L` on a pull request checks it out (cloning the repo if needed) and drops
you straight into lazygit on that branch.

Because gitdeck reads lazygit's theme, the two look like one app.

## GitHub integration

Everything goes through the `gh` CLI, so gitdeck uses whatever account gh is logged in with and
never stores a token. Sections use GitHub's search syntax, the same as gh-dash:

```json
"github": {
  "prSections": [
    { "title": "My PRs", "filters": "is:open author:@me" },
    { "title": "Review requested", "filters": "is:open review-requested:@me" },
    { "title": "Team", "filters": "is:open org:my-org draft:false" }
  ],
  "issueSections": [
    { "title": "Assigned", "filters": "is:open assignee:@me" }
  ],
  "limit": 30
}
```

`is:pr` / `is:issue` is added for you. The two views also feed each other:

- checking out a PR takes you to its repo in the local view, on the Branches panel
- branches with a PR show its number, and `o` opens it
- the selected local repo gets its own section on the GitHub side

## Configuration

`$XDG_CONFIG_HOME/gitdeck/config.json` (`~/.config/gitdeck/config.json`, or `$GITDECK_CONFIG`).
The first run creates it, taking `reposDir` and `keybinds` from gitkeeper if you have it set up.

```json
{
  "reposDir": "~/Development",
  "editor": "",
  "shell": "",
  "sortMode": "recent",
  "lazygit": { "command": "lazygit", "args": [] },
  "theme": {
    "activeBorderColor": ["green", "bold"],
    "selectedLineBgColor": ["blue"],
    "border": "rounded"
  },
  "github": { "prSections": [], "issueSections": [], "limit": 30 },
  "keybinds": [
    { "name": "Claude Code", "keys": "ctrl+o", "command": "claude", "returnOnExit": true }
  ]
}
```

- `reposDir`: where repos live, as `<dir>/<owner>/<repo>`. `$GITDECK_REPOS` overrides it.
- `editor` / `shell`: default to `$VISUAL` / `$EDITOR` and `$SHELL`.
- `sortMode`: `recent`, `name` or `namespace`. `S` cycles it and saves.
- `theme`: lazygit's `gui.theme` keys (`activeBorderColor`, `inactiveBorderColor`,
  `searchingActiveBorderColor`, `optionsTextColor`, `selectedLineBgColor`,
  `inactiveViewSelectedLineBgColor`, `unstagedChangesColor`, `defaultFgColor`) plus `border`
  (`rounded`, `single`, `double`, `thick`, `hidden`). Colors are ANSI names (`green`,
  `brightblue`), `bold` / `underline` / `reverse`, or hex if you really want a fixed color.
  Precedence: lazygit's defaults, then your lazygit config, then this.
- `keybinds`: same format as gitkeeper. The command runs in your shell in the selected repo, with
  the whole terminal. With `returnOnExit` off, gitdeck waits for `enter` so you can read the output.

## How it works

Go on the Charm stack: Bubble Tea v2, Bubbles, Lip Gloss v2 and Glamour v2. Local state comes from
plain `git` (with optional locks off, so refreshing never fights another git process for the
index). GitHub data comes from `gh api graphql`, one request per section, which includes CI and
review status. Commands run in their own session with no controlling terminal, so an ssh or gpg
prompt fails cleanly instead of drawing over the UI. Errors go to the footer and the command log,
and never crash the app.
