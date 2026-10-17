# Mirr - Git mirror tool

Linux is supported. macOS builds but is untested. Windows is not supported.

## Overview

`mirr` copies Git branches and tags from one source repository to one or more mirrors. It preserves commits, tag objects, and their SHAs. Create the destination repositories before using it.

`plan` compares refs without pushing. `sync` checks them again and pushes changes atomically to each mirror, with a lease on the expected destination SHA. Sync fails if the server does not support atomic push. The CLI `sync` command does not ask for confirmation; the TUI does.

## Modes and configuration

| Mode | What it syncs |
| --- | --- |
| `release` | Tags matching the configured filters. This is the default mode. |
| `continuous` | All branches and tags. |

`continuous` describes the ref scope. It does not run in the background. Start a sync from the CLI or TUI. Switching a mirror from `continuous` to `release` leaves its existing branches in place but stops updating them.

`.mirr.yaml` defines source repositories, mirrors, credentials, and per-mirror settings. `source.url` is the Git source; `mirrors.<name>.url` is the push destination. `defaults` applies to every mirror in a repository unless that mirror overrides a setting. `mirr` reads `.mirr.yaml` from the current directory; use `-c PATH` for another file. Relative local paths are resolved from the config file's directory.

By default, `policy.delete` and `policy.allow_force` are false. Missing source refs are not deleted from a mirror, and moved tags or diverged branches produce a conflict. An empty source is never synced. If a mirror overrides `tags` or `policy`, it replaces that whole section.

## Tags and releases

In `release` mode, the default tag filter includes `v*` and excludes `*-rc*`, `*-beta*`, and `*-alpha*`. Set `tags.include` and `tags.exclude` in the config to change it. `continuous` copies all tags regardless of these filters.

`mirr tags NAME` and the TUI tag view show every tag on the source and mirror, including tags excluded by the `release` filter. Use `mirr plan NAME` to see which tag changes will actually be pushed.

A Git tag is separate from a GitHub or GitLab release. `mirr` copies the tag, including the message of an annotated tag. It does not copy release descriptions or attached files. Create the release on the mirror using the synced tag.

## Access and SSH

For SSH, use a normal Git URL such as `git@host:team/repo.git`. `mirr` runs your system Git, which uses your SSH key, `ssh-agent`, `~/.ssh/config`, and `known_hosts`. The key needs read access to the source and write access to the mirror. `mirr` does not manage SSH keys.

For HTTPS without an `auth` profile, Git uses its configured credential helper. Do not put usernames or tokens in HTTPS URLs. Named `auth` profiles support:

- `provider: keyring`: Linux Secret Service through `secret-tool`. Store the token with `mirr auth set PROFILE`.
- `provider: env`: read the token from an environment variable, for example a CI secret.

Auth profiles work only with HTTPS. `mirr` passes the token to Git through askpass, without writing it to the URL or config. Git prompts are disabled, so configure access before running in CI.

## CLI, TUI, and Make

To build from source, you need Go 1.24.2 or newer and Git. Run `go install ./cmd/mirr` from the project checkout to install the `mirr` command in your Go binary directory.

| CLI | Action |
| --- | --- |
| `mirr init` | Guided setup for `.mirr.yaml` in a terminal. The flag-based form is also available. |
| `mirr validate` | Validate the config without contacting Git servers. |
| `mirr plan NAME` / `mirr p NAME` | Preview ref changes. |
| `mirr status` | Compare all configured mirrors. |
| `mirr branches NAME` | Compare source and mirror branches and SHAs. |
| `mirr tags NAME` | Compare source and mirror tags and SHAs. |
| `mirr sync [NAME]` / `mirr s [NAME]` | Sync one repository; with one configured repository, the name is optional. Use `--to MIRROR` for one mirror or `--all` for all repositories. |
| `mirr history` | Show the last recorded sync result for each mirror. |
| `mirr auth set PROFILE` | Store an HTTPS token in the keyring. |
| `mirr` or `mirr tui` | Open the TUI. |

Short aliases are available: `st` for `status`, `b` for `branches`, `t` for `tags`, and `check` for `validate`. For scripts, use `--non-interactive`, `--output json`, and `--timeout 10m`. CLI `sync` recalculates the plan and is not bound to an earlier `plan` command. Exit codes: `0` success, `1` mirror error or conflict, `2` command or config error.

In the TUI, `j/k` or the arrow keys select a mirror. `b` shows branches and `t` shows tags; press the same key again to return to the plan. `r` refreshes, `s` syncs the selected mirror, `S` syncs all displayed mirrors, `m` changes a mirror's mode, `h` opens history, `?` shows all keys, and `q` quits. Sync requires confirmation. Before pushing, the TUI checks that the plan still matches what you reviewed.

| Make | Action |
| --- | --- |
| `make build` | Build `bin/mirr`. |
| `make test` | Run tests with the race detector. |
| `make check` | Run tests and `go vet`. |
| `make demo` | Create a small local demo in `.demo/`. |
| `make demo-showcase` | Create a local demo with many branches and tags in `.demo/showcase/`. |
| `make e2e-release` | Test `release` mode against local Git repositories. |

## Examples

From a repository that you want to use as the local source:

```sh
mirr init
# Answer the setup questions, or use flags for scripted setup:
# mirr init --source . --mirror git@github.com:you/project.git --name project --mode continuous
mirr validate
mirr plan project
mirr
```

Only committed Git refs are copied. For a server-side source, pass its Git URL to `--source` instead of `.`. The [example config](.example.mirr.yaml) shows GitHub, self-hosted GitLab, SSH, HTTPS tokens, local paths, and CI credentials.

To try the TUI without network access:

```sh
make demo-showcase
cd .demo/showcase/root
mirr
```
