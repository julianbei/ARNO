# Publishing to the MCP Registry

Jade is listed in the [official MCP Registry](https://registry.modelcontextprotocol.io)
as `io.github.julianbei/jade`, through its container image on ghcr.io. Clients
and directories that read the registry pick it up from there.

The listing is described by [server.json](../server.json). The registry checks
two things before accepting it:

- **The name belongs to us.** `io.github.julianbei/*` names can only be
  published by someone logged in to GitHub as `julianbei`.
- **The image belongs to the name.** The image's
  `io.modelcontextprotocol.server.name` label, set in the
  [Dockerfile](../Dockerfile), must equal `name` in `server.json`. Images
  released before v0.0.10 do not carry the label, so `server.json` points at
  v0.0.10 or later.

## For each release

Nothing to do by hand. The `registry` job in the
[release workflow](../.github/workflows/release.yml) runs after the image is
pushed: it sets `version` and the image tag in `server.json` from the tag,
logs in with GitHub OIDC (no secret) and runs `mcp-publisher publish`.

If that job fails, publish from the repository root instead, after setting
`version` and the image tag in `server.json` to the release:

```bash
brew install mcp-publisher      # once; or see the registry's README
mcp-publisher login github      # once per machine; opens a browser
mcp-publisher publish
```

The registry keeps every published version; `publish` adds the new one.

## Other directories

These list open-source MCP servers without a popularity requirement. They are
worth updating when the description or install steps change:

- [Glama](https://glama.ai/mcp/servers) — indexes GitHub on its own; claim
  the listing to edit it.
- [awesome-mcp-servers](https://github.com/punkpeye/awesome-mcp-servers) —
  one line under Developer Tools.
- [mcp.so](https://mcp.so) and [PulseMCP](https://www.pulsemcp.com) — submit
  forms.
