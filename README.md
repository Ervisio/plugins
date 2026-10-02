# Ervisio plugin registry

This repository is the plugin marketplace of [Ervisio](https://github.com/Ervisio/ervisio). It lists the plugin
repositories Ervisio trusts, keeps the reviewed version of each plugin, signs those versions with the Ervisio team key
and publishes the signed catalog that every console reads in **Plugins › Browse**:

| File | URL |
|---|---|
| Catalog | https://ervisio.github.io/plugins/catalog.json |
| Catalog signature | https://ervisio.github.io/plugins/catalog.sig |
| Signed packages | `https://github.com/Ervisio/plugins/releases/download/<id>-<version>/<id>-<version>.tar.gz` |

Plugin authors: see [CONTRIBUTING.md](CONTRIBUTING.md).

## Trust model

* Only maintainers merge into `main`. Every plugin listed in `registry.json` is reviewed by a maintainer, and so is
  every new version of it (a pull request per version).
* After a version is merged, CI signs it with the **Ervisio team key** (ed25519). The consoles only run plugins whose
  signature verifies against that key (`plugins.allow_unsigned = false` is their default), so a package that was not
  reviewed here cannot be installed from the marketplace.
* The catalog itself is signed with the same key. A console ignores a catalog whose `catalog.sig` does not verify,
  so nobody who controls the network or the hosting can change what Browse offers, the checksums it pins, or the
  permissions it shows in the consent dialog.
* Plugin repositories need no secret and get no write access here: the registry pulls their public releases.
* `trust` in `registry.json` says who wrote a plugin: `team` (the Ervisio team) or `community` (a third party, reviewed
  by the maintainers). Both are signed and shown as verified once they are listed.

## How a release flows

1. The plugin repository tags `vX.Y.Z` (equal to the manifest version). Its release workflow builds the package and
   attaches `<id>-<X.Y.Z>.tar.gz` (one top folder `<id>/`, unsigned `manifest.json`) and `<id>-<X.Y.Z>.tar.gz.sha256`.
2. **Sync** (`.github/workflows/sync.yml`, every 6 hours, or by hand / `repository_dispatch` `plugin-release`) finds
   the new release, downloads it, checks the checksum, unpacks it safely, validates the manifest with the core's own
   rules (`plugin-sign` built from `Ervisio/ervisio` at `CORE_REF`, with a throwaway key) and opens a pull request that
   updates `plugins/<id>.json`. The description lists the permission changes ("New permissions", "Changed permissions",
   "Removed permissions") with warnings for administrator rights, the Docker socket, network hosts and writable system
   folders. Updates that add or widen permissions get the `new-permissions` label.
3. A maintainer reviews the source at the tag and the permission summary, then merges.
4. **Publish** (`.github/workflows/publish.yml`, on every merge to `main`) downloads the reviewed package again,
   checks that its checksum and manifest are exactly the reviewed ones, signs it (`manifest.json` with the sha256 of
   every file, `manifest.sig`), verifies the signature against the team key embedded in the core, repacks it
   deterministically and releases it here as `<id>-<version>`. Then it rebuilds `catalog.json` from all reviewed
   versions, signs it (`catalog.sig`), verifies it against `team.pub` and deploys both to GitHub Pages.

Pull requests opened by the sync workflow use the workflow's `GITHUB_TOKEN`, and GitHub does not start other workflows
for them. The sync job already ran every check; to run **Check** as well, close and reopen the pull request.

## Files

| Path | What |
|---|---|
| `registry.json` | Categories of Browse and the plugin sources: `id`, `repo` (`owner/name`), `trust` (`team` or `community`), `category`, `featured`. |
| `plugins/<id>.json` | The reviewed version: `version`, upstream `tag`, `asset`, `upstreamSha256`, short `notes` shown in Browse, and the reviewed part of the manifest (`name`, `author`, `description`, `entry`, `capabilities`, `contributes`, `visibleTo`). Changed only by pull requests. |
| `CORE_REF` | The `Ervisio/ervisio` tag whose `plugin-sign` validates and signs packages. |
| `team.pub` | The team public key. The catalog must verify against it. |
| `tools/regtool` | Go tool (standard library only): checks the files, writes `plugins/<id>.json` and the pull request summary, builds, signs and verifies the catalog. |
| `scripts/` | The workflow steps (`sync.sh`, `check.sh`, `publish.sh`, shared `lib.sh`), runnable locally. |

## Catalog format and signature

`catalog.json` is the format the core's `plugins.catalog` reads (`docs/api/plugins.md` in the core):
`{"generated", "categories": [...], "plugins": [{id, name, version, author, description, icon, color, category,
verified, installs, featured, notes, source, sha256, capabilities, contributes, visibleTo, homepage, repo, trust}]}`.
`capabilities`, `contributes` and `visibleTo` are copied from the signed manifest, so the consent dialog shows exactly
what the package declares (the console refuses an install whose manifest differs from what the user approved).

`catalog.sig` is the base64 ed25519 signature of `"ervisio-catalog-v1\n"` followed by the canonical form of
`catalog.json` (the JSON re-encoded with sorted keys, no whitespace and no HTML escaping, numbers as written; the same
canonical form the core uses for plugin manifests), then a newline. To check a catalog by hand:

```sh
curl -fsSLO https://ervisio.github.io/plugins/catalog.json
curl -fsSLO https://ervisio.github.io/plugins/catalog.sig
(cd tools/regtool && go run . verify-catalog -pubfile ../../team.pub ../../catalog.json ../../catalog.sig)
```

## Secrets and settings

| Name | Where | What |
|---|---|---|
| `PLUGIN_SIGNING_KEY` | Repository secret | The team private key: the content of the key file written by `plugin-sign -genkey` (one line, base64 of the 64-byte ed25519 private key). Only the publish workflow reads it, writes it to a temporary file with mode 0600 and deletes it. |

A release maintainer sets it from the machine that holds the key (the command reads the file; the key never appears
on the command line):

```sh
gh secret set PLUGIN_SIGNING_KEY --repo Ervisio/plugins < ~/.config/linuxadmin-signing/team.key
gh workflow run publish.yml --repo Ervisio/plugins
```

Until the secret exists, the publish workflow stops with "PLUGIN_SIGNING_KEY is not set". Recommended settings:

* Branch protection on `main`: require a pull request with one approving review, require the **Check** and **CI**
  checks, no force pushes. Signing happens only on `main`, so this is what keeps unreviewed code out of the catalog.
* Pages: source "GitHub Actions" (Settings › Pages).
* Actions: "Allow GitHub Actions to create and approve pull requests" (Settings › Actions › General), needed by the
  sync workflow.

## Maintenance

* **CORE_REF**: pin a released core tag. Bump it after a core release that changes manifest validation or the
  signature format, in its own pull request; publish re-verifies every signed package with the new `plugin-sign`.
* **Removing a plugin**: delete its entry from `registry.json` and its `plugins/<id>.json`. The next publish drops it
  from the catalog; installed copies keep working until their owners remove them. For a security problem also delete
  the signed release here.
* **Key rotation**: follow `docs/PLUGIN-SIGNING.md` in the core. Then update `team.pub` and the secret together, delete
  the signed releases (or bump `CORE_REF` first so verification uses the new embedded key) and run publish so every
  package and the catalog are signed with the new key.

## Local runs

`scripts/sync.sh`, `check.sh` and `publish.sh` run outside Actions with `GH_TOKEN` set (or `gh auth login`).
`DRY_RUN=1 scripts/sync.sh` validates the latest releases and prints the summaries without pushing. For a test of
`publish.sh`, use a throwaway key (`plugin-sign -genkey /tmp/test.key`) with `SKIP_RELEASE=1`,
`VERIFY_PUB=<its public key>` and `TEAM_PUB_FILE=<file with it>`; never use the team key outside CI.

## License

MIT, see [LICENSE](LICENSE).
