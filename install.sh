#!/bin/sh
set -eu

repository=https://github.com/emptyenemy/asm
version=${ASM_VERSION:-latest}
install_dir=${ASM_INSTALL_DIR:-"$HOME/.local/bin"}
archive_dir=${ASM_ARCHIVE_DIR:-}
no_path=${ASM_NO_PATH:-0}

interactive=0
accent= muted= reset=
width=${COLUMNS:-80}
if [ -t 1 ] && [ -t 2 ] && [ "${TERM:-}" != dumb ]; then
    interactive=1
    width=$(stty size <&2 2>/dev/null | awk '{print $2}')
    width=${width:-80}
    if [ "${NO_COLOR+x}" != x ]; then
        accent=$(printf '\033[38;2;129;140;248m')
        muted=$(printf '\033[90m')
        reset=$(printf '\033[0m')
    fi
fi
case "$width" in ''|*[!0-9]*) width=80 ;; esac
[ "$width" -gt 1 ] || width=80
highlight() { printf '%s%s%s\n' "$accent" "$*" "$reset"; }
status() { printf '%s%s%s\n' "$muted" "$*" "$reset"; }
clear_activity() {
    [ "$interactive" -eq 0 ] || printf '\r%*s\r' "$((width - 1))" '' >&2
}
draw_activity() {
    case $((frame % 4)) in 0) glyph='|' ;; 1) glyph='/' ;; 2) glyph='-' ;; 3) glyph='\' ;; esac
    text="  $glyph  $label  $((frame / 10))s"
    if [ "$download" -eq 1 ] && [ -f "$destination" ]; then
        received=$(wc -c < "$destination" | tr -d ' ')
        total=$(awk 'tolower($1) == "content-length:" {n=$2} END {gsub("\r", "", n); print n+0}' "$work/headers")
        stats=$(awk -v n="$received" -v total="$total" -v frames="$frame" '
            function bytes(n, u, i) { u="B KB MB GB"; split(u, a, " "); i=1; while(n>=1000 && i<4) {n/=1000; i++} n=int(n*100+0.5)/100; return (n==int(n) ? sprintf("%.0f", n) : sprintf("%.2f", n)) " " a[i] }
            BEGIN { text=bytes(n); if(total>0) text=text " / " bytes(total); if(frames>0) text=text "  " bytes(n*10/frames) "/s"; print text }')
        percent='  --'
        if [ "$total" -gt 0 ]; then percent=$(printf '%3d%%' "$((received * 100 / total))"); fi
        if [ "$width" -ge 60 ]; then
            bar_width=$((width - ${#stats} - 14))
            [ "$bar_width" -le 28 ] || bar_width=28
            [ "$bar_width" -ge 8 ] || bar_width=8
            filled=0
            if [ "$total" -gt 0 ]; then filled=$((received * bar_width / total)); fi
            [ "$filled" -le "$bar_width" ] || filled=$bar_width
            bar= i=0
            while [ "$i" -lt "$bar_width" ]; do
                if [ "$total" -gt 0 ] && [ "$i" -lt "$filled" ]; then bar="$bar="
                elif [ "$total" -gt 0 ] && [ "$i" -eq "$filled" ]; then bar="$bar>"
                elif [ "$total" -le 0 ] && [ "$i" -eq "$((frame % bar_width))" ]; then bar="$bar>"
                else bar="$bar."; fi
                i=$((i + 1))
            done
            text="  [$bar] $percent  $stats"
        else text="  $glyph  $percent  $stats"; fi
    fi
    text=$(printf '%s' "$text" | cut -c "1-$((width - 1))")
    printf '\r%s%s%s%*s' "$accent" "$text" "$reset" "$((width - 1 - ${#text}))" '' >&2
}
if [ "$interactive" -eq 1 ]; then
    printf '\n'
    if [ "$width" -ge 26 ]; then
        highlight '    ____ __________ ___'
        highlight '   / __ `/ ___/ __ `__ \'
        highlight '  / /_/ (__  ) / / / / /'
        highlight '  \__,_/____/_/ /_/ /_/'
    else highlight '  asm'; fi
    printf '\n'; status '  AIR SDK Manager installer'; printf '\n'
fi

fail() { printf 'Error: %s\n' "$*" >&2; exit 1; }
case $(uname -s) in
    Darwin) platform=darwin ;;
    Linux) platform=linux ;;
    *) fail 'This installer supports macOS and Linux. Use install.ps1 on Windows.' ;;
esac
case $(uname -m) in
    x86_64|amd64) arch=amd64 ;;
    arm64|aarch64) arch=arm64 ;;
    *) fail 'No AIR SDK host build is available for this architecture.' ;;
esac
if [ -z "$archive_dir" ]; then command -v curl >/dev/null 2>&1 || fail 'curl is required.'; fi
command -v tar >/dev/null 2>&1 || fail 'tar is required.'
if command -v sha256sum >/dev/null 2>&1; then
    hash_file() { sha256sum "$1" | awk '{print $1}'; }
elif command -v shasum >/dev/null 2>&1; then
    hash_file() { shasum -a 256 "$1" | awk '{print $1}'; }
else
    fail 'sha256sum or shasum is required.'
