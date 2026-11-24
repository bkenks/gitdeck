package git

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestGitHubSlug(t *testing.T) {
	cases := []struct{ url, want string }{
		{"git@github.com:bkenks/gitdeck.git", "bkenks/gitdeck"},
		{"git@github.com:bkenks/gitdeck", "bkenks/gitdeck"},
		{"  git@github.com:bkenks/gitdeck.git\n", "bkenks/gitdeck"},
		{"ssh://git@github.com/bkenks/gitdeck", "bkenks/gitdeck"},
		{"ssh://git@github.com/bkenks/gitdeck.git", "bkenks/gitdeck"},
		{"ssh://git@github.com:22/bkenks/gitdeck.git", ""},
		{"https://github.com/bkenks/gitdeck", "bkenks/gitdeck"},
		{"https://github.com/bkenks/gitdeck.git", "bkenks/gitdeck"},
		{"https://github.com/bkenks/gitdeck/", "bkenks/gitdeck"},
		{"https://github.com/bkenks/gitdeck.git/", "bkenks/gitdeck.git"},
		{"https://github.com/bkenks/gitdeck/tree/main", "bkenks/gitdeck"},
		{"http://github.com/bkenks/gitdeck", "bkenks/gitdeck"},
		{"https://x-access-token:tok@github.com/bkenks/gitdeck", "bkenks/gitdeck"},
		{"ssh://github.com/bkenks/gitdeck", "bkenks/gitdeck"},
		{"github.com:bkenks/gitdeck", "bkenks/gitdeck"},
		{"git://github.com/bkenks/gitdeck.git", "bkenks/gitdeck"},
		{"https://github.com", ""},
		{"https://github.com/bkenks", ""},
		{"git@github.com:bkenks", ""},
		{"bkenks/gitdeck", ""},
		{"git@gitlab.com:bkenks/gitdeck.git", ""},
		{"https://gitlab.com/group/sub/proj.git", ""},
		{"/local/path", ""},
		{"garbage", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := ParseRemote(c.url).GitHubSlug(); got != c.want {
			t.Errorf("ParseRemote(%q).GitHubSlug() = %q, want %q", c.url, got, c.want)
		}
	}
}

func TestCloneDest(t *testing.T) {
	root := "/r"
	cases := []struct{ url, dest, slug string }{
		{"git@github.com:bkenks/gitdeck.git", "/r/bkenks/gitdeck", ""},
		{"ssh://git@github.com/bkenks/gitdeck", "/r/bkenks/gitdeck", ""},
		{"ssh://git@github.com:22/bkenks/gitdeck.git", "/r/bkenks/gitdeck", ""},
		{"ssh://git@host.example:2222/g/r.git", "/r/g/r", ""},
		{"https://github.com/bkenks/gitdeck", "/r/bkenks/gitdeck", ""},
		{"https://github.com/bkenks/gitdeck/", "/r/bkenks/gitdeck", ""},
		{"https://github.com/bkenks/gitdeck.git/", "/r/bkenks/gitdeck.git", ""},
		{"http://github.com/bkenks/gitdeck", "/r/bkenks/gitdeck", ""},
		{"git://github.com/bkenks/gitdeck.git", "/r/bkenks/gitdeck", ""},
		{"https://github.com/bkenks", "/r/github.com/bkenks", ""},
		{"git@github.com:bkenks", "/r/github.com/bkenks", ""},
		{"bkenks/gitdeck", "/r/bkenks/gitdeck", "bkenks/gitdeck"},
		{"  bkenks/gitdeck  ", "/r/bkenks/gitdeck", "bkenks/gitdeck"},
		{"bkenks/gitdeck.git", "/r/bkenks/gitdeck", "bkenks/gitdeck"},
		{"bkenks/gitdeck/", "/r/bkenks/gitdeck", ""},
		{"a/b/c", "/r/b/c", ""},
		{"git@gitlab.com:group/proj.git", "/r/group/proj", ""},
		{"git@gitlab.com:group/sub/proj.git", "/r/sub/proj", ""},
		{"https://gitlab.com/group/sub/proj", "/r/sub/proj", ""},
		{"ssh://git@gitlab.example.com:2222/group/sub/proj.git", "/r/sub/proj", ""},
		{"https://git.example.com/a/b/c.git", "/r/b/c", ""},
		{"https://user:tok@host.example/o/r.git", "/r/o/r", ""},
		{"user@host:o/r.git", "/r/o/r", ""},
		{"/srv/git/proj.git", "/r/git/proj", ""},
		{"file:///srv/git/proj.git", "/r/git/proj", ""},
		{"file:///proj.git", "/r/proj", ""},
		{"C:/src/proj", "/r/src/proj", ""},
		{"x:o/r.git", "/r/o/r", ""},
		{"x:r.git", "/r/x/r", ""},
	}
	for _, c := range cases {
		dest, slug, err := CloneDest(root, c.url)
		want := filepath.FromSlash(c.dest)
		if err != nil || dest != want || slug != c.slug {
			t.Errorf("CloneDest(%q) = %q, %q, %v; want %q, %q", c.url, dest, slug, err, want, c.slug)
		}
	}
}

func TestCloneDestRejectsUnplaceableURLs(t *testing.T) {
	for _, url := range []string{
		"", "   ", "garbage", "not a url", "@", "/", "::::", "https://github.com", "git@host:",
	} {
		if dest, slug, err := CloneDest("/r", url); err == nil {
			t.Errorf("CloneDest(%q) = %q, %q; want an error", url, dest, slug)
		}
	}
}

func TestWebURL(t *testing.T) {
	cases := []struct{ remote, want string }{
		{"git@github.com:bkenks/gitdeck.git", "https://github.com/bkenks/gitdeck"},
		{"git@gitlab.com:group/proj.git", "https://gitlab.com/group/proj"},
		{"git@gitlab.com:group/sub/proj.git", "https://gitlab.com/group/sub/proj"},
		{"ssh://git@codeberg.org/me/thing.git", "https://codeberg.org/me/thing"},
		{"ssh://github.com/bkenks/gitdeck", "https://github.com/bkenks/gitdeck"},
		{"ssh://git@host.example:2222/g/r.git", "https://host.example:2222/g/r"},
		{"https://github.com/bkenks/gitdeck.git", "https://github.com/bkenks/gitdeck"},
		{"https://github.com/bkenks/gitdeck", "https://github.com/bkenks/gitdeck"},
		{"https://github.com/bkenks/gitdeck/", "https://github.com/bkenks/gitdeck/"},
		{"https://gitlab.com/group/sub/proj", "https://gitlab.com/group/sub/proj"},
		{"https://host:8443/o/r", "https://host:8443/o/r"},
		{"http://git.example.com/o/r.git", "http://git.example.com/o/r"},
		{"https://user:tok@host.example/o/r.git", "https://host.example/o/r"},
		{"user@host.example:o/r.git", "https://host.example/o/r"},
		{"https://github.com", "https://github.com"},
		{"  git@github.com:bkenks/gitdeck.git\n", "https://github.com/bkenks/gitdeck"},
		{"git://github.com/bkenks/gitdeck.git", ""},
		{"C:/src/proj", ""},
		{"file:///srv/git/proj.git", ""},
		{"/local/path", ""},
		{"bkenks/gitdeck", ""},
		{"garbage", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := ParseRemote(c.remote).WebURL(); got != c.want {
			t.Errorf("ParseRemote(%q).WebURL() = %q, want %q", c.remote, got, c.want)
		}
	}
}

func TestParseRemote(t *testing.T) {
	cases := []struct {
		raw  string
		want Remote
	}{
		{"git@github.com:o/r.git", Remote{Host: "github.com", Path: "o/r"}},
		{"ssh://git@host:2222/g/sub/r.git", Remote{Scheme: "ssh", Host: "host:2222", Path: "g/sub/r"}},
		{"https://u:p@host/o/r/", Remote{Scheme: "https", Host: "host", Path: "o/r/"}},
		{"https://host", Remote{Scheme: "https", Host: "host"}},
		{"file:///srv/r.git", Remote{Scheme: "file", Path: "srv/r"}},
		{"/srv/r.git", Remote{Path: "/srv/r"}},
		{"C:/src/r", Remote{Path: "C:/src/r"}},
		{`C:\src\r`, Remote{Path: `C:\src\r`}},
		{"x:o/r.git", Remote{Host: "x", Path: "o/r"}},
		{"o/r", Remote{Path: "o/r"}},
		{"", Remote{}},
	}
	for _, c := range cases {
		if got := ParseRemote(c.raw); got != c.want {
			t.Errorf("ParseRemote(%q) = %+v, want %+v", c.raw, got, c.want)
		}
	}
}

func FuzzParseRemote(f *testing.F) {
	for _, seed := range []string{
		"git@github.com:o/r.git", "ssh://git@host:22/o/r", "https://u@github.com/o/r/",
		"o/r", "/local/path", "C:/x", "::::", "@", "://", "a@b@c:d", "",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		r := ParseRemote(raw)
		if strings.Contains(r.Host, "@") {
			t.Errorf("ParseRemote(%q).Host = %q still has user info", raw, r.Host)
		}
		_ = r.GitHubSlug()
		_ = r.WebURL()
		_, _, _ = CloneDest("/r", raw)
	})
}
