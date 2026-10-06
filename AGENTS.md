# Working on asm

Technical notes for anyone changing this repository: how the code is laid out,
how to build and test it, and what was learned about the AIR SDK sources it
talks to. The [README](README.md) is the user-facing document.

## Build and test

Go 1.27.1 or newer. The CLI uses only the standard library, so there is nothing
to install or vendor.

```sh
go build .
go test ./...
go vet ./...
```

See [Release build flags](#release-build-flags) for the command the release
archives are built with, and for why it differs from a plain `go build .`.

The repository stores LF line endings. Keep the line endings of a file as you
find them: edit the lines you mean to change instead of rewriting a file
wholesale, or the diff becomes the entire file and the change disappears into
it.

## Code layout

The root `main.go` starts the CLI and supplies its build version, `go.mod`
declares the import path and the minimum Go version, and
`icon_windows_amd64.syso` gives the Windows executable its
[icon](#windows-icon). Everything else lives in
one package, `internal/cli`, one concern per file:

| File | Responsibility |
| --- | --- |
| `run.go` | `Run`, the app, option parsing and command dispatch. |
| `install.go`, `update.go`, `uninstall.go`, `clean.go` | One command each. |
| `config.go` | The AIR SDK Manager settings file and the API endpoint it may override. |
| `version.go` | The SDK version type: parsing, ordering, prefix matching. |
| `installed.go` | Finding installed SDKs through their `air-sdk-description.xml`. |
| `catalog.go` | Release lists and build manifests from the official API, the announcement archive and the manager catalog. |
| `http.go` | Requests, HTTP status errors, and network errors shortened to the host and the cause. |
| `download.go` | Archive downloads: retries, resuming, verification, leftover partial files. |
| `extract.go` | ZIP extraction. |
| `assemble.go` | Turning a build manifest into an SDK, with the shockpkg mirror as the fallback recipe. |
| `paths.go` | The path checks that keep every write inside the SDK directory. |
| `platform.go`, `platform_unix.go`, `platform_windows.go` | The host's SDK build, and the per-OS pieces: locking, directory moves, SDK configuration, the console. |
| `terminal.go` | Output: framing, indentation, color, columns. |
| `progress.go` | Spinners and the download bar. |
| `help.go` | General and per-command help. |

`doc.go` gives the same map in the package documentation, so `go doc` and an
editor show it too.

No external dependencies or terminal UI library are required.

The project page in `docs/` is plain HTML, CSS and JavaScript with no build
step, served by GitHub Pages from the `/docs` folder of `main`. Keep it working
without a network: no CDN, no web fonts, no analytics.

## Adding a command

Five places, all of them:

1. `internal/cli/run.go` — add the name to `validCommand`.
2. `internal/cli/run.go` — add a `case` to the switch in `run`, and reject unwanted
   arguments the way the neighbouring cases do.
3. `internal/cli/run.go` — if the command takes options, handle them in
   `parseOptions` and reject the flags it does not accept.
4. `internal/cli/help.go` — add a `helpTopics` entry and a line to the general
   help.
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

## Release build flags

During development, build from source with Go 1.27.1 or newer:

```sh
go build .
```

That binary is larger than a released one, because it keeps the symbol table
and debug information. The release workflow strips them, bakes the version in,
and disables cgo; the same command locally is:

```sh
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=1.0.0" .
```

On Windows, set `CGO_ENABLED=0` in the environment before running the command.

`-s -w` removes the symbol table and DWARF, which is most of the size
difference, and `-trimpath` keeps build-machine paths out of the binary.
Panic messages still name functions and lines, because the runtime's own
function table is not removed. `CGO_ENABLED=0` keeps the build pure Go, which
on Linux produces a statically linked binary with no libc dependency, and it is
what the release workflow uses. Use the version you are building in place of
`1.0.0`; release archives take it from the tag.

The same command with the same toolchain on the same commit produces a
byte-identical binary, so a downloaded archive can be checked by rebuilding it.
A working tree with uncommitted changes is not identical to a released one: the
VCS stamp records that it was modified.

### Windows icon

`icon_windows_amd64.syso` in the repository root gives `asm.exe` its icon.
`go build` links any `*_windows_amd64.syso` beside `main.go` into a Windows
x64 build on its own, so no flag or build dependency is involved, and other
targets ignore the file. It holds the icon only, without a manifest, so the
executable behaves exactly as it would without it.

The icon is `.github/assets/icon.ico`, the page's favicon drawn at sizes from
16 to 256 pixels with JetBrains Mono Bold. After changing it, regenerate the
object file from the repository root and commit both:

```sh
go run github.com/tc-hib/go-winres@v0.3.3 simply --icon .github/assets/icon.ico --manifest none --arch amd64 --out icon
```

`go run` with a version fetches the tool for that one command; it is not added
to `go.mod`.

## Downloads and SDK assembly

`install` and `update` share SDK assembly logic. asm tries the official API
manifest first and falls back to the full host archive with the compiler from
the shockpkg metadata and archive.org. An explicit `API_ENDPOINT` replaces the
official base. Component recipes select `window`, `macos`, or
`linux` for the current host and retain common tools, Android, and iPhone
components. Other desktop OS components are excluded.

Files are assembled in a temporary directory inside `AIR_SDKS`. Download size,
SHA-256, ZIP paths, the host's `bin/adt` (`adt.bat` on Windows), and `lib/adt.jar`
are checked. Unix executable permissions and safe relative symbolic links are
preserved. Linux SDKs run `configure_linux.sh` when provided; ARM64 requires
that script and passes `arm64`. Legacy Linux runtime/library paths are corrected
as in the inspected manager. macOS quarantine is inspected and removed if
present, without requesting administrator privileges. A compatible
`air-sdk-description.xml` is generated before the installation is moved into
place. Download or extraction failures leave existing SDKs untouched.

Archives are deleted after extraction. Temporary directories and the operation
lock are removed on completion, and a canceled operation cleans up after
itself. An update temporarily renames the old SDK for rollback and deletes it
after a successful replacement. Persistent `.asm-backups` and archive caches
are not created. If rollback itself fails, the original files are retained and
their location is included in the error.

Install and update share a lock for the SDK root. Another concurrent writer
fails instead of changing the same SDKs. Official API requests have a 6-second
timeout, the mirror and announcement requests 15 seconds, and each archive
download attempt 600 seconds. A transfer that receives no data for 30 seconds
is interrupted and retried.

An interrupted download is kept in `AIR_SDKS/.asm-partial`, named after the
expected SHA-256, and continues from the stored bytes on the next attempt or
run: up to four attempts with 1, 2, and 4-second delays, then the remaining
bytes are kept for a later run. `asm list` and `uninstall` ignore that
directory. A verified download replaces the partial file; data that fails
verification, and leftovers older than seven days, are deleted.

## Sources and fallback options

The network findings below were recorded on **October 5, 2026**. Availability
can change, and measured request times are not speed guarantees. These notes
are retained for switching sources or revisiting an alternative.

### Version catalogs

| Source | Use and limitations |
| --- | --- |
| [HARMAN API](https://api.airsdk.harman.com/releases?types=production) | Default catalog: a `releases` array with release types. A probe returned HTTP 200 containing `Sandbox.Timedout` after roughly ten seconds; a VPN did not fix the server error. Validate JSON, not only HTTP status. asm tries it first and falls back on failure. |
| [AIR SDK announcement archive](https://airsdk.dev/news/archive) | First fallback for search, short-version resolution, and update checks. About 18 KB and one second in a probe. Newest release at the time was `51.4.1.1`, announced September 23, 2026. Unannounced builds may be absent. |
| AIR SDK Manager database | Last-resort fallback: `availableSDKs`, `latestSDKs`, and `installableSDKs`, each containing `build` metadata. Main database first, then newest usable backups. An existing local snapshot, not a guarantee of freshness. |
| [RSS](https://airsdk.dev/news/rss.xml) and [Atom](https://airsdk.dev/news/atom.xml) | Investigated alternatives to HTML parsing, generally covering recent announcements rather than a complete history. Not used automatically. |
| [News source repository](https://github.com/airsdk/airsdk.dev/tree/main/news) | Another way to obtain published announcements. GitHub API rate limits apply; unannounced builds remain absent. Not currently used. |
| [shockpkg catalog](https://shockpkg.github.io/packages/api/1/packages.json) | More archives, including unannounced builds; approximately 3.9 MB in the inspected snapshot. No production/prerelease classification, so stable short-version resolution needs another source. Used as the fallback download source. |

An explicit `API_ENDPOINT` in the manager configuration replaces the default
`https://api.airsdk.harman.com` base for catalog and manifest requests. Entries
explicitly typed as non-production are excluded. Catalog requests try the API,
then the announcement archive, then the manager database; the warning names the
source that answered.

### SDK download sources

| Source | Use and findings |
| --- | --- |
| HARMAN components | Default recipe: `POST /releases/components/<name>/<component-version>` with form data `acceptedLicense=true`. Probes returned 403. Used when the manifest lists `components`. |
| HARMAN full archives | Recipe for a manifest that carries `urls.AIR_Win` with `url`, `checksum`, and `fileSize`. Relative URLs use `https://airsdk.harman.com`; that site's URLs receive `license=accepted`. A direct probe returned 403. |
| [shockpkg packages](https://github.com/shockpkg/packages) → [JSON catalog](https://shockpkg.github.io/packages/api/1/packages.json) → archive.org | Fallback recipe when the official API fails at any step: `air-sdk-<full-version>-<windows\|mac\|linux>-compiler`, using `source`, `sha256`, and `size`. The `51.4.1.1` catalog contains all three host archives. A 32-byte probe of Windows SDK `51.3.4.3` returned HTTP 206 and a ZIP signature in 1.8 seconds. Hashes come from shockpkg. |
| [HARMAN website API](https://airsdk.harman.com/download) | Investigated `/api/versions/release-notes`, `/api/config-settings/download`, and `/api/versions/<full-version>`. Probes exceeded 15 seconds, including requests with browser headers. Worth checking again if service availability changes. |

`/releases/<full-version>`, `/releases/versions/<version>`, and
`/releases/recent/30` returned 502 during probes. `/releases/<full-version>/urls`
responded quickly but provided string URLs without hashes, insufficient for
the current verified installation recipe.

asm now tries the official API first and falls back to the mirror recipe when
the manifest or a component download fails, so a 403 on the components route
still installs from the mirror. Verifying the live service against the catalog,
production classification, exact manifest, and an actual Windows component
download is still pending. The mirror recipe remains an independent option. No
private package server is required.

## Manager configuration on macOS and Linux

The inspected manager code uses `File.userDirectory.resolvePath(".airsdk")`,
then reads `airsdkmanager.cfg`. Mapping this through the
[AIR File.userDirectory documentation](https://airsdk.dev/reference/actionscript/3.0/flash/filesystem/File.html#userDirectory)
gives the following locations:

| Platform | Configuration | Database and SDK directory |
| --- | --- | --- |
| Windows | `%USERPROFILE%\.airsdk\airsdkmanager.cfg` | `airsdkmanager.db` beside the configuration; SDK root from `AIR_SDKS`. |
| macOS | `~/.airsdk/airsdkmanager.cfg`, usually `/Users/<user>/.airsdk/airsdkmanager.cfg` | Same database name and `AIR_SDKS` setting. |
| Linux | `~/.airsdk/airsdkmanager.cfg`, usually `/home/<user>/.airsdk/airsdkmanager.cfg` | Same database name and `AIR_SDKS` setting. |

These paths are established from the manager code and runtime documentation.
Fixture checks run on all five release targets in GitHub Actions. The manager uses the
home directory directly, rather than macOS Application Support or an XDG
configuration directory. Nonstandard home directories must also work.

asm writes the same file, and only two keys. When `AIR_SDKS` is missing, the
first command that needs the SDK folder sets it to `~/sdks/air`, the location
the [airsdk.dev installation guides](https://airsdk.dev/docs/basics/install/windows)
recommend (`C:\Users\<user>\sdks\air`, `/Users/<user>/sdks/air`). A yes to the
license question saves `HAS_ACCEPTED_LICENSE=true`, the line the manager writes
when its license is accepted. `saveSetting` replaces the line that sets the key,
or appends one, and keeps comments, other keys, a byte order mark and CRLF line
endings as they were, so the manager reads the file exactly as before.

## Researching AIR SDK Manager

The CLI was developed after inspecting the **Linux amd64 build of AIR SDK
Manager 1.4.0**, `AIRSDKManager_linux_amd64_1.4.0.zip`, obtained from the
[official GitHub releases](https://github.com/airsdk/airsdkmanager-releases/releases).
The archive was extracted and its decompiled ActionScript inspected to
understand the existing settings, download protocol, and SDK assembly process.

The main references were `AIRSDKAPI`, `AIRSDKBuild`, `AIRSDKDescription`,
`AIRSDKDownloadProcess`, `AIRSDKAssembleProcess`, and
`CreateSDKDescriptionProcess`. They established the following:

- Settings under `.airsdk`, `AIR_SDKS`, and discovery through SDK descriptions.
- API base `https://api.airsdk.harman.com`, `/releases`,
  `/releases/<full-version>`, and `/releases/versions/<version>`.
- `build.components` with independent component versions, SHA-256 `checksum`,
  and `fileSize`; component numbers need not match the SDK number.
- License-accepting POST downloads, sequential assembly into one directory,
  and full-archive recipes from `urls`.
- XML with `air-sdk-description`, `name`, a three-component `version`, and `build`.
- Updating the build component while preserving an installation's path.

The inspected `51.4.1.1` manifest contained `linux`, `core-tools`, `iphone`,
`air-tools`, `macos`, `window`, and `android`. The GUI caches component archives;
asm removes them after extraction. The Go implementation includes Linux
configuration, executable permissions, safe symbolic links, and macOS quarantine
handling. Manager binaries and decompilation are excluded from this repository.

## Related projects and command naming

| Project | Relevance |
| --- | --- |
| [AIR SDK Manager](https://github.com/airsdk/airsdkmanager-releases) | GUI for SDKs, related tools, licenses, and configuration; reference for the inspected protocol. |
| [shockpkg CLI](https://github.com/shockpkg/cli) | Closest discovered package CLI for Flash/AIR. Installed with `npm install -g @shockpkg/cli`; supports install, installed, remove, and verify. Its `update` refreshes the catalog; `upgrade` updates packages. |
| [AIR Package Manager](https://github.com/airsdk/apm) | SWC/ANE dependencies and application descriptors; potential companion tool. |
| [setup-adobe-air-action](https://github.com/joshtynjala/setup-adobe-air-action) | CI example for exact/short versions, license acceptance, `AIR_HOME`, and `PATH`. |

The search did not find a dedicated AIR CLI combining short-version install,
project SDK selection, and a user default. This is a search finding, not a
claim that no such tool exists.

**The command name `asm` is already used by unrelated projects**, including
[Agent Skill Manager](https://github.com/luongnv89/asm) and
[Assemble](https://getassemble.dev/docs/cli). It is not a globally unique name.
The installers detect conflicting commands before adding asm to `PATH` and
refuse to overwrite or silently shadow another tool.

### Package-manager conventions

| Operation | [apt](https://manpages.debian.org/trixie/apt/apt.8.en.html) | [winget](https://learn.microsoft.com/en-us/windows/package-manager/winget/) | [Chocolatey](https://docs.chocolatey.org/en-us/choco/commands/) | asm |
| --- | --- | --- | --- | --- |
| Install | `install` | `install` | `install` | `install VERSION` |
| Search | `search` | `search` | `search` | `search [VERSION]` |
| Installed | `list --installed` | `list` | `list` | `list`, `ls` |
| Update installed | `upgrade` | `upgrade`, alias `update` | `upgrade` | `update` |
| Refresh catalog | `update` | `source update` | No separate `update` command listed | Automatic before search and update checks |
| Remove | `remove` | `uninstall` | `uninstall` | `uninstall VERSION`, alias `remove` |

The chosen SDK-update name is `update`. Its default preview and `--all` behavior
resemble [winget upgrade](https://learn.microsoft.com/en-us/windows/package-manager/winget/upgrade).
asm manages one product, so a version replaces the usual package name.
[winget](https://learn.microsoft.com/en-us/windows/package-manager/winget/install)
and [Chocolatey](https://docs.chocolatey.org/en-us/choco/commands/install/)
use `--version` for an exact package version; apt uses `PKG=VERSION`. Short AIR
version resolution is an asm-specific rule. New short flags need unambiguous
help because other managers assign them different meanings.

## Builds and releases

The GitHub Actions `Build` workflow runs tests and `go vet` on native runners
and builds these targets. Each job also installs and reinstalls its own archive
with the matching bootstrap installer and checks the installed version:

| Host | Go target | Release archive |
| --- | --- | --- |
| Windows x64 | `windows/amd64` | `asm_<version>_windows_amd64.zip` |
| macOS Intel | `darwin/amd64` | `asm_<version>_darwin_amd64.tar.gz` |
| macOS Apple Silicon | `darwin/arm64` | `asm_<version>_darwin_arm64.tar.gz` |
| Linux x64 | `linux/amd64` | `asm_<version>_linux_amd64.tar.gz` |
| Linux ARM64 | `linux/arm64` | `asm_<version>_linux_arm64.tar.gz` |

Each archive contains the executable and README. Ordinary commits and pull
requests produce workflow artifacts kept for seven days. Pushing a version
tag such as `v1.0.0` publishes the five archives and `SHA256SUMS` to
[GitHub Releases](https://github.com/emptyenemy/asm/releases) after every build
passes. The tag supplies the binary version, without the `v` prefix.
Releases use the latest stable patch of Go 1.27; their OS minimums follow that
toolchain. New dependencies should use their current stable versions.

Version 1.0.0 is tagged `v1.0.0`; its archives come from the workflow above.

## Current development plan

The Go CLI implements the existing commands, `uninstall`/`remove`, terminal
presentation, home-based configuration, host archive selection, platform setup,
an official-API-first source chain with named fallbacks, and resumable
downloads with backoff retries. Native builds, fixture tests, and release
installers cover Windows amd64, macOS amd64/arm64, and Linux amd64/arm64.

Full SDK downloads and tool execution still need validation on each supported
host. The
[Linux SDK documentation](https://airsdk.dev/docs/basics/install/linux)
explicitly supports x86_64 and ARM64; Linux SDK tools require a commercial
AIR license. See also the
[macOS](https://airsdk.dev/docs/basics/install/macos) and
[Windows](https://airsdk.dev/docs/basics/install/windows) installation guides.

Each feature is committed separately. Development continues on `1.0.0`;
published releases and tags will be added only once that version is ready.

### Planned for 1.0.0

No command beyond the current set is planned; the remaining work is validation
rather than new features.

The command set is deliberately small: `list`, `search`, `install`, `update`,
`uninstall`, and `clean`. SDK selection, running tools, and other extras are
out of scope. License acceptance remains explicit: a question in a terminal,
`--accept-license` everywhere else, never a default.

Go is the implementation language. Use its standard HTTP/ZIP support and keep
the application small, adding layers only when a concrete feature needs them.
Measure real downloads and extraction before making speed claims.
macOS/Linux SDK archives and setup must be exercised on each supported target;
shipping an asm binary does not imply SDK availability for every architecture.
[Linux ARM installation documentation](https://airsdk.dev/docs/basics/install/linux)
is a starting point.

## Validation

Run `go test ./...` and `go vet ./...`. Tests create small SDK/ZIP fixtures in
temporary directories and use local HTTP servers. They exercise numeric
versions, idempotent installation, spaces/Unicode, licenses, source failures,
hashes, sizes, retries, stalled transfers, resumption across runs, source
fallback, ZIP traversal, SDK structure, locking, occupied destinations,
cleanup, leftover sweeping and its `--check` mode, configuration/license
preservation, new-branch notifications, and
uninstall ambiguity. Linux checks also cover architecture setup and legacy
path corrections; Unix checks cover symbolic links. Test directories are
removed automatically. Full SDK downloads and tools still need separate
validation on each target.

Bootstrap checks run from the archives built by GitHub Actions, using isolated
installation directories without changing `PATH`. Local installer checks on
Windows (PowerShell 5.1 and 7) and Linux also cover repeat installation,
checksum failures, unrelated files, archive contents, cleanup, and paths with
spaces and special characters. Shell profile changes are tested in an isolated
home directory.
