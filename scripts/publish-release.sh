#!/usr/bin/env bash
# One-shot GitHub Release publisher for this host (scheme A: root only).
#
# Default path: push main/tag (if needed) → local GoReleaser build + upload
# (does not rely on GitHub Actions minutes / billing).
#
# Usage (as root):
#   export GITHUB_TOKEN=ghp_...    # repo scope; never commit
#   cd /home/yyze/projects/AutoZeAgent
#   ./scripts/publish-release.sh v0.1.0
#   ./scripts/publish-release.sh v0.1.0 --commit-paths release
#   ./scripts/publish-release.sh v0.1.0 --upload-only      # tag already on HEAD
#   ./scripts/publish-release.sh v0.1.0 --via-actions      # tag push only (needs Actions)
#   ./scripts/publish-release.sh v0.1.0 --dry-run
#
# Requires: git, make, goreleaser. GITHUB_TOKEN for local upload (default).
set -euo pipefail

REPO_DEFAULT="/home/yyze/projects/AutoZeAgent"
REMOTE="origin"
BRANCH="main"
GIT_USER_NAME="yyZe"
GIT_USER_EMAIL="yyze@debianze.local"
GITHUB_REPO_SLUG="yyZe0122/AutoZeAgent"

TAG=""
COMMIT_PATHS="" # empty | release | all
DRY_RUN=0
SKIP_CHECK=0
SKIP_SNAPSHOT=0
SNAPSHOT_ONLY=0
UPLOAD_ONLY=0
VIA_ACTIONS=0
FORCE_TAG=0
YES=0
COMMIT_MSG=""
TAG_MSG=""
REPO_DIR=""
PARALLELISM="${GORELEASER_PARALLELISM:-1}"

usage() {
  cat <<'EOF'
One-shot release (root only). Default: local GoReleaser upload via GITHUB_TOKEN.

  ./scripts/publish-release.sh v0.1.0
  ./scripts/publish-release.sh v0.1.0 --commit-paths release
  ./scripts/publish-release.sh v0.1.0 --upload-only
  ./scripts/publish-release.sh v0.1.0 --via-actions
  ./scripts/publish-release.sh v0.1.0 --dry-run

Options:
  --repo DIR            Repository root (default: /home/yyze/projects/AutoZeAgent)
  --commit-paths MODE   release = whitelist; all = full tree (needs --yes)
  --message TEXT        Commit message when committing
  --tag-message TEXT    Annotated tag message
  --skip-check          Skip make check
  --skip-snapshot       Skip snapshot preflight before real release
  --snapshot-only       make check + snapshot only (no tag/upload)
  --upload-only         HEAD already tagged; only goreleaser release (no push/tag)
  --via-actions         Push tag only; let GitHub Actions publish (needs billing OK)
  --force-tag           Delete local+remote tag if present, then recreate
  --dry-run             Print steps only
  --yes                 Confirm dangerous modes
  -h, --help            This help

Environment:
  GITHUB_TOKEN          Required for default local upload (repo write / contents)
  GITHUB_REPOSITORY     Optional owner/name (default yyZe0122/AutoZeAgent)
  GORELEASER_PARALLELISM  Default 1
EOF
}

log() { printf '==> %s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }
run() {
  if [[ "$DRY_RUN" -eq 1 ]]; then
    printf '[dry-run] %s\n' "$*"
    return 0
  fi
  # shellcheck disable=SC2086
  eval "$@"
}

find_goreleaser() {
  if command -v goreleaser >/dev/null 2>&1; then
    command -v goreleaser
    return 0
  fi
  local c
  for c in /home/yyze/go/bin/goreleaser "${HOME}/go/bin/goreleaser" /usr/local/bin/goreleaser; do
    if [[ -x "$c" ]]; then
      echo "$c"
      return 0
    fi
  done
  return 1
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    -h|--help) usage; exit 0 ;;
    --repo) REPO_DIR=${2:-}; shift 2 ;;
    --commit-paths) COMMIT_PATHS=${2:-}; shift 2 ;;
    --message) COMMIT_MSG=${2:-}; shift 2 ;;
    --tag-message) TAG_MSG=${2:-}; shift 2 ;;
    --skip-check) SKIP_CHECK=1; shift ;;
    --skip-snapshot) SKIP_SNAPSHOT=1; shift ;;
    --snapshot-only) SNAPSHOT_ONLY=1; shift ;;
    --upload-only) UPLOAD_ONLY=1; shift ;;
    --via-actions) VIA_ACTIONS=1; shift ;;
    --force-tag) FORCE_TAG=1; shift ;;
    --dry-run) DRY_RUN=1; shift ;;
    --yes) YES=1; shift ;;
    -*)
      die "unknown option: $1"
      ;;
    *)
      if [[ -z "$TAG" ]]; then
        TAG=$1
        shift
      else
        die "unexpected argument: $1"
      fi
      ;;
  esac
