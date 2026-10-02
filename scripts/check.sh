#!/usr/bin/env bash
# check.sh: the pull request check. Validates registry.json and
# plugins/*.json; for every plugins/<id>.json changed against BASE (a git
# ref, default origin/main) it downloads the upstream package named there,
# checks upstreamSha256, validates the manifest with the core's rules and
# verifies that the package declares exactly the reviewed manifest. For a
# plugin listed in registry.json without plugins/<id>.json it validates the
# latest release of its repository and prints the summary a sync would open.
# Needs no secrets.
set -euo pipefail
# shellcheck source=scripts/lib.sh
. "$(dirname "$0")/lib.sh"
cd "$ROOT"

BASE="${BASE:-origin/main}"
regtool check

changed="$(git diff --name-only "$BASE"...HEAD -- plugins/ 2>/dev/null || git ls-files plugins/)"
for f in $changed; do
  [ -f "$f" ] || { note "$f removed: the plugin leaves the catalog"; continue; }
  id="$(basename "$f" .json)"
  repo="$(regtool field "$f" repo)"
  tag="$(regtool field "$f" tag)"
  asset="$(regtool field "$f" asset)"
  want="$(regtool field "$f" upstreamSha256)"
  note "$id: checking $repo $tag"
  fetch_release "$repo" "$tag" "$asset" "$WORK/dl/$id" "$want" >/dev/null
  safe_unpack "$WORK/dl/$id/$asset" "$id" "$WORK/pkg"
  validate_dir "$WORK/pkg/$id"
  regtool compare -id "$id" -dir "$WORK/pkg/$id"
done

while read -r -u 3 id repo trust; do
  [ ! -f "plugins/$id.json" ] || continue
  note "$id: new source $repo ($trust), no reviewed version yet"
  if ! tag="$(gh release view --repo "$repo" --json tagName --jq .tagName 2>/dev/null)"; then
    note "$id: $repo has no release yet; the sync workflow proposes the first one when it exists"
    continue
  fi
  version="${tag#v}"
  asset="$id-$version.tar.gz"
  sum="$(fetch_release "$repo" "$tag" "$asset" "$WORK/dl/$id")"
  safe_unpack "$WORK/dl/$id/$asset" "$id" "$WORK/pkg"
  validate_dir "$WORK/pkg/$id"
  gh release view "$tag" --repo "$repo" --json body --jq .body > "$WORK/notes-$id.md"
  regtool sync-entry -write=false -id "$id" -tag "$tag" -asset "$asset" -sha256 "$sum" -notes "$WORK/notes-$id.md" \
    -dir "$WORK/pkg/$id" -body "$WORK/body-$id.md"
  echo "Summary the sync workflow will open for $id after merge:"
  cat "$WORK/body-$id.md"
  if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then cat "$WORK/body-$id.md" >> "$GITHUB_STEP_SUMMARY"; fi
done 3< <(regtool list)
echo "check passed"
