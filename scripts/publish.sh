#!/usr/bin/env bash
# publish.sh SITE_DIR: signs every reviewed plugin version that has no
# signed release yet, releases it in this repository (tag <id>-<version>),
# then writes SITE_DIR/catalog.json, catalog.sig and index.html.
#
# KEY_FILE      the team private key file (mode 0600). Required.
# TEAM_PUB_FILE public key the result must verify against (default team.pub).
# VERIFY_PUB    local tests only: verify plugins with this key instead of
#               the one embedded in plugin-sign.
# SKIP_RELEASE=1 local tests only: never call gh release (sign everything).
set -euo pipefail
# shellcheck source=scripts/lib.sh
. "$(dirname "$0")/lib.sh"
cd "$ROOT"

SITE="${1:?usage: publish.sh SITE_DIR}"
if [ -z "${KEY_FILE:-}" ] || [ ! -s "$KEY_FILE" ]; then die "KEY_FILE is not set or empty (the PLUGIN_SIGNING_KEY secret; see README)"; fi
TEAM_PUB_FILE="${TEAM_PUB_FILE:-$ROOT/team.pub}"
SIGNED="$WORK/signed"
mkdir -p "$SITE" "$SIGNED"
regtool check

verify_plugin() {
  if [ -n "${VERIFY_PUB:-}" ]; then
    plugin_sign -verify -pub "$VERIFY_PUB" "$1"
  else
    plugin_sign -verify "$1"   # the team key embedded in the core
  fi
}

while read -r -u 3 id repo _trust; do
  f="plugins/$id.json"
  [ -f "$f" ] || { note "$id: no reviewed version yet, not in the catalog"; continue; }
  version="$(regtool field "$f" version)"
  tag="$id-$version"
  name="$tag.tar.gz"
  if [ "${SKIP_RELEASE:-}" != 1 ] && gh release view "$tag" --repo "$REGISTRY_REPO" >/dev/null 2>&1; then
    note "$id $version: signed release $tag exists"
    rm -f "$SIGNED/$name"
    gh release download "$tag" --repo "$REGISTRY_REPO" --pattern "$name" --dir "$SIGNED" || die "cannot download $name from $tag"
    safe_unpack "$SIGNED/$name" "$id" "$SIGNED"
    verify_plugin "$SIGNED/$id" || die "$name in release $tag does not verify"
    regtool compare -id "$id" -dir "$SIGNED/$id"
    continue
  fi
  note "$id $version: signing"
  utag="$(regtool field "$f" tag)"
  asset="$(regtool field "$f" asset)"
  want="$(regtool field "$f" upstreamSha256)"
  fetch_release "$repo" "$utag" "$asset" "$WORK/dl/$id" "$want" >/dev/null
  safe_unpack "$WORK/dl/$id/$asset" "$id" "$SIGNED"
  [ ! -e "$SIGNED/$id/manifest.sig" ] || die "$asset is already signed; the registry signs packages itself"
  regtool compare -id "$id" -dir "$SIGNED/$id"
  plugin_sign -key "$KEY_FILE" "$SIGNED/$id"
  verify_plugin "$SIGNED/$id" || die "$id: the signature does not verify against the team key: is PLUGIN_SIGNING_KEY the team key?"
  regtool compare -id "$id" -dir "$SIGNED/$id"
  mtime="$(git log -1 --format=%ct -- "$f" 2>/dev/null || true)"
  repack "$SIGNED" "$id" "$SIGNED/$name" "${mtime:-0}"
  sum="$(sha256sum "$SIGNED/$name" | cut -d' ' -f1)"
  if [ "${SKIP_RELEASE:-}" != 1 ]; then
    {
      regtool field "$f" notes
      echo
      echo "Signed with the Ervisio team key from [$repo $utag](https://github.com/$repo/releases/tag/$utag) (upstream sha256 \`$want\`)."
      echo
      echo "\`$name\` sha256: \`$sum\`"
    } > "$WORK/relnotes-$id.md"
    disp="$(regtool field "$SIGNED/$id/manifest.json" name)"
    gh release create "$tag" "$SIGNED/$name" --repo "$REGISTRY_REPO" --title "${disp:-$id} $version" \
      --notes-file "$WORK/relnotes-$id.md" --latest=false
  fi
done 3< <(regtool list)

regtool catalog -signed "$SIGNED" -out "$SITE/catalog.json"
regtool sign-catalog -key "$KEY_FILE" "$SITE/catalog.json"
regtool verify-catalog -pubfile "$TEAM_PUB_FILE" "$SITE/catalog.json" "$SITE/catalog.sig" \
  || die "catalog.sig does not verify with $(basename "$TEAM_PUB_FILE"): is PLUGIN_SIGNING_KEY the team key?"
# Cross-check with the core's own verifier when the plugin-sign of CORE_REF
# has catalog support (the regtool check above is the authoritative one).
help="$(plugin_sign -h 2>&1 || true)"
if grep -q -- '-catalog' <<< "$help"; then
  if [ -n "${VERIFY_PUB:-}" ]; then
    xcheck=(plugin_sign -verify -pub "$VERIFY_PUB" -catalog "$SITE/catalog.json")
  else
    xcheck=(plugin_sign -verify -catalog "$SITE/catalog.json")
  fi
  if ! "${xcheck[@]}"; then
    echo "::warning::the core's plugin-sign -verify -catalog did not accept the catalog; check that its catalog format still matches regtool"
  fi
fi
regtool site -catalog "$SITE/catalog.json" -out "$SITE/index.html"
ls -l "$SITE"