done

[[ -n "$TAG" ]] || { usage >&2; die "tag required (e.g. v0.1.0)"; }

if [[ "$(id -u)" -ne 0 ]]; then
  die "must run as root (scheme A: only root commits/pushes/tags)"
fi

[[ "$TAG" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]] \
  || die "tag must look like v0.1.0 or v0.1.0-alpha.1 (got: $TAG)"

if [[ "$SNAPSHOT_ONLY" -eq 1 && "$UPLOAD_ONLY" -eq 1 ]]; then
  die "use only one of --snapshot-only / --upload-only"
fi
if [[ "$VIA_ACTIONS" -eq 1 && "$UPLOAD_ONLY" -eq 1 ]]; then
  die "--via-actions and --upload-only are mutually exclusive"
fi

REPO_DIR=${REPO_DIR:-$REPO_DEFAULT}
cd "$REPO_DIR" || die "cannot cd $REPO_DIR"
git rev-parse --is-inside-work-tree >/dev/null 2>&1 || die "not a git repository: $REPO_DIR"

NOTES="docs/changelog/${TAG}.md"
[[ -f "$NOTES" ]] || die "missing release notes: $NOTES (create before publishing)"

current_branch=$(git rev-parse --abbrev-ref HEAD)
[[ "$current_branch" == "$BRANCH" ]] || die "on branch '$current_branch', expected '$BRANCH'"

export GITHUB_REPOSITORY="${GITHUB_REPOSITORY:-$GITHUB_REPO_SLUG}"

# Local git identity (this repo only)
if [[ -z "$(git config --local user.name 2>/dev/null || true)" ]]; then
  log "set local user.name=$GIT_USER_NAME"
  run "git config user.name \"$GIT_USER_NAME\""
fi
if [[ -z "$(git config --local user.email 2>/dev/null || true)" ]]; then
  log "set local user.email=$GIT_USER_EMAIL"
  run "git config user.email \"$GIT_USER_EMAIL\""
fi

assert_no_secrets_staged() {
  local bad
  bad=$(git diff --cached --name-only 2>/dev/null | grep -iE 'local\.json$|\.db$|\.jsonl$|permissions-trust|credentials\.json|\.pem$|id_rsa|id_ed25519|\.env$' || true)
  if [[ -n "$bad" ]]; then
    printf '%s\n' "$bad" >&2
    die "refusing to commit sensitive paths (see above)"
  fi
}

commit_release_paths() {
  log "stage release whitelist"
  local paths=(
    .goreleaser.yaml
    .github/workflows/release.yml
    docs/changelog/
    docs/release.md
    packaging/scripts/
    README.md
    scripts/publish-release.sh
    internal/daemonctl/process_windows.go
  )
  local p
  for p in "${paths[@]}"; do
    if [[ -e "$p" ]] || git ls-files --error-unmatch "$p" >/dev/null 2>&1; then
      run "git add -- \"$p\""
    fi
  done
  assert_no_secrets_staged
  if git diff --cached --quiet 2>/dev/null; then
    log "nothing to commit on release whitelist"
    return 0
  fi
  local msg=${COMMIT_MSG:-"release: prepare ${TAG} pipeline"}
  if [[ "$DRY_RUN" -eq 1 ]]; then
    log "would commit: $msg"
    git diff --cached --stat || true
    return 0
  fi
  git commit -m "$msg"
  log "committed release paths"
}

