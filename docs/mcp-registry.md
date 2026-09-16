# Publishing to the MCP Registry

Arno is listed in the [official MCP Registry](https://registry.modelcontextprotocol.io)
as `io.github.julianbei/arno`. Clients and directories that read the registry
pick it up from there. The listing offers two packages:

- **The container image** on ghcr.io, for clients that run servers in Docker.
- **The MCP bundle** (`arno-mcp_<version>.mcpb`) attached to the GitHub
  release, for Claude Desktop and other clients that install bundles. It
  carries the macOS and Linux binaries for Intel and ARM; the sources are in
  [packaging/mcpb](../packaging/mcpb).

The listing is described by [server.json](../server.json). The registry checks
three things before accepting it:

- **The name belongs to us.** `io.github.julianbei/*` names can only be
  published by someone logged in to GitHub as `julianbei`, or by this
  repository's workflows through GitHub OIDC.
- **The image belongs to the name.** The image's
  `io.modelcontextprotocol.server.name` label, set in the
  [Dockerfile](../Dockerfile), must equal `name` in `server.json`. Images
  released before v0.0.10 do not carry the label.
- **The bundle is the one published.** The bundle's URL must contain "mcp",
  and `fileSha256` must match the file on the release.

## For each release

Nothing to do by hand. The `publish` job in the
[release workflow](../.github/workflows/release.yml) builds, validates and
attaches the bundle. The `registry` job runs once the image and the release
are up: it sets `version`, the image tag, and the bundle's URL and sha256 in
`server.json` from the tag, logs in with GitHub OIDC (no secret) and runs
`mcp-publisher publish`.

The committed `server.json` lists only the image, because the bundle's
checksum exists only after the release is built.

If the job fails, publish from the repository root instead, after making the
same changes to `server.json` (the job's "Set version from tag" step is the
recipe):

```bash
brew install mcp-publisher      # once; or see the registry's README
mcp-publisher login github      # once per machine; opens a browser
mcp-publisher publish
```

The registry keeps every published version; `publish` adds the new one.
Published versions cannot be changed, so a broken listing is fixed by
releasing a new version.

## Other directories

These list open-source MCP servers without a popularity requirement. They are
worth updating when the description or install steps change:

- [Glama](https://glama.ai/mcp/servers/julianbei/ARNO) — indexes GitHub on
  its own and builds a new release after ours, from the build spec on its
  Dockerfile page (it installs the latest release with the install script).
  Nothing to do per release; [glama.json](../glama.json) names the maintainer.
  Glama moved the listing to `julianbei/ARNO` after the repository rename,
  keeping its score; the slug is case-sensitive, so the badge URL above uses
  the capitals.
- [awesome-mcp-servers](https://github.com/punkpeye/awesome-mcp-servers) —
  one line under Developer Tools, submitted in
  [#14418](https://github.com/punkpeye/awesome-mcp-servers/pull/14418).
- [PulseMCP](https://www.pulsemcp.com) — manual submissions are paused; it
  reads the official registry, so the listing arrives from there.
- Registry browsers such as [MCP Bench](https://mcpbench.ai) and the
  [MCP Registry UI](https://vemonet.github.io/mcp-registry) read the registry
  directly.

Directories that charge for a listing (mcp.so, for one) are skipped.
