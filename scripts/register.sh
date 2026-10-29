#!/usr/bin/env bash
# Register a source AND its first reviewed release in one PR.
set -euo pipefail
# shellcheck source=scripts/lib.sh
. "$(dirname "$0")/lib.sh"
cd "$ROOT"
: "${PLUGIN_ID:?}" "${PLUGIN_REPO:?}" "${PLUGIN_CATEGORY:?}"
python3 - <<'PY'
import json, os, re
from pathlib import Path
id, repo, category = (os.environ[k] for k in ('PLUGIN_ID', 'PLUGIN_REPO', 'PLUGIN_CATEGORY'))
if not re.fullmatch(r'[a-z][a-z0-9-]{1,39}', id): raise SystemExit('Invalid plugin id')
if not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9-]{0,38}/[A-Za-z0-9._-]{1,100}', repo): raise SystemExit('Invalid owner/repository')
p = Path('registry.json'); r = json.loads(p.read_text())
if any(x['id'] == id or x['repo'].lower() == repo.lower() for x in r['plugins']):
    raise SystemExit('Plugin already registered: use Sync plugin releases instead')
if category not in [c['id'] for c in r['categories']]: raise SystemExit('Unknown category')
r['plugins'].append(dict(id=id, repo=repo, category=category,
                         trust='team' if repo.split('/')[0].lower() == 'ervisio' else 'community'))
p.write_text(json.dumps(r, indent=2) + '\n')
PY
tag="$(gh release view --repo "$PLUGIN_REPO" --json tagName --jq .tagName)"
[[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "Latest stable release must use vX.Y.Z"
version="${tag#v}"
asset="$PLUGIN_ID-$version.tar.gz"
sum="$(fetch_release "$PLUGIN_REPO" "$tag" "$asset" "$WORK/download")"
safe_unpack "$WORK/download/$asset" "$PLUGIN_ID" "$WORK/pkg"
validate_dir "$WORK/pkg/$PLUGIN_ID"
gh release view "$tag" --repo "$PLUGIN_REPO" --json body --jq .body > "$WORK/notes.md"
regtool sync-entry -id "$PLUGIN_ID" -tag "$tag" -asset "$asset" -sha256 "$sum" \
  -notes "$WORK/notes.md" -dir "$WORK/pkg/$PLUGIN_ID" -body "$WORK/body.md"
regtool check
base="$(git rev-parse HEAD)"
branch="register/$PLUGIN_ID-$version"
if [ -n "$(gh pr list --repo "$REGISTRY_REPO" --state open --head "$branch" --json number --jq '.[].number')" ]; then
  die "A registration PR already exists for $PLUGIN_ID $version"
fi
git checkout -q -b "$branch"
git add registry.json "plugins/$PLUGIN_ID.json"
git -c user.name='github-actions[bot]' -c user.email='41898282+github-actions[bot]@users.noreply.github.com' \
  commit -qm "Register $PLUGIN_ID $version"
git push origin "$branch"
gh pr create --repo "$REGISTRY_REPO" --base main --head "$branch" \
  --title "Register $PLUGIN_ID $version" --body-file "$WORK/body.md"
gh workflow run ci.yml --repo "$REGISTRY_REPO" --ref "$branch"
gh workflow run check.yml --repo "$REGISTRY_REPO" --ref "$branch" -f "base=$base"
cat "$WORK/body.md" >> "${GITHUB_STEP_SUMMARY:-/dev/stdout}"
