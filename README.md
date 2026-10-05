# asm

A command-line version manager for AIR SDK. List installed SDKs, find available
releases, install a version, and update existing installations.

Version **1.0.0 is in development**. There are no published releases yet.
The implementation uses **Go** and its standard library. One native executable
runs on Windows, macOS, or Linux without PowerShell or a Go installation.

## Getting started

During development, build from source with Go 1.25 or newer:

```sh
go build .
```

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
each command. SDK discovery through `PATH` and additional saved roots are planned.

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
Command and fixture checks have run on Windows and Linux. The manager uses the
home directory directly, rather than macOS Application Support or an XDG
configuration directory. Nonstandard home directories must also work.

## Terminal output

General help includes a compact ASCII banner. Interactive output uses an
indigo accent (`#818CF8`), aligned version/path columns, and suggestions for the
next command. Narrow terminals use stacked entries and wrapped paths.

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
| `asm install <VERSION>` | Install a branch, an exact build, or `latest`. |
| `asm uninstall <VERSION>`, `asm remove <VERSION>` | Delete one installed SDK selected by exact version or an unambiguous prefix. |
| `asm update` | Show updates for installed SDKs and announce a newer uninstalled branch. |
| `asm update <VERSION>` | Update matching installed SDKs. |
| `asm update --all` | Update every installed SDK that has a newer build. |
| `asm update <VERSION> --check` | Preview updates for selected SDKs. |
| `asm update --all --check` | Preview all installed updates and the new-branch notice. |

Angle brackets mark required arguments; square brackets mark optional ones.
Replace `VERSION` with a number or `latest`; do not type the brackets.

Help is also available through `asm --help`, `asm -h`, and commands such as
`asm install --help` or `asm update -h`. `install` currently requires a version.
`install --version VERSION`, `--json`, and other undocumented options are not
implemented yet.

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

`install` and `update` share SDK assembly logic. By default, asm downloads the
full host OS archive with the compiler using shockpkg metadata and archive.org.
An explicit `API_ENDPOINT` enables that API's manifest or an exact manifest
from the manager database. Component recipes select `window`, `macos`, or
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

Archives are deleted after extraction. Temporary directories, incomplete
downloads, and the operation lock are removed on completion. An update
temporarily renames the old SDK for rollback and deletes it after a successful
replacement. Persistent `.asm-backups` and archive caches are not created.
If rollback itself fails, the original files are retained and their location is
included in the error.

Install and update share a lock for the SDK root. Another concurrent writer
fails instead of changing the same SDKs. Catalog and manifest requests have a
15-second timeout; a large archive download has a 600-second timeout.

## Sources and fallback options

The network findings below were recorded on **October 5, 2026**. Availability
can change, and measured request times are not speed guarantees. These notes
are retained for switching sources or revisiting an alternative.

### Version catalogs