fi
case "$install_dir" in /*) ;; *) fail 'ASM_INSTALL_DIR must be an absolute directory.' ;; esac
while [ "${install_dir%/}" != "$install_dir" ]; do install_dir=${install_dir%/}; done
[ -n "$install_dir" ] || fail 'Choose an installation directory below the filesystem root.'
[ ! -L "$install_dir" ] || fail 'Installation requires a regular directory.'
marker="$install_dir/.asm-install"
target="$install_dir/asm"
assert_target() {
    if [ -e "$marker" ] || [ -L "$marker" ]; then
        [ -f "$marker" ] && [ ! -L "$marker" ] && [ "$(cat "$marker")" = "$repository" ] || fail "Installation marker belongs to another program: $marker"
    fi
    if [ -e "$target" ] || [ -L "$target" ]; then
        [ -f "$target" ] && [ ! -L "$target" ] && [ -f "$marker" ] || fail "An unrelated file already exists: $target"
    fi
}
assert_target
existing=$(command -v asm 2>/dev/null || true)
if [ -n "$existing" ] && [ "$existing" != "$target" ]; then fail "The command asm is already in use: $existing"; fi

work=$(mktemp -d "${TMPDIR:-/tmp}/asm-install.XXXXXXXX")
stage=
lock=
curl_pid=
marker_created=0
committed=0
cleanup() {
    if [ -n "$curl_pid" ]; then kill "$curl_pid" 2>/dev/null || true; wait "$curl_pid" 2>/dev/null || true; fi
    clear_activity
    [ -z "$stage" ] || rm -f -- "$stage"
    if [ "$marker_created" -eq 1 ] && [ "$committed" -eq 0 ]; then rm -f -- "$marker"; fi
    [ -z "$lock" ] || rmdir -- "$lock" 2>/dev/null || true
    case "$work" in "${TMPDIR:-/tmp}"/asm-install.*) rm -rf -- "$work" ;; esac
}
trap cleanup EXIT
trap 'exit 130' INT TERM
fetch() {
    destination=$2 label=$3 download=${4:-0} timeout=${5:-15}
    status "  $label"
    if [ "$interactive" -eq 0 ]; then
        curl -fLsS --connect-timeout 10 --max-time "$timeout" "$1" -o "$destination"
        return
    fi
    curl -fLsS --connect-timeout 10 --max-time "$timeout" -D "$work/headers" "$1" -o "$destination" &
    curl_pid=$! frame=0
    while kill -0 "$curl_pid" 2>/dev/null; do draw_activity; frame=$((frame + 1)); sleep 0.1; done
    result=0
    wait "$curl_pid" || result=$?
    curl_pid=
    clear_activity
    return "$result"
}
if [ "$version" = latest ]; then
    [ -z "$archive_dir" ] || fail 'Specify ASM_VERSION when installing from local archives.'
    fetch https://api.github.com/repos/emptyenemy/asm/releases/latest "$work/release.json" 'Finding the latest asm release' || fail 'No asm release is available. Until the first release, build from source with go build .'
    version=$(sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\(v[0-9][^"]*\)".*/\1/p' "$work/release.json" | head -n 1)
fi
version=${version#v}
printf '%s\n' "$version" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$' || fail 'Expected an asm version such as 1.0.0.'
archive="asm_${version}_${platform}_${arch}.tar.gz"
base="$repository/releases/download/v$version"
if [ -n "$archive_dir" ]; then
    cp "$archive_dir/SHA256SUMS" "$work/SHA256SUMS"
    cp "$archive_dir/$archive" "$work/$archive"
else
    fetch "$base/SHA256SUMS" "$work/SHA256SUMS" 'Reading release checksums'
    fetch "$base/$archive" "$work/$archive" "Downloading asm $version for $platform/$arch" 1 60
fi
expected=$(awk -v name="$archive" '$2 == name {print $1}' "$work/SHA256SUMS")
[ "${#expected}" -eq 64 ] || fail "Release has no SHA-256 for $archive."
status '  Verifying SHA-256'
[ "$(hash_file "$work/$archive")" = "$expected" ] || fail 'Release archive SHA-256 mismatch.'
tar -xOf "$work/$archive" asm > "$work/asm"
chmod 755 "$work/asm"
[ "$("$work/asm" --version)" = "$version" ] || fail 'The downloaded asm executable failed its version check.'
mkdir -p "$install_dir"
lock_path="$install_dir/.asm-install.lock"
mkdir "$lock_path" 2>/dev/null || fail 'Another asm installer is running.'
lock=$lock_path
assert_target
stage=$(mktemp "$install_dir/.asm-install.XXXXXXXX")
cat "$work/asm" > "$stage"
chmod 755 "$stage"
if [ "$platform" = darwin ]; then xattr -d com.apple.quarantine "$stage" 2>/dev/null || true; fi
[ -f "$marker" ] || marker_created=1
printf '%s\n' "$repository" > "$marker"
mv -f -- "$stage" "$target"
stage=
committed=1

if [ "$no_path" -ne 1 ]; then
case ":$PATH:" in
    *":$install_dir:"*) ;;
    *)
        case ${SHELL:-/bin/sh} in
            */zsh) profile="$HOME/.zshrc" ;;
            */bash) if [ "$platform" = darwin ]; then profile="$HOME/.bash_profile"; else profile="$HOME/.bashrc"; fi ;;
            */sh) profile="$HOME/.profile" ;;
            *) profile= ;;
        esac
        if [ -n "$profile" ]; then
            escaped=$(printf '%s' "$install_dir" | sed "s/'/'\\\\''/g")
            line="export PATH='$escaped':\$PATH"
            if ! grep -Fqx "$line" "$profile" 2>/dev/null; then printf '\n%s\n' "$line" >> "$profile"; fi
            status "  PATH configured in $profile"
            status "  Open a new terminal or run: . \"$profile\""
        else
            status "  Add to PATH in the shell configuration: $install_dir"
        fi
        ;;
esac
fi
printf '\n'
highlight "  Installed asm $version"
status "  $target"
printf '\n'
if [ "$no_path" -eq 1 ]; then status '  PATH was not changed.'
else highlight '  Ready: asm --help'; fi
