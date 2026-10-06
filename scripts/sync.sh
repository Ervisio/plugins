#!/usr/bin/env bash
# sync.sh [ID]: for every plugin in registry.json (or only ID), look at the
# latest release of its repository. When it is newer than plugins/<id>.json,
# download and validate it, write the new plugins/<id>.json on a branch
# sync/<id>-<version> and open a pull request with the permission summary.
#
# DRY_RUN=1 validates and prints the summary without pushing anything.
# A version that already has a pull request (open, merged or closed) is not
# proposed again.
set -euo pipefail
# shellcheck source=scripts/lib.sh
. "$(dirname "$0")/lib.sh"
cd "$ROOT"

only="${1:-}"
regtool check >/dev/null || { regtool check; exit 1; }
failed=0

sync_one() {
  local id="$1" repo="$2" trust="$3"
  local tag version current branch
  # Without a tag, gh shows the latest release (never a draft or a pre-release).
  if ! tag="$(gh release view --repo "$repo" --json tagName --jq .tagName 2>/dev/null)"; then
    note "$id: $repo has no release yet"
    return 0
  fi
  version="${tag#v}"
  [[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || { note "$id: latest tag $tag is not vX.Y.Z, skipped"; return 0; }
  current=""
  if [ -f "plugins/$id.json" ]; then
    current="$(regtool field "plugins/$id.json" version)"
    if ! regtool semver-gt "$version" "$current"; then
      note "$id: $current is current ($repo latest is $tag)"
      return 0
    fi
  fi
  branch="sync/$id-$version"
  if [ "${DRY_RUN:-}" != 1 ]; then
    if [ -n "$(gh pr list --repo "$REGISTRY_REPO" --state all --head "$branch" --json number --jq '.[].number')" ]; then
      note "$id $version: a pull request for $branch exists already"
      return 0
    fi
  fi
  note "$id: ${current:-none} -> $version ($repo $tag, trust $trust)"
  local asset="$id-$version.tar.gz" dl="$WORK/dl/$id" sum
  sum="$(fetch_release "$repo" "$tag" "$asset" "$dl")"
  safe_unpack "$dl/$asset" "$id" "$WORK/pkg"
  validate_dir "$WORK/pkg/$id"
  gh release view "$tag" --repo "$repo" --json body --jq .body > "$WORK/notes-$id.md"
  regtool sync-entry -id "$id" -tag "$tag" -asset "$asset" -sha256 "$sum" -notes "$WORK/notes-$id.md" \
    -dir "$WORK/pkg/$id" -body "$WORK/body-$id.md" -labels "$WORK/labels-$id"
  regtool check >/dev/null
  if [ "${DRY_RUN:-}" = 1 ]; then
    cat "$WORK/body-$id.md"
    git checkout -q -- "plugins/$id.json" 2>/dev/null || rm -f "plugins/$id.json"
    return 0
  fi
  # Team plugins whose update asks for no new permissions are published at
  # once: committed to main, then the Publish workflow is started (a push
  # made with GITHUB_TOKEN does not start workflows by itself). The first
  # version of a plugin and any update with new permissions still need a
  # reviewed pull request.
  if [ "$trust" = team ] && [ -n "$current" ] && ! grep -qx new-permissions "$WORK/labels-$id" && [ "${AUTO_PUBLISH:-1}" = 1 ]; then
    git fetch -q origin main
    git checkout -q -B "auto/$id" origin/main
    git add "plugins/$id.json"
    git -c user.name="github-actions[bot]" -c user.email="41898282+github-actions[bot]@users.noreply.github.com" \
      commit -q -m "Update $id to $version" -m "From $repo $tag ($asset, sha256 $sum). Team plugin, no new permissions: published without review."
    if git push -q origin "HEAD:main"; then
      note "$id $version: no new permissions, published directly"
      if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
        { echo "### $id $version published (team plugin, no new permissions)"; cat "$WORK/body-$id.md"; } >> "$GITHUB_STEP_SUMMARY"
      fi
      git fetch -q origin main
      git checkout -q main && git reset -q --hard origin/main
      : > "$WORK/publish-needed"   # sync_one runs in a subshell
      return 0
    fi
    note "$id $version: could not push to main (protected?), opening a pull request instead"
    git checkout -q main
  fi
  git checkout -q -B "$branch" origin/main
  git add "plugins/$id.json"
  git -c user.name="github-actions[bot]" -c user.email="41898282+github-actions[bot]@users.noreply.github.com" \
    commit -q -m "Update $id to $version" -m "From $repo $tag ($asset, sha256 $sum)."
  git push -q -f origin "$branch"
  local labels=()
  while IFS= read -r l; do
    [ -n "$l" ] || continue
    case "$l" in
      new-permissions) gh label create new-permissions --repo "$REGISTRY_REPO" --color D93F0B --description "The update asks for new or wider permissions" --force >/dev/null ;;
      plugin-update) gh label create plugin-update --repo "$REGISTRY_REPO" --color 1D76DB --description "A new plugin version from the sync workflow" --force >/dev/null ;;
    esac
    labels+=(--label "$l")
  done < "$WORK/labels-$id"
  local title="Update $id to $version"
  [ -n "$current" ] || title="Add $id $version"
  if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then cat "$WORK/body-$id.md" >> "$GITHUB_STEP_SUMMARY"; fi
  if ! gh pr create --repo "$REGISTRY_REPO" --base main --head "$branch" --title "$title" --body-file "$WORK/body-$id.md" "${labels[@]}"; then
    git checkout -q main
    die "branch $branch was pushed but the pull request could not be opened. If GitHub says Actions may not create pull requests, allow it in the organization and repository settings (Actions > General > Workflow permissions), or open it by hand: gh pr create --repo $REGISTRY_REPO --head $branch (the summary is in the job summary)"
  fi
  git checkout -q main
}

while read -r -u 3 id repo trust; do
  [ -z "$only" ] || [ "$only" = "$id" ] || continue
  # One failing plugin must not block the others.
  set +e
  (set -e; sync_one "$id" "$repo" "$trust")
  rc=$?
  set -e
  if [ "$rc" -ne 0 ]; then
    echo "::error::sync of $id failed"
    failed=1
    git checkout -q main 2>/dev/null || true
  fi
done 3< <(regtool list)
if [ -f "$WORK/publish-needed" ] && [ "${DRY_RUN:-}" != 1 ]; then
  gh workflow run publish.yml --repo "$REGISTRY_REPO" --ref main || { echo "::error::could not start the Publish workflow"; failed=1; }
fi
exit "$failed"
