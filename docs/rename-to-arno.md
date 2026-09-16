# Renaming Jade to ARNO

**ARNO — Agent Repository Navigation & Operations.**

Jade is ambiguous in software: existing products, a prior trademark conflict,
and other MCP projects already use the name. That costs searchability and
carries legal risk. Renaming now is far cheaper than renaming after the name
spreads through packages, docs, integrations and user configs.

## Decisions

| Question | Decision |
| --- | --- |
| Command and binary | `arno-mcp` |
| Old spellings | `jade.*` tools, `JADE_*` variables and `.jade/` keep working, deprecated, removed at the release after this one |
| Repository | `julianbei/jade` → `julianbei/arno`, in this release |
| Version | `0.0.12`; 0.1.0 keeps its meaning |

## Scope, measured

2380 occurrences of "jade" across 215 of 305 tracked files.

| Surface | Count | Notes |
| --- | --- | --- |
| `github.com/julianbei/jade` | 282 | Go module path: `go.mod` plus every import |
| `jade.<tool>` | 456 | Wire names in the catalog, docs and tests |
| `jade-mcp` | 220 | Binary, directory `cmd/jade-mcp`, install docs |
| `JADE_*` | 129 | 18 variables |
| `.jade/` | 71 | `commands.json`, `project.json`, `telemetry.jsonl` |
| `ghcr.io/julianbei/jade-mcp` | 10 | Image name |
| `io.github.julianbei/jade` | 7 | MCP Registry listing |

Renamed files and directories: `cmd/jade-mcp/` → `cmd/arno-mcp/`,
`cmd/jade-bench/` → `cmd/arno-bench/`, `cmd/jade/` → `cmd/arno/`,
`configs/jade.example.yaml`, `.jade/commands.json`, the logo files, and
`internal/bench/agent/testdata/stream-jade.jsonl`.

## Compatibility, and when it ends

Everything below is accepted, answers, and says it is deprecated. All of it is
deleted in the release after the rename.

- **Tool names.** `jade.find` and `jade_find` resolve to `arno.find`.
  `canonicalToolName` in `toolname.go` already maps two spellings to one wire
  name; this adds a third rule. `tools/list` advertises only `arno.*`, so an
  agent reading the catalog never learns the old name.
- **Environment.** `ARNO_*` wins; `JADE_*` is read when the new one is unset,
  and using it logs a deprecation line to stderr once.
- **Workspace directory.** `.arno/` is written. `.jade/` is read when `.arno/`
  is absent, for `commands.json`, `project.json` and `telemetry.jsonl`. One
  line on startup says how to move it (`git mv .jade .arno`).
- **Install script.** `JADE_*` variables keep working for one release.
- **Not compatible:** the Go module path and the container image. Old versions
  of both keep working at their old names forever; new ones only exist at the
  new name.

## Why 0.0.12 and not 0.1.0

[release-plan-0.1.0.md](release-plan-0.1.0.md) defines 0.1.0 as a claim
someone else can check: 20+ outside repositories benchmarked against the
shell, setup verified in five hosts, outside users running it. None of that is
done, and the rename advances none of it.

So the rename ships as **0.0.12**. The version number is the only place left
that still promises the benchmark, and a rename is not evidence.

## Progress

- [x] Module path, command directories, tool wire names, variables, `.arno/`,
      identity strings in `server.json`, `Dockerfile` and the workflows.
- [x] Compatibility: `internal/compat` (env fallback, legacy directory, tool
      names), wired into the loaders, the tool router and `install.sh`, with
      tests.
- [x] Artwork: `logo.png`, `logo-dark.png`, the bundle icon and the social
      preview derived from the new ARNO cards.
- [ ] Gate green, then commit.
- [ ] GitHub rename, then the registries below.

## Order of work

Each phase ends green: build, vet, gofmt, tests, and the TDQS lint.

### 1. Code and repository

1. `go.mod` module path, then every import.
2. Directories: `cmd/jade-mcp` → `cmd/arno-mcp`, and the other two commands.
3. Wire names `jade.*` → `arno.*`, with the alias rule in `toolname.go` and a
   test per old spelling.
4. `JADE_*` → `ARNO_*`, with the fallback and its deprecation notice.
5. `.jade/` → `.arno/`, with the fallback read.
6. Server identity: `serverInfo.name`, instructions, `install.sh`, Makefile,
   Dockerfile, workflows, the MCP bundle manifest.
7. Docs: README, llms.txt, CHANGELOG, ROADMAP, docs/, release notes. Release
   notes for shipped versions keep their text; they describe what was true.
8. Logos: the new ARNO marks replace `logo.png` and `logo-dark.png`, and the
   social preview is regenerated from them.

### 2. GitHub

1. Rename the repository. GitHub redirects the old URLs: git remotes, the
   API, and raw file URLs, which keep serving under the old owner and name
   (verified against a repository renamed years ago). So the install one-liner
   the testers already have keeps working; the docs are updated anyway.
2. Description, topics, social preview image.
3. Issue #2 (Windows) and the templates mention Jade; update the text.

### 3. Registries — the part that must be repointed by hand

| Registry | What to do | Who |
| --- | --- | --- |
| **MCP Registry** | Publish `io.github.julianbei/arno` (new `server.json`, new image label). Then `mcp-publisher status --status deprecated --all-versions --message "Renamed to io.github.julianbei/arno" io.github.julianbei/jade`. Versions are immutable; the old listing stays visible as deprecated. | CI publishes the new one; the deprecation is one manual command |
| **ghcr.io** | New package `ghcr.io/julianbei/arno-mcp`. The old package cannot be renamed; leave its tags in place and stop pushing to it. | Release workflow |
| **Glama** | The listing follows the repository, but the build spec installs `jade-mcp` by name: update the Dockerfile page's build steps and CMD, then deploy. `glama.json` keeps the maintainer. | Manual, on Glama |
| **awesome-mcp-servers** | The merged entry (PR #14418) names `julianbei/jade`. New PR updating the line, its Glama badge and the install command. | New PR |
| **tdqs.dev** | Scores are per server name; the next CI run scores `ARNO` as a new server. Nothing to migrate. | Automatic |
| **PulseMCP, MCP Bench, registry browsers** | They read the official registry, so they follow the new listing and the deprecation on their own. | Automatic |

### 4. Users

- Release notes lead with the rename, what breaks, and what keeps working.
- The testers were sent a `jade-mcp` install command; the announcement says
  to re-run the install script, rename the MCP server entry in their client
  config, and (optionally) `git mv .jade .arno`.
- `jade-mcp` stays installed on their machines until they remove it: the
  script installs `arno-mcp` beside it. The notes say to delete the old one.

## What cannot be undone

- Published registry versions and image tags under the old name stay forever.
- The Go module proxy keeps `github.com/julianbei/jade` and every version
  published under it. That is fine: they remain installable, and nothing new
  appears there.
- The merged awesome-mcp-servers entry stays until a new PR lands.
