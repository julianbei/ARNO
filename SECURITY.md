# Security policy

## Reporting a vulnerability

Please **don't open a public issue** for a security problem. Report it
privately through
[GitHub's security advisories](https://github.com/julianbei/jade/security/advisories/new)
instead. You'll get a reply within a few days, and we'll agree on disclosure
together once there's a fix.

Useful to include: the Jade version (`jade-mcp --version`), the tool call or
command involved, and what an attacker gains.

## What counts

Jade runs on your machine with your permissions and edits the repository you
point it at. These are in scope:

- reading or writing outside the workspace root
- running a command that was not declared in `.jade/commands.json` or
  `.jade/project.json`
- launching a binary shipped inside a repository
- the install script installing something other than the verified release
  binary
- telemetry or the update check sending anything beyond what the README says

Jade is not hardened for untrusted input: an agent following instructions
planted in a repository can still ask Jade to make edits or run the commands
that repository declares. The README's "What Jade does not do yet" section
explains what only a sandbox covers.

## Supported versions

Only the latest release gets fixes.
