#!/usr/bin/env bash
# Runs from trusted main after CI/Check. PR objects are data, never executable.
set -euo pipefail
: "${GH_TOKEN:?}" "${GITHUB_REPOSITORY:?}" "${HEAD_SHA:?}"
[[ "$HEAD_SHA" =~ ^[0-9a-f]{40}$ ]] || exit 1
while read -r -u 3 number; do
  [ -n "$number" ] || continue
  pr="$(gh api "repos/$GITHUB_REPOSITORY/pulls/$number")"
  [ "$(jq -r .state <<< "$pr")" = open ] || continue
  [ "$(jq -r .user.login <<< "$pr")" = 'github-actions[bot]' ] || continue
  [ "$(jq -r .head.repo.full_name <<< "$pr")" = "$GITHUB_REPOSITORY" ] || continue
  [ "$(jq -r .base.ref <<< "$pr")" = main ] || continue
  [ "$(jq -r .head.sha <<< "$pr")" = "$HEAD_SHA" ] || continue
  branch="$(jq -r .head.ref <<< "$pr")"
  [[ "$branch" == sync/* ]] || continue
  base="$(gh api "repos/$GITHUB_REPOSITORY/git/ref/heads/main" --jq .object.sha)"
  git fetch -q origin "$base" "$HEAD_SHA"
  if ! python3 scripts/review-policy.py "$base" "$HEAD_SHA"; then continue; fi
  if [ "$(gh pr view "$number" --repo "$GITHUB_REPOSITORY" --json mergeStateStatus --jq .mergeStateStatus)" = BEHIND ]; then
    # Strict branch protection may require a fresh base after another plugin
    # merged. Refresh the bot branch and rerun checks; never merge stale checks.
    gh api --method PUT "repos/$GITHUB_REPOSITORY/pulls/$number/update-branch" -f expected_head_sha="$HEAD_SHA"
    gh workflow run ci.yml --repo "$GITHUB_REPOSITORY" --ref "$branch"
    gh workflow run check.yml --repo "$GITHUB_REPOSITORY" --ref "$branch" -f "base=$base"
    continue
  fi
  checks="$(gh api "repos/$GITHUB_REPOSITORY/commits/$HEAD_SHA/check-runs" --paginate --jq '.check_runs[] | [.name, .status, .conclusion, (.app.id|tostring), .html_url] | @tsv')"
  # Both required checks must have completed successfully on this exact head.
  for name in check 'regtool (gofmt, go vet, go test)'; do
    if ! awk -F '\t' -v name="$name" '$1==name && $4=="15368" {seen=1; if ($2!="completed" || $3!="success") bad=1} END {exit (!seen || bad)}' <<< "$checks"; then
      echo "PR #$number: waiting for $name"; continue 2
    fi
  done
  # workflow_dispatch checks are not attached to the bot-created PR.
  # Report their successful results as commit statuses, only after all of
  # the exact-head checks and the trusted policy above have passed.
  for name in check 'regtool (gofmt, go vet, go test)'; do
    target="$(awk -F '\t' -v name="$name" '$1==name && $4=="15368" {print $5; exit}' <<< "$checks")"
    [ -n "$target" ] || target="https://github.com/$GITHUB_REPOSITORY/commit/$HEAD_SHA/checks"
    gh api --method POST "repos/$GITHUB_REPOSITORY/statuses/$HEAD_SHA" \
      -f state=success -f "context=$name" -f "target_url=$target" \
      -f description="Registry validation passed on this exact commit" >/dev/null
  done
  if gh api --method PUT "repos/$GITHUB_REPOSITORY/pulls/$number/merge" \
      -f sha="$HEAD_SHA" -f merge_method=squash --jq .merged | grep -qx true; then
    # A merge with GITHUB_TOKEN does not fire push workflows.
    gh workflow run publish.yml --repo "$GITHUB_REPOSITORY" --ref main
    echo "Merged and queued Publish for #$number" >> "${GITHUB_STEP_SUMMARY:-/dev/stdout}"
  else
    echo "::warning::PR #$number passed validation but repository rules require a maintainer. Allow the publishing bot to merge routine team updates to make this fully automatic."
  fi
done 3< <(gh api "repos/$GITHUB_REPOSITORY/commits/$HEAD_SHA/pulls" --paginate --jq '.[].number')