commit_all_paths() {
  [[ "$YES" -eq 1 ]] || die "--commit-paths all requires --yes"
  log "stage all changes (tracked + untracked, respect gitignore)"
  run "git add -A"
  assert_no_secrets_staged
  if git diff --cached --quiet 2>/dev/null; then
    log "nothing to commit"
    return 0
  fi
  local msg=${COMMIT_MSG:-"release: prepare ${TAG}"}
  if [[ "$DRY_RUN" -eq 1 ]]; then
    log "would commit all: $msg"
    git diff --cached --stat || true
    return 0
  fi
  git commit -m "$msg"
}

case "$COMMIT_PATHS" in
  "") ;;
  release) commit_release_paths ;;
  all) commit_all_paths ;;
  *) die "--commit-paths must be 'release' or 'all'" ;;
esac

# Dirty tree (snapshot-only may still want clean for goreleaser; enforce for publish)
if [[ "$SNAPSHOT_ONLY" -eq 0 ]]; then
  dirty=$(git status --porcelain --untracked-files=normal 2>/dev/null || true)
  if [[ -n "$dirty" ]]; then
    printf '%s\n' "$dirty" >&2
    die "working tree not clean; commit first or use --commit-paths release|all"
  fi
fi

if [[ "$SKIP_CHECK" -eq 0 ]]; then
  log "make check"
  if [[ "$DRY_RUN" -eq 1 ]]; then
    log "would run: make check"
  else
    make check
  fi
else
  log "skip make check"
fi

GR=""
if GR=$(find_goreleaser); then
  :
else
  GR=""
fi

if [[ "$SKIP_SNAPSHOT" -eq 0 && -n "$GR" ]]; then
  log "goreleaser snapshot preflight ($GR)"
  if [[ "$DRY_RUN" -eq 1 ]]; then
    log "would run: $GR release --snapshot --clean --parallelism ${PARALLELISM}"
  else
    "$GR" release --snapshot --clean --parallelism "$PARALLELISM"
    log "snapshot archives:"
    ls -1 dist/autozeagent_* 2>/dev/null | head -20 || true
  fi
elif [[ "$SKIP_SNAPSHOT" -eq 0 ]]; then
  log "goreleaser not found; skip snapshot preflight"
fi

if [[ "$SNAPSHOT_ONLY" -eq 1 ]]; then
  log "snapshot-only done (no tag/upload)"
  exit 0
fi

ensure_tag_on_head() {
  local head tag_commit
  head=$(git rev-parse HEAD)
  if git rev-parse -q --verify "refs/tags/${TAG}" >/dev/null 2>&1; then
    tag_commit=$(git rev-parse "${TAG}^{}")
    if [[ "$tag_commit" != "$head" ]]; then
      if [[ "$FORCE_TAG" -eq 1 ]]; then
        [[ "$YES" -eq 1 ]] || die "tag ${TAG} points elsewhere; --force-tag requires --yes"
        log "move local tag ${TAG} to HEAD"
        run "git tag -d \"$TAG\""
      else
        die "local tag ${TAG} points to $tag_commit, HEAD is $head (use --force-tag --yes)"
      fi
    else
      log "local tag ${TAG} already on HEAD"
      return 0
    fi
  fi
  TAG_MSG=${TAG_MSG:-"AutoZeAgent ${TAG}"}
  log "create annotated tag ${TAG}"
  run "git tag -a \"$TAG\" -m \"$TAG_MSG\""
}

push_main_and_tag() {
  log "push ${BRANCH} to ${REMOTE}"
  run "git push \"$REMOTE\" \"$BRANCH\""

  if git ls-remote --tags "$REMOTE" "refs/tags/${TAG}" 2>/dev/null | grep -q .; then
    remote_commit=$(git ls-remote --tags "$REMOTE" "refs/tags/${TAG}" | awk '{print $1}' | head -1)
    # annotated tags show peeled in ls-remote with ^{}; compare peeled if possible
    if [[ "$FORCE_TAG" -eq 1 ]]; then
      [[ "$YES" -eq 1 ]] || die "remote tag exists; --force-tag requires --yes"
      log "delete remote tag ${TAG}"
      run "git push \"$REMOTE\" \":refs/tags/${TAG}\""
    else
      # If remote tag already exists, still push only if we recreated local — require force to replace
      log "remote tag ${TAG} already exists (leave in place; re-upload uses same tag)"
    fi
  fi

  # Ensure remote has our tag
  if ! git ls-remote --tags "$REMOTE" "refs/tags/${TAG}" 2>/dev/null | grep -q .; then
    log "push tag ${TAG}"
    run "git push \"$REMOTE\" \"$TAG\""
  elif [[ "$FORCE_TAG" -eq 1 ]]; then
    log "push tag ${TAG} (after force delete)"
    run "git push \"$REMOTE\" \"$TAG\""
  else
    # verify remote points at same commit as local
    local_peeled=$(git rev-parse "${TAG}^{}")
    # fetch remote peeled is hard without network object; push --force only with force-tag
    log "remote tag ${TAG} present; not force-updating (use --force-tag --yes to replace)"
  fi
}

