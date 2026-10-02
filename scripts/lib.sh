# Shared helpers for the registry scripts (sync.sh, check.sh, publish.sh).
# shellcheck shell=bash
# Source it from bash with `set -euo pipefail`. Needs: git, go, gh, tar,
# gzip, sha256sum. GH_TOKEN must be set for gh.

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORK="${WORK:-$(mktemp -d)}"
mkdir -p "$WORK/bin"
REGISTRY_REPO="${REGISTRY_REPO:-Ervisio/plugins}"
CORE_REPO="${CORE_REPO:-https://github.com/Ervisio/ervisio.git}"
MAX_ASSET_BYTES=$((32 << 20))   # compressed package, same limit as the consoles
MAX_UNPACKED_KB=$((64 << 10))   # 64 MiB unpacked

die() { echo "::error::$*" >&2; exit 1; }
note() { echo "--- $*" >&2; }

# regtool: built once from tools/regtool.
REGTOOL="$WORK/bin/regtool"
regtool() {
  if [ ! -x "$REGTOOL" ]; then
    (cd "$ROOT/tools/regtool" && CGO_ENABLED=0 go build -o "$REGTOOL" .) || die "cannot build regtool"
  fi
  "$REGTOOL" "$@"
}

# plugin_sign: the core's plugin-sign. $PLUGIN_SIGN overrides (local tests);
# otherwise it is built from Ervisio/ervisio at the ref in CORE_REF.
plugin_sign() {
  if [ -n "${PLUGIN_SIGN:-}" ]; then
    "$PLUGIN_SIGN" "$@"
    return
  fi
  local bin="$WORK/bin/plugin-sign"
  if [ ! -x "$bin" ]; then
    local ref
    ref="$(tr -d '[:space:]' < "$ROOT/CORE_REF")"
    [ -n "$ref" ] || die "CORE_REF is empty"
    note "building plugin-sign from $CORE_REPO at $ref"
    rm -rf "$WORK/core"
    git init -q "$WORK/core"
    git -C "$WORK/core" fetch -q --depth 1 "$CORE_REPO" "$ref" || die "cannot fetch $ref from $CORE_REPO"
    git -C "$WORK/core" checkout -q FETCH_HEAD
    (cd "$WORK/core/server" && CGO_ENABLED=0 go build -o "$bin" ./internal/modules/plugins/cmd/plugin-sign) || die "cannot build plugin-sign"
  fi
  "$bin" "$@"
}

# throwaway_key: a key made for this run, to validate packages with
# plugin-sign without the team key. Prints the public key.
throwaway_pub() {
  if [ ! -s "$WORK/throwaway.pub" ]; then
    rm -f "$WORK/throwaway.key"
    plugin_sign -genkey "$WORK/throwaway.key" | sed -n 's/^public key: *//p' > "$WORK/throwaway.pub"
    [ -s "$WORK/throwaway.pub" ] || die "plugin-sign -genkey printed no public key"
  fi
  cat "$WORK/throwaway.pub"
}

# fetch_release REPO TAG ASSET OUTDIR [EXPECTED_SHA256]
# Downloads ASSET and ASSET.sha256 of a release, checks the size and the
# checksum (against the .sha256 asset and, when given, the expected one).
fetch_release() {
  local repo="$1" tag="$2" asset="$3" out="$4" want="${5:-}"
  mkdir -p "$out"
  local size
  size="$(gh release view "$tag" --repo "$repo" --json assets --jq ".assets[] | select(.name == \"$asset\") | .size")"
  [ -n "$size" ] || die "$repo $tag has no asset $asset"
  [ "$size" -le "$MAX_ASSET_BYTES" ] || die "$repo $tag: $asset is larger than 32 MiB"
  rm -f "$out/$asset" "$out/$asset.sha256"
  gh release download "$tag" --repo "$repo" --pattern "$asset" --pattern "$asset.sha256" --dir "$out" \
    || die "cannot download $asset (and $asset.sha256) from $repo $tag"
  [ -f "$out/$asset.sha256" ] || die "$repo $tag has no $asset.sha256"
  local got declared
  got="$(sha256sum "$out/$asset" | cut -d' ' -f1)"
  declared="$(awk 'NR==1{print tolower($1)}' "$out/$asset.sha256")"
  [ "$got" = "$declared" ] || die "$asset: sha256 $got does not match $asset.sha256 ($declared)"
  if [ -n "$want" ] && [ "$got" != "$want" ]; then
    die "$asset: sha256 $got is not the reviewed one ($want); was the release replaced?"
  fi
  echo "$got"
}

# safe_unpack TARBALL ID DEST: DEST/ID is the plugin folder afterwards.
# One top folder named ID, no absolute paths, no "..", only regular files
# and folders.
safe_unpack() {
  local tgz="$1" id="$2" dest="$3"
  rm -rf "${dest:?}/$id"
  mkdir -p "$dest"
  local listing
  listing="$(tar -tvzf "$tgz")" || die "$tgz is not a .tar.gz archive"
  [ -n "$listing" ] || die "$tgz is empty"
  # Entry types: first character of the mode column ('-' file, 'd' folder).
  if printf '%s\n' "$listing" | cut -c1 | grep -qv '^[-d]$'; then
    die "$tgz contains links or special files"
  fi
  local names
  names="$(tar -tzf "$tgz")"
  while IFS= read -r n; do
    case "$n" in
      /*) die "$tgz: absolute path $n" ;;
    esac
    case "/$n/" in
      */../*|*/./*) die "$tgz: path $n contains . or .." ;;
    esac
    case "$n" in
      "$id"|"$id/"|"$id"/*) ;;
      *) die "$tgz: every entry must be inside a top folder named $id/ (found $n)" ;;
    esac
  done <<< "$names"
  tar -xzf "$tgz" -C "$dest" --no-same-owner --no-same-permissions || die "cannot unpack $tgz"
  [ -f "$dest/$id/manifest.json" ] || die "$tgz has no $id/manifest.json"
  if [ -n "$(find "$dest/$id" ! -type f ! -type d | head -n1)" ]; then
    die "$tgz unpacked to links or special files"
  fi
  local kb
  kb="$(du -sk "$dest/$id" | cut -f1)"
  [ "$kb" -le "$MAX_UNPACKED_KB" ] || die "$tgz unpacks to more than 64 MiB"
  chmod -R u+rwX,go+rX,go-w "$dest/$id"
}

# validate_dir DIR: the core's manifest validation and signing, on a copy,
# with the throwaway key. The package must be unsigned (no manifest.sig).
validate_dir() {
  local dir="$1" pub copy
  [ ! -e "$dir/manifest.sig" ] || die "$dir: the package must not be signed (the registry signs it)"
  pub="$(throwaway_pub)"
  copy="$WORK/validate/$(basename "$dir")"
  rm -rf "$copy"; mkdir -p "$WORK/validate"
  cp -r "$dir" "$copy"
  plugin_sign -key "$WORK/throwaway.key" "$copy" >/dev/null || die "$(basename "$dir"): the manifest is not valid for Ervisio (see plugin-sign above)"
  plugin_sign -verify -pub "$pub" "$copy" >/dev/null || die "$(basename "$dir"): the signed copy does not verify"
  note "$(basename "$dir"): manifest valid"
}

# repack DIR ID OUTFILE MTIME: deterministic tarball with top folder ID/.
repack() {
  local parent="$1" id="$2" out="$3" mtime="$4"
  tar --sort=name --owner=0 --group=0 --numeric-owner --mtime="@$mtime" --format=gnu \
    -C "$parent" -cf - "$id" | gzip -n -9 > "$out"
}
