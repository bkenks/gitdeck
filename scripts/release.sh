#!/usr/bin/env bash
# Cut a gitdeck release: check the tree, run the tests, tag, push.
#
#   scripts/release.sh patch          v1.0.2 -> v1.0.3
#   scripts/release.sh minor          v1.0.2 -> v1.1.0
#   scripts/release.sh major          v1.0.2 -> v2.0.0
#   scripts/release.sh v1.4.0         explicit version
#   scripts/release.sh patch --dry-run
#
# Pushing the tag is the whole job: .github/workflows/release.yml picks up
# the v* tag, builds every platform and publishes the GitHub release.
set -euo pipefail

cd "$(dirname "$0")/.."

readonly BRANCH=main REMOTE=origin

die() {
  echo "release: $*" >&2
  exit 1
}

spec="${1:-}"
dry_run=false
[ "${2:-}" = "--dry-run" ] && dry_run=true
[ -n "$spec" ] || die "usage: scripts/release.sh patch|minor|major|vX.Y.Z [--dry-run]"

latest="$(git tag --list 'v*' --sort=-v:refname | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | head -1 || true)"
latest="${latest:-v0.0.0}"
IFS=. read -r major minor patch <<<"${latest#v}"

case "$spec" in
patch) next="v${major}.${minor}.$((patch + 1))" ;;
minor) next="v${major}.$((minor + 1)).0" ;;
major) next="v$((major + 1)).0.0" ;;
*)
  [[ "$spec" =~ ^v?[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "$spec is not patch, minor, major or vX.Y.Z"
  next="v${spec#v}"
  ;;
esac

# Refuse to go backwards.
newest="$(printf '%s\n%s\n' "$latest" "$next" | sort -V | tail -1)"
if [ "$next" = "$latest" ] || [ "$newest" != "$next" ]; then
  die "$next is not newer than $latest"
fi

[ -z "$(git status --porcelain)" ] || die "working tree is dirty; commit or stash first"
[ "$(git rev-parse --abbrev-ref HEAD)" = "$BRANCH" ] || die "releases are cut from $BRANCH"
git fetch --quiet "$REMOTE" "$BRANCH"
[ "$(git rev-parse HEAD)" = "$(git rev-parse "$REMOTE/$BRANCH")" ] ||
  die "$BRANCH and $REMOTE/$BRANCH differ; push or pull first"
if git rev-parse -q --verify "refs/tags/$next" >/dev/null; then
  die "tag $next already exists"
fi

echo "Releasing $next (latest was $latest)"
echo "Running checks..."
mise run check

if $dry_run; then
  echo "Dry run: would tag $next and push it to $REMOTE."
  exit 0
fi

git tag -a "$next" -m "gitdeck $next"
git push "$REMOTE" "$next"
echo "Pushed $next. GitHub Actions is building the release:"
echo "  gh run watch \$(gh run list --workflow release.yml --limit 1 --json databaseId --jq '.[0].databaseId')"
