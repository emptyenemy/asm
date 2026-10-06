# Contributing to asm

asm is a small Go program with no dependencies, so getting it running takes a
minute. Bug reports from real machines help as much as code.

## Set up

Go 1.27.1 or newer, nothing else:

```sh
git clone https://github.com/emptyenemy/asm
cd asm
go build .
go test ./...
go vet ./...
```

The tests build tiny SDK archives and serve them from local HTTP servers, so
they need no network and finish in about two seconds.

## Find your way around

- `main.go` — the entry point. It only calls `cli.Run`.
- `internal/cli/` — everything else, one concern per file. `doc.go` is the
  map, and `commands.go` lists every command with its help and arguments.
- `docs/` — the project page: plain HTML, CSS and JavaScript, no build step.
- `install.ps1`, `install.sh` — the installers behind the one-line install.

[AGENTS.md](AGENTS.md) has the file-by-file table, the steps for adding a
command, the release build, and what was learned about the AIR SDK services
asm talks to.

## Report a bug

Downloads from the live AIR SDK service are not verified on every host yet, so
a report from your machine is worth a lot.
[Open an issue](https://github.com/emptyenemy/asm/issues/new/choose) with the
command, everything it printed, `asm --version`, and your OS and CPU.

## Send a change

- One change per pull request, with a test that would have caught the bug or
  that covers the feature.
- `go test ./...` and `go vet ./...` pass, and the code is formatted with
  `gofmt`.
- Commit subjects are short imperative English, like the existing log:
  `Add …`, `Keep …`, `Shorten …`.
- The repository stores LF line endings. Edit the lines you mean to change
  rather than rewriting a file, or the diff swallows the change.
- The CLI uses only the standard library. A new dependency needs a strong
  reason.
