<!-- Adding a plugin? Read CONTRIBUTING.md first. -->

## What this changes

- [ ] Adds a plugin source to `registry.json`
- [ ] Updates a reviewed version in `plugins/<id>.json`
- [ ] Other (tools, workflows, documentation)

## Reviewer checklist (maintainers)

- [ ] The repository is public, has a license and builds the release asset from the tagged source in CI.
- [ ] The code at the tag (and the changes since the previous reviewed version) was read; nothing is obfuscated or fetched at run time from elsewhere.
- [ ] Every permission is needed for what the plugin says it does. Administrator rights, the Docker socket, `adminUnlessGroup`, terminal commands, writable system folders and network hosts were looked at one by one.
- [ ] `visibleTo` is sensible for the audience of the plugin.
- [ ] The id, name and category do not imitate another plugin or project.
- [ ] The Check workflow passed (for pull requests opened by the sync workflow: close and reopen to run it).

Merging signs the version with the Ervisio team key and publishes it in the catalog.
