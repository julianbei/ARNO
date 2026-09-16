# Contributing to Arno

Thanks for your interest! Arno is small and early, so every report and every
change is read.

## The most useful contribution: tell us how it went

You don't need to write code to help. After using Arno for real work, the
[feedback template](https://github.com/julianbei/arno/issues/new?template=feedback.yml)
takes a few minutes — and "I turned it off" is the report we most want. If
your agent reached for the shell although a Arno tool existed, file
[friction](https://github.com/julianbei/arno/issues/new?template=friction.yml).
[docs/reporting.md](docs/reporting.md) says what makes a report easy to act on.

Questions and ideas that aren't a bug or a wish fit in
[Discussions](https://github.com/julianbei/arno/discussions).

## Changing the code

Arno is written in Go and supports macOS and Linux (Windows is not supported;
see [issue #2](https://github.com/julianbei/arno/issues/2)).

```bash
git clone https://github.com/julianbei/arno.git
cd arno
make build      # go build ./...
make test       # go test ./...
make binary     # bin/arno-mcp, version-stamped
```

Before opening a pull request, run the release gate — build, vet, tests and a
gofmt check:

```bash
make build && go vet ./... && make test && test -z "$(gofmt -l ./cmd ./internal)"
```

`make conformance` runs every language against its real language server in a
container; run it when you change language support.

### What a good change looks like

- **It says what it approximates.** The one hard rule: if a code path gives an
  approximate answer, the response has to say so.
- **It comes with a test** that fails without the change.
- **It adds a CHANGELOG entry** under `## Unreleased` (create the heading if
  it isn't there): what changed, and why it matters to someone using Arno.
- **Tool names, required arguments and response vocabulary** are covered by
  [docs/tool-contract.md](docs/tool-contract.md). Changing them needs a
  reason, and the contract updated in the same change.

For anything larger than a fix, opening an issue first saves you work: the
[release plan](docs/release-plan-0.1.0.md) says what is planned and what is
deliberately not.

## License

By contributing, you agree that your contributions are licensed under the
[Apache License 2.0](LICENSE).
