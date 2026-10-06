# asm

A command-line version manager for AIR SDK. List installed SDKs, find available
releases, install a version, and update existing installations.

Version **1.0.0 is in development**. There are no published releases yet.
The implementation uses **Go** and its standard library. One native executable
runs on Windows, macOS, or Linux without PowerShell or a Go installation.

## Getting started

### Install a published release

These commands will work once the first release has been published. The
installers select the native binary and verify it against the release's
`SHA256SUMS` before installation.

Windows PowerShell:

```powershell
irm https://raw.githubusercontent.com/emptyenemy/asm/main/install.ps1 | iex
```

macOS or Linux:

```sh
curl -fsSL https://raw.githubusercontent.com/emptyenemy/asm/main/install.sh | sh
```

The Windows installer puts `asm.exe` in `%LOCALAPPDATA%\Programs\asm`, updates
the user `PATH`, and makes `asm` available in the current PowerShell session.
Other open terminals need to be reopened. The Unix installer uses
`~/.local/bin` and adds it to `.zshrc`, `.bashrc`, `.bash_profile`, or `.profile`
for the detected shell. Open a new terminal or source the profile printed by
the installer. Existing `PATH` entries are kept; repeated installs replace
only an installation owned by asm. An unrelated command named `asm` stops
installation with a message identifying the conflict.

