param(
    [string]$Version = 'latest',
    [string]$InstallDirectory = '',
    [string]$ArchiveDirectory = '',
    [switch]$NoPath
)

& {
    param([string]$Version, [string]$InstallDirectory, [string]$ArchiveDirectory, [bool]$NoPath)
    $ErrorActionPreference = 'Stop'
    $ProgressPreference = 'SilentlyContinue'
    if ([Environment]::OSVersion.Platform -ne [PlatformID]::Win32NT) { throw 'Use install.sh on macOS or Linux.' }
    if (-not [Environment]::Is64BitOperatingSystem -or $env:PROCESSOR_ARCHITECTURE -eq 'ARM64' -or $env:PROCESSOR_ARCHITEW6432 -eq 'ARM64') {
        throw 'The Windows build currently supports x64. No native Windows ARM64 SDK build is available.'
    }
    [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
    $interactive = -not [Console]::IsOutputRedirected -and -not [Console]::IsErrorRedirected -and $env:TERM -ne 'dumb'
    $color = $interactive -and -not (Test-Path Env:NO_COLOR)
    $escape = [string][char]27
    if ($color) {
        if (-not ('AsmBootstrapConsole' -as [type])) {
            Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;
public static class AsmBootstrapConsole {
    [DllImport("kernel32.dll")] static extern IntPtr GetStdHandle(int handle);
    [DllImport("kernel32.dll")] static extern bool GetConsoleMode(IntPtr handle, out uint mode);
    [DllImport("kernel32.dll")] static extern bool SetConsoleMode(IntPtr handle, uint mode);
    public static bool Enable() {
        foreach (int id in new[] {-11, -12}) {
            uint mode; IntPtr handle = GetStdHandle(id);
            if (!GetConsoleMode(handle, out mode) || !SetConsoleMode(handle, mode | 4)) return false;
        }
        return true;
    }
}
'@
        }
        $color = [AsmBootstrapConsole]::Enable()
    }
    function Write-InstallerLine([string]$Text = '', [string]$Style = '') {
        if ($color -and $Style) {
            $code = if ($Style -eq 'muted') { '90' } else { '38;2;129;140;248' }
            [Console]::WriteLine("$escape[${code}m$Text$escape[0m")
        } else { [Console]::WriteLine($Text) }
    }
    function Get-InstallerWidth {
        try { if ($interactive -and [Console]::WindowWidth -gt 0) { return [Console]::WindowWidth } } catch {}
        return 80
    }
    function Format-InstallerBytes([double]$Bytes) {
        $unit = 0
        while ($Bytes -ge 1000 -and $unit -lt 3) { $Bytes /= 1000; $unit++ }
        $Bytes = [Math]::Round($Bytes, 2)
        $format = if ($Bytes -eq [Math]::Truncate($Bytes)) { '0' } else { '0.00' }
        $Bytes.ToString($format, [Globalization.CultureInfo]::InvariantCulture) + ' ' + @('B', 'KB', 'MB', 'GB')[$unit]
    }
    function Write-InstallerActivity([string]$Label, [int]$Frame, [double]$Seconds, [long]$Received = -1, [long]$Total = 0) {
        if (-not $interactive) { return }
        $width = [Math]::Max(1, (Get-InstallerWidth) - 1)
        $text = '  ' + '|/-\'[$Frame % 4] + "  $Label  " + [int]$Seconds + 's'
        if ($Received -ge 0) {
            $stats = Format-InstallerBytes $Received
            if ($Total -gt 0) { $stats += ' / ' + (Format-InstallerBytes $Total) }
            if ($Seconds -gt 0) { $stats += '  ' + (Format-InstallerBytes ($Received / $Seconds)) + '/s' }
            $percent = if ($Total -gt 0) { '{0,3}%' -f [Math]::Min(100, [int]($Received * 100.0 / $Total)) } else { '  --' }
            if ($width -ge 59) {
                $barWidth = [Math]::Max(8, [Math]::Min(28, $width - $stats.Length - 13))
                if ($Total -gt 0) {
                    $filled = [Math]::Min($barWidth, [int][Math]::Floor($Received * 1.0 / $Total * $barWidth))
                    $bar = '=' * $filled
                    if ($filled -lt $barWidth) { $bar += '>'; $filled++ }
                    $bar += '.' * ($barWidth - $filled)
                } else {
                    $position = $Frame % $barWidth
                    $bar = '.' * $position + '>' + '.' * ($barWidth - $position - 1)
                }
                $text = "  [$bar] $percent  $stats"
            } else { $text = "  $percent  " + (Format-InstallerBytes $Received) }
        }
        if ($text.Length -gt $width) { $text = $text.Substring(0, $width) }
        $padding = ' ' * ($width - $text.Length)
        if ($color) { $text = "$escape[38;2;129;140;248m$text$escape[0m" }
        [Console]::Error.Write("`r$text$padding")
    }
    function Clear-InstallerActivity {
        if ($interactive) { [Console]::Error.Write("`r" + (' ' * [Math]::Max(1, (Get-InstallerWidth) - 1)) + "`r") }
    }
    function Get-InstallerDownload([string]$Uri, [string]$Label, [string]$Destination = '', [int]$Timeout = 15) {
        Write-InstallerLine "  $Label" 'muted'
        Add-Type -AssemblyName System.Net.Http
        $client = [Net.Http.HttpClient]::new()
        $client.DefaultRequestHeaders.UserAgent.ParseAdd('asm-installer')
        $cancel = [Threading.CancellationTokenSource]::new([TimeSpan]::FromSeconds($Timeout))
        $clock = [Diagnostics.Stopwatch]::StartNew()
        $response = $null; $inputStream = $null; $outputStream = $null
        $frame = 0; $received = 0L; $nextDraw = 0L
        try {
            $task = $client.GetAsync($Uri, [Net.Http.HttpCompletionOption]::ResponseHeadersRead, $cancel.Token)
            while (-not $task.IsCompleted) {
                Write-InstallerActivity $Label $frame $clock.Elapsed.TotalSeconds
                $frame++
                Start-Sleep -Milliseconds 100
            }
            $response = $task.GetAwaiter().GetResult()
            $response.EnsureSuccessStatusCode() | Out-Null
            $inputStream = $response.Content.ReadAsStreamAsync().GetAwaiter().GetResult()
            $total = [long]$response.Content.Headers.ContentLength
            $outputStream = if ($Destination) { [IO.File]::Open($Destination, [IO.FileMode]::CreateNew) } else { [IO.MemoryStream]::new() }
            $buffer = [byte[]]::new(65536)
            do {
                $task = $inputStream.ReadAsync($buffer, 0, $buffer.Length, $cancel.Token)
                while (-not $task.IsCompleted) {
                    Write-InstallerActivity $Label $frame $clock.Elapsed.TotalSeconds $received $total
                    $frame++
                    Start-Sleep -Milliseconds 100
                }
                $count = $task.GetAwaiter().GetResult()
                if ($count) { $outputStream.Write($buffer, 0, $count); $received += $count }
                if ($clock.ElapsedMilliseconds -ge $nextDraw) {
                    Write-InstallerActivity $Label $frame $clock.Elapsed.TotalSeconds $received $total
                    $frame++; $nextDraw = $clock.ElapsedMilliseconds + 100
                }
            } while ($count)
            if (-not $Destination) { return [Text.Encoding]::UTF8.GetString($outputStream.ToArray()) }
        } finally {
            Clear-InstallerActivity
            if ($outputStream) { $outputStream.Dispose() }
            if ($inputStream) { $inputStream.Dispose() }
            if ($response) { $response.Dispose() }
            $cancel.Dispose(); $client.Dispose()
        }
    }
    if ($interactive) {
        Write-InstallerLine
        if ((Get-InstallerWidth) -ge 26) {
            foreach ($line in @('  ____ __________ ___', ' / __ `/ ___/ __ `__ \', '/ /_/ (__  ) / / / / /', '\__,_/____/_/ /_/ /_/')) { Write-InstallerLine ('  ' + $line) 'accent' }
        } else { Write-InstallerLine '  asm' 'accent' }
        Write-InstallerLine
        Write-InstallerLine '  AIR SDK Manager installer' 'muted'
        Write-InstallerLine
    }
    $repository = 'https://github.com/emptyenemy/asm'
    if (-not $InstallDirectory) { $InstallDirectory = Join-Path ([Environment]::GetFolderPath('LocalApplicationData')) 'Programs\asm' }
    $InstallDirectory = [IO.Path]::GetFullPath($InstallDirectory)
    if ($InstallDirectory -eq [IO.Path]::GetPathRoot($InstallDirectory)) { throw 'Choose an installation directory below a drive root.' }
    $InstallDirectory = $InstallDirectory.TrimEnd('\', '/')
    $parent = [IO.Directory]::GetParent($InstallDirectory)
    if (-not $parent) { throw 'Choose an installation directory below a drive root.' }
    $parent = $parent.FullName
    $launcher = Join-Path $InstallDirectory 'asm.exe'
    $marker = '.asm-install.json'

    function Test-SamePath([string]$Left, [string]$Right) {
        if (-not $Left -or -not $Right) { return $false }
        try {
            $a = [IO.Path]::GetFullPath([Environment]::ExpandEnvironmentVariables($Left.Trim('"'))).TrimEnd('\', '/')
            $b = [IO.Path]::GetFullPath($Right).TrimEnd('\', '/')
            [string]::Equals($a, $b, [StringComparison]::OrdinalIgnoreCase)
        } catch { $false }
    }

    function Assert-InstallDirectory {
        if ([IO.File]::Exists($InstallDirectory)) { throw "Installation path is a file: $InstallDirectory" }
        if (-not [IO.Directory]::Exists($InstallDirectory)) { return }
        if ((Get-Item -LiteralPath $InstallDirectory -Force).Attributes -band [IO.FileAttributes]::ReparsePoint) { throw "Installation requires a regular directory: $InstallDirectory" }
        $items = @(Get-ChildItem -LiteralPath $InstallDirectory -Force)
        if (-not $items.Count) { return }
        $file = Join-Path $InstallDirectory $marker
        if (-not [IO.File]::Exists($file)) { throw "Directory is not an asm installation: $InstallDirectory" }
        if (([IO.File]::ReadAllText($file) | ConvertFrom-Json).repository -ne $repository) { throw "Directory belongs to another program: $InstallDirectory" }
        foreach ($item in $items) {
            if ($item.PSIsContainer -or $item.Name -notin @('asm.exe', $marker) -or ($item.Attributes -band [IO.FileAttributes]::ReparsePoint)) {
                throw "Installation contains an unexpected item; keep it outside this directory: $($item.FullName)"
            }
        }
    }

    function Remove-InstallerDirectory([string]$Path) {
        $absolute = [IO.Path]::GetFullPath($Path)
        $prefix = $parent.TrimEnd('\', '/') + [IO.Path]::DirectorySeparatorChar
        $name = [IO.Path]::GetFileName($absolute)
        if (-not $absolute.StartsWith($prefix, [StringComparison]::OrdinalIgnoreCase) -or
            (-not (Test-SamePath $absolute $InstallDirectory) -and $name -notmatch '^\.asm-(install|old)-[a-f0-9]{32}$')) { throw "Unsafe installation cleanup path: $Path" }
        if ([IO.Directory]::Exists($absolute)) { Remove-Item -LiteralPath $absolute -Recurse -Force }
    }

    Assert-InstallDirectory
    foreach ($command in @(Get-Command asm -All -ErrorAction SilentlyContinue)) {
        if (-not (Test-SamePath $command.Path $launcher)) { throw "The command 'asm' is already in use: $($command.Definition). Remove that conflict before installing." }
    }
    foreach ($scope in @('User', 'Machine')) {
        foreach ($entry in ([Environment]::GetEnvironmentVariable('Path', $scope) -split ';')) {
            if (-not $entry) { continue }
            $directory = [Environment]::ExpandEnvironmentVariables($entry.Trim('"'))
            foreach ($extension in @('.exe', '.com', '.bat', '.cmd', '.ps1')) {
                $candidate = Join-Path $directory ('asm' + $extension)
                if ([IO.File]::Exists($candidate) -and -not (Test-SamePath $candidate $launcher)) { throw "The command 'asm' is already in use: $candidate. Remove that conflict before installing." }
            }
        }
    }
    if ($Version -eq 'latest') {
        if ($ArchiveDirectory) { throw 'Specify -Version when installing from local archives.' }
        try { $Version = (Get-InstallerDownload 'https://api.github.com/repos/emptyenemy/asm/releases/latest' 'Finding the latest asm release' | ConvertFrom-Json).tag_name }
        catch { throw 'Cannot find an asm release. Until the first release is published, build from source with go build .' }
    }
    $Version = $Version -replace '^v', ''
    if ($Version -notmatch '^\d+\.\d+\.\d+$') { throw 'Expected an asm version such as 1.0.0.' }
    $archiveName = "asm_${Version}_windows_amd64.zip"
    [IO.Directory]::CreateDirectory($parent) | Out-Null
    $stage = Join-Path $parent ('.asm-install-' + [guid]::NewGuid().ToString('N'))
    $previous = Join-Path $parent ('.asm-old-' + [guid]::NewGuid().ToString('N'))
    $lock = $null
    $moved = $false
    $committed = $false
    $pathChanged = $false
    $oldUserPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    $oldProcessPath = $env:PATH
    try {
        $lock = [IO.FileStream]::new((Join-Path $parent ('.' + [IO.Path]::GetFileName($InstallDirectory) + '.asm-install.lock')),
            [IO.FileMode]::OpenOrCreate, [IO.FileAccess]::ReadWrite, [IO.FileShare]::None, 4096, [IO.FileOptions]::DeleteOnClose)
        [IO.Directory]::CreateDirectory($stage) | Out-Null
        if ($ArchiveDirectory) {
            $sums = [IO.File]::ReadAllText((Join-Path $ArchiveDirectory 'SHA256SUMS'))
            [IO.File]::Copy((Join-Path $ArchiveDirectory $archiveName), (Join-Path $stage $archiveName))
        } else {
            $base = "$repository/releases/download/v$Version/"
            $sums = Get-InstallerDownload ($base + 'SHA256SUMS') 'Reading release checksums'
            Get-InstallerDownload ($base + $archiveName) "Downloading asm $Version for Windows x64" (Join-Path $stage $archiveName) 60
        }
        $pattern = '(?im)^([a-f0-9]{64})\s+\*?' + [regex]::Escape($archiveName) + '\s*$'
        $match = [regex]::Match($sums, $pattern)
        if (-not $match.Success) { throw "Release has no SHA-256 for $archiveName." }
        Write-InstallerLine '  Verifying SHA-256' 'muted'
        $stream = [IO.File]::OpenRead((Join-Path $stage $archiveName))
        $algorithm = [Security.Cryptography.SHA256]::Create()
        try { $hash = [BitConverter]::ToString($algorithm.ComputeHash($stream)).Replace('-', '').ToLowerInvariant() }
        finally { $stream.Dispose(); $algorithm.Dispose() }
        if ($hash -ne $match.Groups[1].Value.ToLowerInvariant()) { throw 'Release archive SHA-256 mismatch.' }
        Add-Type -AssemblyName System.IO.Compression.FileSystem
        $archive = [IO.Compression.ZipFile]::OpenRead((Join-Path $stage $archiveName))
        try {
            $entry = @($archive.Entries | Where-Object { $_.FullName -eq 'asm.exe' -or $_.FullName -eq "asm_${Version}_windows_amd64/asm.exe" })
            if ($entry.Count -ne 1) { throw 'Release archive must contain exactly one asm.exe.' }
            [IO.Compression.ZipFileExtensions]::ExtractToFile($entry[0], (Join-Path $stage 'asm.exe'))
        } finally { $archive.Dispose() }
        [IO.File]::Delete((Join-Path $stage $archiveName))
        $result = & (Join-Path $stage 'asm.exe') --version
        if ($LASTEXITCODE -ne 0 -or $result.Trim() -ne $Version) { throw 'The downloaded asm executable failed its version check.' }
        [IO.File]::WriteAllText((Join-Path $stage $marker), (@{ repository = $repository; version = $Version } | ConvertTo-Json), [Text.UTF8Encoding]::new($false))
        Assert-InstallDirectory
        if ([IO.Directory]::Exists($InstallDirectory)) { [IO.Directory]::Move($InstallDirectory, $previous) }
        [IO.Directory]::Move($stage, $InstallDirectory)
        $moved = $true
        if (-not $NoPath) {
            $present = @($oldUserPath -split ';' | Where-Object { Test-SamePath $_ $InstallDirectory })
            if (-not $present.Count) {
                $newPath = if ($oldUserPath) { $oldUserPath.TrimEnd(';') + ';' + $InstallDirectory } else { $InstallDirectory }
                $pathChanged = $true
                [Environment]::SetEnvironmentVariable('Path', $newPath, 'User')
            }
            $processPresent = @($oldProcessPath -split ';' | Where-Object { Test-SamePath $_ $InstallDirectory })
            if (-not $processPresent.Count) { $env:PATH = $InstallDirectory + ';' + $oldProcessPath }
        }
        $committed = $true
        Remove-InstallerDirectory $previous
        Write-InstallerLine
        Write-InstallerLine "  Installed asm $Version" 'accent'
        Write-InstallerLine "  $InstallDirectory" 'muted'
        Write-InstallerLine
        if ($NoPath) { Write-InstallerLine '  PATH was not changed.' 'muted' }
        else { Write-InstallerLine '  Ready: asm --help' 'accent'; Write-InstallerLine '  Reopen other terminals to pick up PATH.' 'muted' }
    } catch {
        if (-not $committed) {
            if ($pathChanged) { [Environment]::SetEnvironmentVariable('Path', $oldUserPath, 'User') }
            $env:PATH = $oldProcessPath
            if ($moved) { Remove-InstallerDirectory $InstallDirectory }
            if ([IO.Directory]::Exists($previous)) {
                try { [IO.Directory]::Move($previous, $InstallDirectory) }
                catch { throw "Cannot restore asm. The previous installation remains at $previous. $($_.Exception.Message)" }
            }
        }
        throw
    } finally {
        try { Remove-InstallerDirectory $stage }
        finally { if ($lock) { $lock.Dispose() } }
    }
} $Version $InstallDirectory $ArchiveDirectory ([bool]$NoPath)