| Source | Use and limitations |
| --- | --- |
| [AIR SDK announcement archive](https://airsdk.dev/news/archive) | Current source for search, short-version resolution, and update checks. About 18 KB and one second in a probe. Newest release at the time was `51.4.1.1`, announced September 23, 2026. Unannounced builds may be absent. |
| [HARMAN API](https://api.airsdk.harman.com/releases?types=production) | GUI's native catalog: a `releases` array with release types. A probe returned HTTP 200 containing `Sandbox.Timedout` after roughly ten seconds; a VPN did not fix the server error. Validate JSON, not only HTTP status. A future primary source once healthy. |
| AIR SDK Manager database | Current fallback: `availableSDKs`, `latestSDKs`, and `installableSDKs`, each containing `build` metadata. Main database first, then newest usable backups. An existing local snapshot, not a guarantee of freshness. |
| [RSS](https://airsdk.dev/news/rss.xml) and [Atom](https://airsdk.dev/news/atom.xml) | Investigated alternatives to HTML parsing, generally covering recent announcements rather than a complete history. Not used automatically. |
| [News source repository](https://github.com/airsdk/airsdk.dev/tree/main/news) | Another way to obtain published announcements. GitHub API rate limits apply; unannounced builds remain absent. Not currently used. |
| [shockpkg catalog](https://shockpkg.github.io/packages/api/1/packages.json) | More archives, including unannounced builds; approximately 3.9 MB in the inspected snapshot. No production/prerelease classification, so stable short-version resolution needs another source. Used for downloads. |

An explicit `API_ENDPOINT` in the manager configuration switches catalog
requests to `GET <API_ENDPOINT>/releases?types=production`. Entries explicitly
typed as non-production are excluded. On failure, the manager database remains
the fallback. There is no `--source` command option yet.

### SDK download sources

| Source | Use and findings |
| --- | --- |
| [shockpkg packages](https://github.com/shockpkg/packages) → [JSON catalog](https://shockpkg.github.io/packages/api/1/packages.json) → archive.org | Default recipe: `air-sdk-<full-version>-<windows\|mac\|linux>-compiler`, using `source`, `sha256`, and `size`. The `51.4.1.1` catalog contains all three host archives. A 32-byte probe of Windows SDK `51.3.4.3` returned HTTP 206 and a ZIP signature in 1.8 seconds. Hashes come from shockpkg. |
| HARMAN components | Native manager recipe: `POST /releases/components/<name>/<component-version>` with form data `acceptedLicense=true`. Probes returned 403. Supported for an explicit endpoint with component metadata. |
| HARMAN full archives | Fallback recipe: `urls.AIR_Win` with `url`, `checksum`, and `fileSize`. Relative URLs use `https://airsdk.harman.com`; that site's URLs receive `license=accepted`. A direct probe returned 403. |
| [HARMAN website API](https://airsdk.harman.com/download) | Investigated `/api/versions/release-notes`, `/api/config-settings/download`, and `/api/versions/<full-version>`. Probes exceeded 15 seconds, including requests with browser headers. Worth checking again if service availability changes. |

`/releases/<full-version>`, `/releases/versions/<version>`, and
`/releases/recent/30` returned 502 during probes. `/releases/<full-version>/urls`
responded quickly but provided string URLs without hashes, insufficient for
the current verified installation recipe.

Before switching back to the official API, verify the catalog, production
classification, exact manifest, and an actual Windows component download.
The mirror recipe remains an independent option. No private package server is
required.

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
The planned installer must detect conflicting commands before adding asm to
`PATH` and avoid overwriting or silently shadowing another tool.

### Package-manager conventions

| Operation | [apt](https://manpages.debian.org/trixie/apt/apt.8.en.html) | [winget](https://learn.microsoft.com/en-us/windows/package-manager/winget/) | [Chocolatey](https://docs.chocolatey.org/en-us/choco/commands/) | asm |
| --- | --- | --- | --- | --- |
| Install | `install` | `install` | `install` | `install VERSION` |
| Search | `search` | `search` | `search` | `search [VERSION]` |
| Installed | `list --installed` | `list` | `list` | `list`, `ls` |
| Update installed | `upgrade` | `upgrade`, alias `update` | `upgrade` | `update` |
| Refresh catalog | `update` | `source update` | No separate `update` command listed | Automatic before search and update checks |
| Remove | `remove` | `uninstall` | `uninstall` | `uninstall VERSION`, alias `remove` |
| Details | `show` | `show` | `info` | Planned `show`, alias `info` |

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
and builds these targets:

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
Releases use Go 1.27; their OS minimums follow that toolchain.

Version 1.0.0 is still being developed. No version tag or release has been
published as part of this migration.

## Current development plan

The Go CLI implements the existing commands, `uninstall`/`remove`, terminal
presentation, home-based configuration, host archive selection, and platform
setup. The remaining steps in this iteration are:

1. **Validate supported SDK hosts.** Target Windows amd64, macOS amd64/arm64,
   and Linux amd64/arm64. Builds exist for all five targets; fixture tests have
   run on Windows and Linux. The
   [Linux SDK documentation](https://airsdk.dev/docs/basics/install/linux)
   explicitly supports x86_64 and ARM64; Linux SDK tools require a commercial
   AIR license. See also the
   [macOS](https://airsdk.dev/docs/basics/install/macos) and
   [Windows](https://airsdk.dev/docs/basics/install/windows) installation guides.
   Full SDK/tool checks on each host are still required before calling the
   first version ready.
2. **Install from a release.** Provide `install.ps1` and `install.sh` that
   select the matching binary, verify its checksum, install per user, and set
   up `PATH`. Detect unrelated commands named `asm` and remove temporary files.

Each feature is committed separately. Development continues on `1.0.0`;
published releases and tags will be added only once that version is ready.

### Later capabilities

| Capability | Intended behavior |
| --- | --- |
| `show [VERSION]`, `info` | Display date, size, platform, and source. |
| `path [VERSION]` | Print an installed or selected SDK path for IDEs and scripts. |
| `use VERSION` | Save an exact build in the project's `.asm-version`. |
| `use --global VERSION`, `current` | A user default and effective selection; storage location remains undecided. |
| `exec [--sdk VERSION] -- TOOL [ARGS...]` | Run with the selected `AIR_HOME` and SDK `bin` in the child's environment, preserving arguments and exit status. |
| `install` without a version | Install the exact build from the nearest `.asm-version`. |

Future selection order: `exec --sdk`, then the nearest project `.asm-version`,
then the user default. Resolve short selections from installed SDKs and store
the full number. A missing selected SDK is an error. Parent-shell environment
changes need separate shell integration or shims.

Additional roots, environment discovery, JSON output, resumable downloads, and
limited retries follow the basic commands. Flex overlays, older Adobe SDKs,
`doctor`, completion, and APM integration are later additions. License acceptance
remains explicit.

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
hashes, sizes, ZIP traversal, SDK structure, locking, occupied destinations,
cleanup, configuration/license preservation, new-branch notifications, and
uninstall ambiguity. Linux checks also cover architecture setup and legacy
path corrections; Unix checks cover symbolic links. Test directories are
removed automatically. Full SDK downloads and tools still need separate
validation on each target.