To select an asm version or installation directory, download
[install.ps1](https://raw.githubusercontent.com/emptyenemy/asm/main/install.ps1)
and run `./install.ps1 -Version 1.0.0 -InstallDirectory C:\Tools\asm`, or use
`ASM_VERSION=1.0.0` and `ASM_INSTALL_DIR=/absolute/path` when running
[install.sh](https://raw.githubusercontent.com/emptyenemy/asm/main/install.sh).
`-NoPath` / `ASM_NO_PATH=1` leaves shell configuration unchanged.
For local release archives, use `-ArchiveDirectory` / `ASM_ARCHIVE_DIR` with an
explicit version and a directory containing the matching archive and
`SHA256SUMS`. Archives, staging files, and previous asm binaries are removed
after successful installation. The installer configures the asm command;
AIR SDK paths still come from AIR SDK Manager settings.

### Build from source

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

From the project directory in cmd after building:

```bat
asm --version
asm list
asm search 51.4
asm install 51.4
asm update
```

In PowerShell, use `./asm.exe`; on macOS/Linux, use `./asm`. Add the executable's
directory to `PATH` to run `asm` from other directories. `asm --version` and
`asm -v` print only `1.0.0`, with an indigo accent in an interactive terminal.

## SDK locations and configuration

asm reads the existing AIR SDK Manager configuration. It does not rewrite the
manager's settings or database. The SDK directory can be anywhere:

```ini
AIR_SDKS=C:\AIRSDK
HAS_ACCEPTED_LICENSE=true
```

asm reads `~/.airsdk/airsdkmanager.cfg`, using the current user's home directory
on each OS (`%USERPROFILE%` on Windows). No directory argument is needed for
each command.

Downloading requires acceptance of the AIR SDK license: either
`HAS_ACCEPTED_LICENSE=true` in the manager configuration, or `--accept-license`
on `install`/`update`. The flag applies to the current operation and does not
change the configuration.

### Manager configuration on macOS and Linux

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

## Terminal output

General help includes a compact ASCII banner. Interactive output uses an
indigo accent (`#818CF8`), aligned version/path columns, and suggestions for the
next command. Narrow terminals use stacked entries and wrapped paths.
The bootstrap installers use the same banner, colors, and custom activity
indicators. Metadata requests show a spinner; binary downloads show bytes and
a progress bar when the server supplies a size. `NO_COLOR`, `TERM=dumb`, and
redirected output are respected by the installers too.

Catalog and manifest requests display a spinner. Downloads use a custom bar
with transferred bytes, percentage when the total is known, and average speed.
Unknown-size downloads show activity and bytes without inventing a percentage.
SHA-256 is calculated while streaming the download; extraction and platform
configuration also display activity. Progress is rendered directly by asm.

Progress goes to stderr and clears on completion or error. Redirecting either
output stream disables colors and animation; `TERM=dumb` does the same.
`NO_COLOR` disables colors while retaining activity in an interactive terminal.
Redirected search results remain one full version per line, without headings or
installation suggestions. `--version` and `-v` print only `1.0.0`, accented in
an interactive terminal and plain when redirected or with `NO_COLOR`.

The root `main.go` starts the CLI and supplies its build version. Implementation
files and tests live in one `modules` package: `app.go` handles commands and
settings, `sdk.go` handles catalogs and SDK assembly, and `terminal.go` handles
output. `platform_windows.go` and `platform_unix.go` contain terminal, locking,
and platform setup. `go.mod` declares the project's import path and minimum Go
version. No external dependencies or terminal UI library are required.

## Commands

| Command | Behavior |
| --- | --- |
| `asm help [COMMAND]` | Show general or command help. Running `asm` without arguments also shows help. |
| `asm --version`, `asm -v` | Print only the manager version: `1.0.0`. |
| `asm list`, `asm ls` | List installed SDK versions and absolute paths. |
| `asm search [VERSION]` | List announced stable releases, newest first. |
| `asm install [VERSION]` | Install a branch, an exact build, or `latest`. |
| `asm uninstall [VERSION]`, `asm remove [VERSION]` | Delete one installed SDK selected by exact version or an unambiguous prefix. |
| `asm update` | Show updates for installed SDKs and announce a newer uninstalled branch. |
| `asm update [VERSION]` | Update matching installed SDKs. |
| `asm update --all` | Update every installed SDK that has a newer build. |
| `asm update [VERSION] --check` | Preview updates for selected SDKs. |
| `asm update --all --check` | Preview all installed updates and the new-branch notice. |
| `asm clean` | Remove incomplete downloads and temporary directories left by an interrupted operation. |
| `asm clean --check` | List what `clean` would change without changing anything. |

Square brackets mark a placeholder: replace `[VERSION]` with the appropriate
value and do not type the brackets. `install` and `uninstall` require a
version; `search`, `update`, and `help` accept one optionally.

Help is also available through `asm --help`, `asm -h`, and commands such as
`asm install --help` or `asm update -h`. `install` requires a version.

Errors and warnings go to stderr. Successful commands return `0`; errors return
`1`. Unknown commands and unexpected arguments are rejected.

### List

```bat
asm list
```

Only immediate subdirectories of `AIR_SDKS` are examined, as in AIR SDK Manager.
Folder names are arbitrary. Versions come from `air-sdk-description.xml`:
`<version>51.3.4</version>` and `<build>3</build>` produce `51.3.4.3`.
A four-component number in `<version>` is also supported.

Versions are sorted numerically, newest first. Directories without a description are skipped;
invalid descriptions produce a warning. Missing settings, an empty `AIR_SDKS`,
or a missing SDK root are errors. An existing empty root produces an explanatory
message and returns `0`.

### Search

```bat
asm search
asm search 51.4
asm search 51.4.1.1
```

Filters match version components: `51.4` does not match `51.40`. An exact
four-component filter requires an exact match. Results are full build numbers,
newest first. No matches produce an explanatory message and return `0`.

The announcement archive includes fresh releases but may omit older builds
without individual announcements. Beta, alpha, and preview entries are excluded
by title. Known previews `51.0.0.2` and `51.0.0.4`, titled as releases, are
excluded explicitly.

When the source fails, asm reads `airsdkmanager.db`, then
`airsdkmanager.db.backup*` from newest to oldest. A warning identifies the
database used; its contents may be stale. Failure to obtain any usable catalog
is an error. This existing GUI database is separate from SDK archives.

### Install

```bat
asm install 51.4
asm install 51.4.1.1
asm install latest
asm install 51.4 --accept-license
```

`51`, `51.4`, and `51.4.1` resolve to the newest matching stable build;
`latest` resolves to the newest stable version. All four components are compared
numerically. An exact version queries the download source directly, allowing
installation of builds without an announcement. An unavailable version causes
an error instead of selecting a different build.

Version `51.4.1.1` installs into `AIR_SDKS\AIRSDK_51.4.1.1`. A missing root is
created. An existing matching version is detected by its description, regardless
of the directory name, and is reported without reinstalling it. An occupied
destination containing other files is rejected. Installation does not change
the selected SDK or `PATH`.

### Update

```bat
asm update
asm update 51.3 --check
asm update 51.3
asm update 51.3.4.1
asm update --all
asm update --all --accept-license
```

Without arguments, asm shows installed versions, available updates, and paths.
It also announces the newest stable release if it is newer than every installed
SDK and its three-component version is not installed:

```text
Installed SDKs are up to date.

New AIR SDK available: 51.4.1.1
Install: asm install 51.4
```

The notice also appears with `--check`, `--all`, and `--all --check`. It disappears
once that branch is installed. An empty SDK root receives a suggestion to
install the newest stable version. A positional filter restricts the overview
to matching installed SDKs.

`--all` applies updates to existing installations. A version selects installed
SDKs by prefix; a full number selects the current installation, not the target
build. `--check` can appear before or after the number. A number and `--all`
cannot be combined.

As in AIR SDK Manager, an update changes only the fourth component:
`51.3.4.1` to `51.3.4.3`. Installed `51.3.3` and `51.3.4` remain separate SDKs.
New branches are installed with `install`. The SDK path, `lib/adt.cfg`, and
`lib/adt.lic` are preserved, keeping existing tool paths valid.

No matching installation is an error with an `asm list` suggestion. No available
update is a successful result. `--all` processes SDKs sequentially and stops at
the first error; completed updates remain installed.

### Uninstall

```sh
asm uninstall 51.4
asm uninstall 51.3.4.3
asm remove 51.4.1.1
```

An exact build or a prefix must match exactly one installed SDK. Ambiguous
prefixes show the matching versions and paths and return an error; no SDK is
deleted. No match is also an error. `latest`, `--all`, and removing several
versions in one command are not supported.

The command deletes the selected SDK directory directly, without keeping a
backup. It validates the SDK description and its location below `AIR_SDKS`,
rejects symbolic-link directories, and shares the install/update lock. Manager
settings, other SDKs, and `PATH` remain unchanged. If files are locked or removal
otherwise fails, the error identifies the directory that needs attention.

## Downloads and cleanup

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

### Leftovers and `asm clean`

Cancelling with `Ctrl+C` stops the operation and cleans up what it staged.
Closing the terminal window, pressing `Ctrl+C` twice, killing the process, or
losing power skips that cleanup, and `asm clean` is the way back:

| Leftover | What `clean` does |
| --- | --- |
| `.asm-partial` | Removes incomplete downloads. The next install starts over instead of resuming. |
| `.asm-install-*`, `.asm-update-*` | Removes staging directories from an interrupted install or update. |
| `.asm-old-*` | An SDK renamed away for an update rollback: moved back when that build is no longer installed, removed when it is. |
| An empty version directory | Removed. A directory created just before its contents were moved in would otherwise block installing that version again. |

`clean` takes the same SDK root lock as `install` and `update`, so it refuses
to run beside them and releases the lock file on exit. Installed SDKs,
settings, and unrelated files are never touched. A saved SDK whose description
cannot be read is reported and left in place. `clean --check` prints the same
report without changing anything.

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

Version 1.0.0 is still being developed. No version tag or release has been
published as part of this migration.

## Current development plan

The Go CLI implements the existing commands, `uninstall`/`remove`, terminal
presentation, home-based configuration, host archive selection, platform setup,
an official-API-first source chain with named fallbacks, and resumable
downloads with backoff retries. Native builds, fixture tests, and release
installers cover Windows amd64, macOS amd64/arm64, and Linux amd64/arm64.

Before the first release, full SDK downloads and tool execution still need
validation on each supported host. The
[Linux SDK documentation](https://airsdk.dev/docs/basics/install/linux)
explicitly supports x86_64 and ARM64; Linux SDK tools require a commercial
AIR license. See also the
[macOS](https://airsdk.dev/docs/basics/install/macos) and
[Windows](https://airsdk.dev/docs/basics/install/windows) installation guides.

Each feature is committed separately. Development continues on `1.0.0`;
published releases and tags will be added only once that version is ready.

### Planned for 1.0.0

No command beyond the current set is planned; the remaining work is validation
rather than new features. A version tag is deliberately withheld until that
validation passes.

The command set is deliberately small: `list`, `search`, `install`, `update`,
`uninstall`, and `clean`. SDK selection, running tools, and other extras are
out of scope. License acceptance remains explicit.

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