if [[ "$UPLOAD_ONLY" -eq 0 ]]; then
  ensure_tag_on_head
  push_main_and_tag
else
  log "upload-only: skip push main"
  if ! git rev-parse -q --verify "refs/tags/${TAG}" >/dev/null 2>&1; then
    die "upload-only requires local tag ${TAG}"
  fi
  head=$(git rev-parse HEAD)
  tag_commit=$(git rev-parse "${TAG}^{}")
  [[ "$head" == "$tag_commit" ]] || die "HEAD ($head) != ${TAG} ($tag_commit)"
fi

VER_NUM=${TAG#v}

if [[ "$VIA_ACTIONS" -eq 1 ]]; then
  log "via-actions: tag pushed; GitHub Actions must build (needs billing OK)"
  cat <<EOF

==> tag ${TAG} pushed for Actions

  Actions:  https://github.com/${GITHUB_REPOSITORY}/actions
  Release:  https://github.com/${GITHUB_REPOSITORY}/releases/tag/${TAG}

  If you only see Source code zip/tar.gz, the Release workflow failed
  (e.g. billing lock). Prefer default local upload instead of --via-actions.

  Expected assets after a green Release job:
    autozeagent_${VER_NUM}_linux_amd64.tar.gz
    autozeagent_${VER_NUM}_linux_arm64.tar.gz
    autozeagent_${VER_NUM}_darwin_amd64.tar.gz
    autozeagent_${VER_NUM}_darwin_arm64.tar.gz
    autozeagent_${VER_NUM}_windows_amd64.zip
    autozeagent_${VER_NUM}_windows_arm64.zip
    checksums.txt
EOF
  exit 0
fi

# --- Default: local GoReleaser upload ---
[[ -n "$GR" ]] || GR=$(find_goreleaser) || die "goreleaser not found (install: go install github.com/goreleaser/goreleaser/v2@latest)"

if [[ -z "${GITHUB_TOKEN:-}" ]]; then
  die "GITHUB_TOKEN is required for local upload (PAT with repo/contents write). export GITHUB_TOKEN=... then re-run. Or use --via-actions if Actions billing works."
fi

log "local goreleaser release + upload ($GR)"
if [[ "$DRY_RUN" -eq 1 ]]; then
  log "would run: GITHUB_TOKEN=*** $GR release --clean --parallelism ${PARALLELISM} --release-notes=${NOTES}"
else
  # Tag must be reachable; goreleaser uses git describe
  git describe --tags --exact-match HEAD >/dev/null 2>&1 \
    || die "HEAD is not exactly tag ${TAG}; checkout the tagged commit"
  "$GR" release --clean --parallelism "$PARALLELISM" --release-notes="$NOTES"
fi

cat <<EOF

==> local publish finished for ${TAG}

  Release:  https://github.com/${GITHUB_REPOSITORY}/releases/tag/${TAG}

  Assets (Pre-release):
    autozeagent_${VER_NUM}_linux_amd64.tar.gz
    autozeagent_${VER_NUM}_linux_arm64.tar.gz
    autozeagent_${VER_NUM}_darwin_amd64.tar.gz
    autozeagent_${VER_NUM}_darwin_arm64.tar.gz
    autozeagent_${VER_NUM}_windows_amd64.zip
    autozeagent_${VER_NUM}_windows_arm64.zip
    checksums.txt

  Body: ${NOTES}
  Install: AUTOZEAGENT_VERSION=${TAG}

  unset GITHUB_TOKEN   # when done
EOF
