#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  prepare-pages-worktree.sh TARGET_DIR [BRANCH]

Prepare a local git worktree for the publication branch used by GitHub Pages.
If the branch does not exist yet, an orphan branch is initialized.
EOF
}

[[ $# -ge 1 && $# -le 2 ]] || {
  usage
  exit 1
}

target_dir="$1"
branch="${2:-gh-pages}"

rm -rf "${target_dir}"

git fetch origin "${branch}" >/dev/null 2>&1 || true

if git show-ref --verify --quiet "refs/remotes/origin/${branch}"; then
  git worktree add -B "${branch}" "${target_dir}" "origin/${branch}"
else
  git worktree add --detach "${target_dir}"
  (
    cd "${target_dir}"
    git checkout --orphan "${branch}"
    find . -mindepth 1 -maxdepth 1 ! -name .git -exec rm -rf {} +
  )
fi
