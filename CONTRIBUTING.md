# Publishing a plugin in the Ervisio marketplace

Any public plugin can be listed. Listed plugins are reviewed by the Ervisio maintainers and then signed with the Ervisio
team key, which is what lets every console install them. This page says what a plugin needs and how review works.

## Requirements

* **A public GitHub repository** with the full source of the plugin and a license (MIT or another OSI license is
  recommended). The [plugin SDK](https://github.com/Ervisio/plugin-sdk) has a template and the build preset;
  [plugin-docker](https://github.com/Ervisio/plugin-docker) is a complete example.
* **A manifest for SDK v3** (`manifest.json`, described in the core's `docs/api/plugins.md`). Declare only the
  permissions the plugin needs: commands, HTTP APIs on sockets, folders, network hosts, `visibleTo`.
* **Releases built by CI from a tag.** The tag is `vX.Y.Z` and equals `version` in the manifest. The release has two
  assets:
  * `<id>-<X.Y.Z>.tar.gz`: one top folder named `<id>/` with `manifest.json` (unsigned: no `files` map and no
    `manifest.sig`; the registry signs it) and the built files. Regular files and folders only, at most 32 MiB
    compressed and 64 MiB unpacked.
  * `<id>-<X.Y.Z>.tar.gz.sha256`: `<sha256 hex>  <id>-<X.Y.Z>.tar.gz`.

  The release body is the changelog of the version; its first lines are shown in Browse. Do not replace the assets of
  a published release: publish a new version instead (the registry pins the checksum it reviewed).
* **A CHANGELOG** in the repository, so reviewers can follow what changed.
* **Readable source.** The released files must be what the tagged source builds. Minified output is fine when the
  source and the build are in the repository; obfuscated code, binaries without source, or code downloaded at run
  time are not accepted.

## Proposing a plugin

Publish a stable `vX.Y.Z` release with the assets above. Then use **Actions → Register plugin** in this repository,
or run `npm run marketplace -- "Monitoring"` from a project with the SDK release launcher. Supply the manifest id,
public repository and existing category. The workflow opens one PR with the source and first validated version.
Community sources receive `trust: community`; only repositories in Ervisio receive `trust: team`.

Review and merge that one PR to list the plugin. There is no second first-version PR. If you propose a new category,
add it separately first. Manual source PRs are still supported, but the workflow is the recommended path.

## What reviewers check

* The source at the tag, and for updates the changes since the last reviewed version.
* That the release asset is built from the tag by the repository's own release workflow.
* That every permission is proportionate to what the plugin does. Administrator rights (`admin`,
  `adminUnlessGroup`), the Docker socket (equivalent to root), terminal commands, writable system folders and network
  hosts get the closest look. A plugin that asks for more than it uses is sent back.
* That `visibleTo` matches the audience of the plugin.
* That the plugin keeps to the sandbox model of the SDK (no attempts to reach the app's origin, no tracking).

## Updates

Release a new version (with the SDK's release workflow: Actions › Release › Run workflow); nothing else is needed.
The sync workflow notices it at once (or on the scheduled scan (every 15 minutes, subject to GitHub scheduling delays)) and opens a pull request with a summary of the permission
changes. Bot updates of team plugins with exactly unchanged permissions are checked and merged automatically, subject to repository rules. First versions and permission changes need one review. Updates that add or widen permissions get the `new-permissions` label and a
closer review. When the pull request is merged, the new version is signed and published, and consoles offer the update
in **Plugins › Updates** (asking the user again when permissions changed).

## Removal

Maintainers remove a plugin from `registry.json` when it is abandoned, broken for current consoles, or no longer meets
these rules; it then leaves the catalog at the next publish. Report security problems in a listed plugin privately to
the maintainers through a GitHub security advisory on this repository (Security › Report a vulnerability), not in a
public issue. For a serious problem the signed release is deleted at once.

