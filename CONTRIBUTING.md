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

Open a pull request here that adds one entry to `registry.json`:

```json
{"id": "my-plugin", "repo": "you/ervisio-plugin-mine", "trust": "community", "category": "Monitoring"}
```

* `id` must equal the manifest `id` (`^[a-z][a-z0-9-]{1,39}$`) and must not imitate another plugin or project.
* `category` is one of the categories at the top of `registry.json`; propose a new one in the same pull request if
  none fits.
* `trust` is `community` for third-party plugins (`team` is reserved for plugins of the Ervisio team).

The **Check** workflow validates your latest release the way the sync job will and prints the permission summary.
After the source is merged, the sync job opens the pull request for your first version; once that one is merged too,
the plugin is signed and appears in the catalog within minutes.

## What reviewers check

* The source at the tag, and for updates the changes since the last reviewed version.
* That the release asset is built from the tag by the repository's own release workflow.
* That every permission is proportionate to what the plugin does. Administrator rights (`admin`,
  `adminUnlessGroup`), the Docker socket (equivalent to root), terminal commands, writable system folders and network
  hosts get the closest look. A plugin that asks for more than it uses is sent back.
* That `visibleTo` matches the audience of the plugin.
* That the plugin keeps to the sandbox model of the SDK (no attempts to reach the app's origin, no tracking).

## Updates

Tag a new version; nothing else is needed. The sync workflow notices it within six hours and opens a pull request
with a summary of the permission changes. Updates that add or widen permissions get the `new-permissions` label and a
closer review. When the pull request is merged, the new version is signed and published, and consoles offer the update
in **Plugins › Updates** (asking the user again when permissions changed).

## Removal

Maintainers remove a plugin from `registry.json` when it is abandoned, broken for current consoles, or no longer meets
these rules; it then leaves the catalog at the next publish. Report security problems in a listed plugin privately to
the maintainers through a GitHub security advisory on this repository (Security › Report a vulnerability), not in a
public issue. For a serious problem the signed release is deleted at once.
