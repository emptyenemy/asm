// Package cli is everything behind asm's main: the commands, the AIR SDK
// catalogs and downloads, SDK assembly, and what is printed on the way.
//
// Commands: run.go parses the arguments and dispatches; install.go,
// update.go, uninstall.go and clean.go implement the commands that change
// SDKs.
//
// SDKs: version.go parses and compares version numbers, config.go reads the
// AIR SDK Manager settings, installed.go finds installed SDKs, catalog.go
// lists releases and loads build manifests, http.go makes the requests,
// download.go fetches archives, extract.go unpacks them, and assemble.go
// turns a manifest into a working SDK.
//
// Platform: platform.go maps the host to an SDK build, and platform_unix.go
// and platform_windows.go hold what differs per OS. paths.go keeps every
// write inside the SDK directory.
//
// Output: terminal.go lays out and colors text, progress.go draws spinners
// and the download bar, and help.go holds the help.
package cli
