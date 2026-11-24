package git

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// Remote is a git remote URL split into the parts gitdeck uses.
type Remote struct {
	// Scheme is "https", "ssh", "git", ... for URL forms; empty for
	// scp-style host:path remotes, local paths and owner/name shorthand.
	Scheme string
	// Host is host[:port] without any user@; empty for local paths and shorthand.
	Host string
	// Path is everything after the host with a trailing ".git" removed,
	// e.g. "owner/name" or "group/sub/project".
	Path string
}

// ParseRemote splits a remote URL in any of the forms git accepts:
// scheme://[user@]host[:port]/path, scp-style [user@]host:path, or a local
// path (which also covers owner/name shorthand). It never fails; parts it
// can't find are left empty.
func ParseRemote(raw string) Remote {
	s := strings.TrimSpace(raw)
	var r Remote
	rest := s
	if scheme, after, ok := strings.Cut(s, "://"); ok {
		r.Scheme = scheme
		r.Host, rest, _ = strings.Cut(after, "/")
	} else if isSCP(s) {
		r.Host, rest, _ = strings.Cut(s, ":")
	}
	if i := strings.LastIndex(r.Host, "@"); i >= 0 {
		r.Host = r.Host[i+1:]
	}
	r.Path = strings.TrimSuffix(rest, ".git")
	return r
}

// isSCP reports whether s is scp-style host:path, which git recognises by a
// colon before the first slash. A lone letter followed by ":/" or ":\" is a
// Windows drive (C:/src); any other lone letter is a host, such as an ssh
// alias (x:owner/name).
func isSCP(s string) bool {
	colon := strings.Index(s, ":")
	slash := strings.Index(s, "/")
	if colon < 1 || (slash >= 0 && slash < colon) {
		return false
	}
	afterColon := s[colon+1:]
	drive := colon == 1 && (strings.HasPrefix(afterColon, "/") || strings.HasPrefix(afterColon, `\`))
	return !drive
}

// GitHubSlug is owner/name when r points at github.com, else "".
func (r Remote) GitHubSlug() string {
	if r.Host != "github.com" {
		return ""
	}
	parts := strings.SplitN(r.Path, "/", 3)
	if len(parts) < 2 {
		return ""
	}
	return parts[0] + "/" + parts[1]
}

// WebURL is the repo's browser page: http(s) remotes keep their scheme and
// ssh/scp remotes become https. It's "" for other schemes and local paths.
func (r Remote) WebURL() string {
	scheme := r.Scheme
	switch scheme {
	case "http", "https":
	case "ssh", "":
		scheme = "https"
	default:
		return ""
	}
	if r.Host == "" {
		return ""
	}
	if r.Path == "" {
		return scheme + "://" + r.Host
	}
	return scheme + "://" + r.Host + "/" + r.Path
}

// ghShorthand is owner/name when r is bare GitHub shorthand rather than a
// URL or a path, else "".
func (r Remote) ghShorthand() string {
	if r.Scheme != "" || r.Host != "" || strings.Contains(r.Path, ":") {
		return ""
	}
	owner, name, ok := strings.Cut(r.Path, "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		return ""
	}
	return r.Path
}

// CloneDest works out where a clone URL (or owner/name) lands under root:
// <root>/<owner>/<name>, like gitkeeper. Nested namespaces keep only the last
// two path segments. ghSlug is set when the clone should go through gh.
func CloneDest(root, url string) (dest string, ghSlug string, err error) {
	if strings.TrimSpace(url) == "" {
		return "", "", errors.New("empty URL")
	}
	r := ParseRemote(url)
	if s := r.GitHubSlug(); s != "" {
		return filepath.Join(root, filepath.FromSlash(s)), "", nil
	}
	// owner/name shorthand goes through gh, which honours its git protocol.
	if s := r.ghShorthand(); s != "" {
		return filepath.Join(root, filepath.FromSlash(s)), s, nil
	}
	path := strings.TrimSuffix(r.Path, "/")
	// A URL's host is the first segment even when it's empty, as in
	// file:///repo.git, so a one-segment path still lands in <root>/repo.
	if r.Host != "" || r.Scheme != "" {
		path = r.Host + "/" + path
	}
	parts := strings.Split(path, "/")
	if r.Path == "" || len(parts) < 2 {
		return "", "", fmt.Errorf("can't tell where %q should go", url)
	}
	return filepath.Join(root, parts[len(parts)-2], parts[len(parts)-1]), "", nil
}
