# Contributing

## Build and test

Go 1.27.1 or newer. The CLI uses only the standard library, so there is nothing
to install or vendor.

```sh
go build .
go test ./...
go vet ./...
```

See [Build from source](README.md#build-from-source) for the flags the release
archives are built with, and for why they differ from a plain `go build .`.

The repository stores LF line endings. Keep the line endings of a file as you
find them: edit the lines you mean to change instead of rewriting a file
wholesale, or the diff becomes the entire file and the change disappears into
it.

## Code layout

| File | Responsibility |
| --- | --- |
| `modules/app.go` | Command dispatch, options, the settings file, installed SDK discovery. |
| `modules/terminal.go` | Output rendering and the command help. |
| `modules/sdk.go` | Version parsing and matching, platform selection, small shared helpers. |
| `modules/catalog.go` | Release lists and build manifests from the official API, the announcement archive and the manager catalog. |
| `modules/download.go` | Archive downloads: retries, resuming, verification, leftover partial files. |
| `modules/extract.go` | ZIP extraction and the path checks that keep it inside the SDK directory. |
| `modules/install.go` | `install`, `update`, `uninstall` and SDK assembly. |
| `modules/clean.go` | Removing what an interrupted operation left behind. |
| `modules/platform_unix.go`, `modules/platform_windows.go` | The per-OS pieces: locking, directory moves, SDK configuration. |

## Adding a command

Five places, all of them:

1. `modules/app.go` — add the name to `validCommand`.
2. `modules/app.go` — add a `case` to the switch in `run`, and reject unwanted
   arguments the way the neighbouring cases do.
3. `modules/app.go` — if the command takes options, handle them in
   `parseOptions` and reject the flags it does not accept.
4. `modules/terminal.go` — add the name to `help`: the `usage` map, the `lines`
   map, and the `entries` list in the general help.
5. `README.md` — add a row to the command table.

Then cover it with tests, and add the command to the invalid-argument and help
cases in `TestCommandsAndVersionOutput`.

Those first four are separate lists, which is the honest weak spot of this
layout: a new command has to be added to each. Collapsing them into one table
per command is the obvious next improvement and has not been done yet.

## Commits

One commit per finished change. The history is meant to stay green — build and
tests pass on every commit, so `git bisect` and reverting a single change keep
working.

Subjects are short, imperative and English, matching the existing log:

```
Add styled installers for native release binaries
Move CLI implementation into the modules package
```

No body unless the reason is genuinely not obvious from the subject, and
nothing else in the message.

## Tests

Tests build small SDK and ZIP fixtures in temporary directories and serve them
from local HTTP servers, so the suite needs no network and finishes in about a
second. Prefer that to mocking: nothing here is mocked today and it is worth
keeping that way.

Rotating a source, a stalled transfer, an interrupted run and a locked SDK
directory all have tests. When fixing a bug, add the case that would have
caught it.
