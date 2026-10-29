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
  every new version of a community plugin (a pull request per version). For **team** plugins first versions and permission changes are reviewed; routine updates with exactly unchanged permissions are validated and merged automatically, subject to repository rules.
* After a version is merged, CI signs it with the **Ervisio team key** (ed25519). The consoles only run plugins whose
  signature verifies against that key (`plugins.allow_unsigned = false` is their default), so a package that was not
  reviewed here cannot be installed from the marketplace.
* The catalog itself is signed with the same key. A console ignores a catalog whose `catalog.sig` does not verify,
  so nobody who controls the network or the hosting can change what Browse offers, the checksums it pins, or the
  permissions it shows in the consent dialog.
* Plugin repositories need no secret and get no write access here: the registry pulls their public releases.
* `trust` in `registry.json` says who wrote a plugin: `team` (the Ervisio team) or `community` (a third party, reviewed
  by the maintainers). Both are signed and shown as verified once they are listed.

## Publishing: everyday use

After committing, merging and pushing a plugin's code:

```sh
npm run release -- patch          # or minor / major
```

The GitHub CLI must be installed and logged in (`gh auth login`, once). The command checks your checkout, starts the
release workflow and prints its URL. Version, changelog, tests, package, checksum and GitHub release are automatic.
Build/tests/manifest validation complete before the version and tag are pushed together. Pull the resulting release
commit with `git pull --ff-only`. Drive runs this from `plugin/`.

For a **new plugin**, publish its first release, then run `npm run marketplace -- "Monitoring"`, or open
**Actions → Register plugin** here and enter its id, source repository and category. The workflow validates the
package and opens **one PR containing `registry.json` plus `plugins/<id>.json`**. Review and merge it once.

## How a release flows

1. The plugin's reusable SDK workflow builds and releases its package. `REGISTRY_TOKEN`, if configured, notifies this
   repository immediately. Otherwise scheduled Sync checks every 15 minutes; GitHub can delay scheduled runs.
2. Sync downloads, checks and validates the release, creates a version PR and explicitly dispatches both required
   checks on that branch. Bot PRs no longer need to be closed and reopened to start CI.
3. **Publish validated team updates** runs trusted code from `main` after CI/Check. It only merges same-repository
   PRs created by `github-actions[bot]` on `sync/*`, changing exactly one existing team plugin version entry, with a
   newer stable version and identical complete capabilities, visibility, platforms and dependencies. Both required
   checks must pass on the exact head SHA. Labels and release descriptions never authorize a merge.
4. First versions, community updates and any permission change remain a single reviewed PR. Permission reductions
   are conservatively reviewed too. The automatic path never changes registry sources or workflow code.
5. After an automatic merge the workflow explicitly dispatches Publish (`GITHUB_TOKEN` merges do not trigger push
   workflows). Human merges trigger Publish normally. Signing, pinned checksums, catalog signatures and Pages remain
   unchanged.

### One-time repository rules

Keep required checks **check** and **regtool (gofmt, go vet, go test)** enabled. Automatic merges use the registry's
`GITHUB_TOKEN` and respect branch protection. If your rule requires a human approval for every PR, either allow a
publishing identity to bypass that review for this automation, or set the repository secret `REGISTRY_MERGE_TOKEN`
to a dedicated identity allowed by that rule (Contents, Pull requests and Actions write on this repository only).
The workflow still performs its narrow policy and exact-commit checks before requesting a merge. Do not disable
branch protection. If the rule refuses the merge, the validated PR stays open and the run reports a warning.

The token is optional and never passed to a plugin repository or PR code. The default token suffices when rules only
require the successful checks. GitHub Actions must be allowed to create PRs in this repository.

## Files

| Path | What |
|---|---|
| `registry.json` | Categories of Browse and the plugin sources: `id`, `repo` (`owner/name`), `trust` (`team` or `community`), `category`, `featured`. |
| `plugins/<id>.json` | The reviewed version: `version`, upstream `tag`, `asset`, `upstreamSha256`, short `notes` shown in Browse, and the reviewed part of the manifest (`name`, `author`, `description`, `entry`, `capabilities`, `contributes`, `visibleTo`). Updated through validated pull requests. |
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
| `REGISTRY_TOKEN` | Organization secret (for the plugin repositories) | A fine-grained token with access to Ervisio/plugins only, permission "Contents: read and write" (needed for `repository_dispatch`). Plugin release workflows use it to start Sync at once. Without it Sync still runs on its next scheduled scan (every 15 minutes). |

A release maintainer sets it from the machine that holds the key (the command reads the file; the key never appears
on the command line):

```sh
gh secret set PLUGIN_SIGNING_KEY --repo Ervisio/plugins < ~/.config/linuxadmin-signing/team.key
gh workflow run publish.yml --repo Ervisio/plugins
```

Until the secret exists, the publish workflow stops with "PLUGIN_SIGNING_KEY is not set". Recommended settings:

* Branch protection on `main`: require a pull request with one approving review, require the **Check** and **CI**
  checks, no force pushes. Signing happens only on `main`, so this is what keeps unreviewed code out of the catalog.
  For automatic team updates, see the one-time repository rules above; version PRs remain validated even when a human review is required.
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

