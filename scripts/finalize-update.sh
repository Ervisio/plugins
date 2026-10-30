#!/usr/bin/env bash
# Called by Sync, which already has registry write permissions.
# Keep ordinary CI read-only; never execute code from the proposed package.
set -euo pipefail
: "${GH_TOKEN:?}" "${REGISTRY_REPO:?}"
branch="${1:?sync branch required}"
[[ "$branch" =~ ^sync/[a-z][a-z0-9-]{1,39}-[0-9]+\.[0-9]+\.[0-9]+$ ]] || exit 1
root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
number="$(gh pr view "$branch" --repo "$REGISTRY_REPO" --json number --jq .number)"
deadline=$((SECONDS + ${FINALIZE_TIMEOUT_SECONDS:-600}))

while [ "$SECONDS" -le "$deadline" ]; do
  IFS=$'\t' read -r state head < <(gh pr view "$number" --repo "$REGISTRY_REPO" --json state,headRefOid --jq '[.state,.headRefOid] | @tsv')
  [ "$state" = OPEN ] || exit 0
  [[ "$head" =~ ^[0-9a-f]{40}$ ]] || exit 1
  git fetch -q origin main "$head"
  if ! python3 scripts/review-policy.py origin/main "$head"; then
    echo "PR #$number requires a maintainer review; no automatic merge."
    exit 0
  fi
  checks="$(gh api "repos/$REGISTRY_REPO/commits/$head/check-runs?filter=latest&per_page=100" --paginate --jq '.check_runs[] | [.name,.status,.conclusion,(.app.id|tostring)] | @tsv')"
  waiting=0
  for name in check 'regtool (gofmt, go vet, go test)'; do
    result="$(awk -F '\t' -v name="$name" '
      $1==name && $4=="15368" {seen=1; if ($2!="completed") pending=1; else if ($3!="success") failed=1}
      END {if (failed) print "failed"; else if (!seen || pending) print "pending"; else print "passed"}
    ' <<< "$checks")"
    if [ "$result" = failed ]; then
      echo "::error::PR #$number: $name failed; the version will not be published."
      exit 1
    fi
    [ "$result" = passed ] || waiting=1
  done
  if [ "$waiting" = 0 ]; then
    HEAD_SHA="$head" GITHUB_REPOSITORY="$REGISTRY_REPO" bash scripts/auto-merge.sh
    IFS=$'\t' read -r state next < <(gh pr view "$number" --repo "$REGISTRY_REPO" --json state,headRefOid --jq '[.state,.headRefOid] | @tsv')
    [ "$state" = OPEN ] || exit 0
    if [ "$next" = "$head" ]; then
      echo "::warning::PR #$number is validated but repository rules prevented automatic publication."
      exit 0
    fi
    # The existing finalizer refreshed an outdated branch. Wait for its new checks.
  fi
  sleep 10
done
echo "::error::Timed out waiting for PR #$number checks. The next Sync can resume it."
exit 1
