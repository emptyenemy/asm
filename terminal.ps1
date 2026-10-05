$script:AsmVersion = '1.0.0'
$script:AsmInteractive = -not [Console]::IsOutputRedirected -and -not [Console]::IsErrorRedirected -and $env:TERM -ne 'dumb'
$script:AsmColor = $false
$script:AsmEscape = [char]27
$script:AsmActivity = ''
$script:AsmActivityWidth = 0
$script:AsmFrame = 0
$script:AsmTick = [Diagnostics.Stopwatch]::StartNew()

if ($script:AsmInteractive -and -not (Test-Path Env:NO_COLOR)) {
    try {
        Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;
public static class AsmConsole {
    [DllImport("kernel32.dll")] static extern IntPtr GetStdHandle(int handle);
    [DllImport("kernel32.dll")] static extern bool GetConsoleMode(IntPtr handle, out uint mode);
    [DllImport("kernel32.dll")] static extern bool SetConsoleMode(IntPtr handle, uint mode);
    public static bool EnableColor() {
        foreach (int stream in new int[] { -11, -12 }) {
            uint mode;
            IntPtr handle = GetStdHandle(stream);
            if (!GetConsoleMode(handle, out mode) || !SetConsoleMode(handle, mode | 4)) return false;
        }
        return true;
    }
}
'@
        $script:AsmColor = [AsmConsole]::EnableColor()
    } catch { $script:AsmColor = $false }
}

function Format-AsmText([string]$Text, [ValidateSet('Accent', 'Muted', 'Normal')][string]$Style = 'Normal') {
    if (-not $script:AsmColor -or $Style -eq 'Normal') { return $Text }
    $code = if ($Style -eq 'Accent') { '38;2;129;140;248' } else { '90' }
    "$script:AsmEscape[$($code)m$Text$script:AsmEscape[0m"
}

function Get-AsmWidth {
    if ($script:AsmInteractive) {
        try { if ([Console]::WindowWidth -gt 0) { return [Console]::WindowWidth } } catch { }
    }
    80
}

function Stop-AsmActivity {
    if ($script:AsmActivityWidth) {
        [Console]::Error.Write("`r" + (' ' * $script:AsmActivityWidth) + "`r")
    }
    $script:AsmActivity = ''
    $script:AsmActivityWidth = 0
}

function Write-AsmLine([string]$Text = '', [string]$Style = 'Normal', [switch]$Stderr) {
    Stop-AsmActivity
    $line = Format-AsmText $Text $Style
    if ($Stderr) { [Console]::Error.WriteLine($line) }
    else { [Console]::WriteLine($line) }
}

function Write-AsmWrapped([string]$Text, [int]$Indent = 2, [string]$Style = 'Normal') {
    $width = [Math]::Max(12, (Get-AsmWidth) - $Indent - 1)
    while ($Text.Length -gt $width) {
        $end = $Text.LastIndexOf(' ', $width)
        if ($end -lt 1) { $end = $width }
        Write-AsmLine ((' ' * $Indent) + $Text.Substring(0, $end)) $Style
        $Text = $Text.Substring($end).TrimStart()
    }
    Write-AsmLine ((' ' * $Indent) + $Text) $Style
}

function Write-AsmHeading([string]$Text) {
    if ($script:AsmInteractive) {
        Write-AsmLine
        Write-AsmWrapped $Text 2 'Accent'
        Write-AsmLine
    }
}

function Write-AsmRows($Rows, [string[]]$Headers) {
    $columns = $Headers.Count - 1
    $widths = @(
        for ($i = 0; $i -lt $columns; $i++) {
            $length = $Headers[$i].Length
            foreach ($row in $Rows) { $length = [Math]::Max($length, ([string]$row[$i]).Length) }
            $length
        }
    )
    if (-not $script:AsmInteractive) {
        if ($columns -gt 1) { Write-AsmLine (($Headers | ForEach-Object { $_.PadRight(14) }) -join ' ').TrimEnd() }
        foreach ($row in $Rows) {
            $parts = @(for ($i = 0; $i -lt $columns; $i++) { ([string]$row[$i]).PadRight(14) })
            Write-AsmLine (($parts -join ' ') + ' ' + $row[$columns])
        }
        return
    }
    $prefix = 2 + ($widths | Measure-Object -Sum).Sum + 2 * $columns
    if ((Get-AsmWidth) - $prefix -lt 18) {
        foreach ($row in $Rows) {
            Write-AsmWrapped ($row[0..($columns - 1)] -join ' -> ') 2 'Accent'
            Write-AsmWrapped $row[$columns] 4 'Muted'
            Write-AsmLine
        }
        return
    }
    $header = '  '
    for ($i = 0; $i -lt $columns; $i++) { $header += $Headers[$i].PadRight($widths[$i]) + '  ' }
    Write-AsmLine ($header + $Headers[$columns]) 'Muted'
    foreach ($row in $Rows) {
        $line = '  '
        for ($i = 0; $i -lt $columns; $i++) { $line += (Format-AsmText ([string]$row[$i]).PadRight($widths[$i]) 'Accent') + '  ' }
        $path = [string]$row[$columns]
        $available = (Get-AsmWidth) - $prefix - 1
        while ($path.Length -gt $available) {
            Write-AsmLine ($line + $path.Substring(0, $available))
            $path = $path.Substring($available)
            $line = ' ' * $prefix
        }
        Write-AsmLine ($line + $path)
    }
}

function Start-AsmActivity([string]$Label) {
    Stop-AsmActivity
    $script:AsmActivity = $Label
    $script:AsmFrame = 0
    $script:AsmTick.Restart()
    if (-not $script:AsmInteractive) { [Console]::Error.WriteLine($Label + '...') }
    else { Update-AsmActivity -Force }
}

function Update-AsmActivity([string]$Detail = '', [switch]$Force) {
    if (-not $script:AsmInteractive -or -not $script:AsmActivity) { return }
    if (-not $Force -and $script:AsmTick.ElapsedMilliseconds -lt 100) { return }
    $script:AsmTick.Restart()
    $frames = @('|', '/', '-', '\')
    $marker = $frames[$script:AsmFrame % $frames.Count]
    $script:AsmFrame++
    $text = '  ' + $marker + '  ' + $script:AsmActivity
    if ($Detail) { $text += '  ' + $Detail }
    $limit = [Math]::Max(1, (Get-AsmWidth) - 1)
    if ($text.Length -gt $limit) { $text = $text.Substring(0, $limit) }
    $padding = ' ' * [Math]::Max(0, [Math]::Min($limit, $script:AsmActivityWidth) - $text.Length)
    [Console]::Error.Write("`r" + (Format-AsmText $text 'Accent') + $padding)
    $script:AsmActivityWidth = $text.Length
}

function Format-AsmBytes([double]$Bytes) {
    $units = @('B', 'KB', 'MB', 'GB')
    $unit = 0
    while ($Bytes -ge 1000 -and $unit -lt 3) { $Bytes /= 1000; $unit++ }
    $rounded = [Math]::Round($Bytes, 2)
    $pattern = if ($rounded -eq [Math]::Truncate($rounded)) { '0' } else { '0.00' }
    $rounded.ToString($pattern, [Globalization.CultureInfo]::InvariantCulture) + ' ' + $units[$unit]
}

function Update-AsmDownload([long]$Received, [long]$Total, [double]$Seconds, [switch]$Force) {
    if (-not $script:AsmInteractive) { return }
    if (-not $Force -and $script:AsmTick.ElapsedMilliseconds -lt 100) { return }
    $script:AsmTick.Restart()
    $stats = (Format-AsmBytes $Received)
    if ($Total -gt 0) { $stats += ' / ' + (Format-AsmBytes $Total) }
    if ($Seconds -gt 0) { $stats += '  ' + (Format-AsmBytes ($Received / $Seconds)) + '/s' }
    $width = Get-AsmWidth
    if ($width -lt 60) {
        $detail = if ($Total -gt 0) { '{0,3}%  {1}' -f [Math]::Min(100, [int]($Received * 100.0 / $Total)), (Format-AsmBytes $Received) } else { Format-AsmBytes $Received }
        Update-AsmActivity $detail -Force
        return
    }
    $barWidth = [Math]::Max(8, [Math]::Min(28, $width - $stats.Length - 14))
    if ($Total -gt 0) {
        $fraction = [Math]::Min(1.0, $Received / [double]$Total)
        $filled = [int][Math]::Floor($fraction * $barWidth)
        $bar = '=' * $filled
        if ($filled -lt $barWidth) { $bar += '>'; $filled++ }
        $bar += '.' * ($barWidth - $filled)
        $percent = '{0,3}%' -f [int][Math]::Floor($fraction * 100)
    } else {
        $position = $script:AsmFrame % $barWidth
        $script:AsmFrame++
        $bar = '.' * $position + '>' + '.' * ($barWidth - $position - 1)
        $percent = '  --'
    }
    $text = '  [' + $bar + '] ' + $percent + '  ' + $stats
    $limit = $width - 1
    if ($text.Length -gt $limit) { $text = $text.Substring(0, $limit) }
    [Console]::Error.Write("`r" + (Format-AsmText $text 'Accent') + (' ' * [Math]::Max(0, $script:AsmActivityWidth - $text.Length)))
    $script:AsmActivityWidth = $text.Length
}

function Wait-AsmTask($Task, [Diagnostics.Stopwatch]$Clock, [int]$Timeout, [scriptblock]$Display) {
    while (-not $Task.IsCompleted) {
        if ($Clock.Elapsed.TotalSeconds -ge $Timeout) { throw "Request timed out after $Timeout seconds." }
        if ($Display) { & $Display } else { Update-AsmActivity ('{0}s' -f [Math]::Floor($Clock.Elapsed.TotalSeconds)) }
        [Threading.Thread]::Sleep(80)
    }
    if ($Clock.Elapsed.TotalSeconds -ge $Timeout) { throw "Request timed out after $Timeout seconds." }
    $Task.GetAwaiter().GetResult()
}

function Invoke-AsmRequest([string]$Url, [string]$Label, [string]$Destination = '', [long]$Size = 0, [string]$Method = 'Get', [int]$Timeout = 15) {
    Add-Type -AssemblyName System.Net.Http
    $client = [Net.Http.HttpClient]::new()
    $client.Timeout = [Threading.Timeout]::InfiniteTimeSpan
    $client.DefaultRequestHeaders.UserAgent.ParseAdd('asm/' + $script:AsmVersion)
    $cancel = [Threading.CancellationTokenSource]::new()
    $request = [Net.Http.HttpRequestMessage]::new([Net.Http.HttpMethod]::new($Method), $Url)
    $response = $null
    $inputStream = $null
    $outputStream = $null
    $clock = [Diagnostics.Stopwatch]::StartNew()
    Start-AsmActivity $Label
    try {
        if ($Method -eq 'Post') {
            $request.Content = [Net.Http.StringContent]::new('acceptedLicense=true', [Text.Encoding]::UTF8, 'application/x-www-form-urlencoded')
        }
        $response = Wait-AsmTask ($client.SendAsync($request, [Net.Http.HttpCompletionOption]::ResponseHeadersRead, $cancel.Token)) $clock $Timeout
        $response.EnsureSuccessStatusCode() | Out-Null
        if (-not $Destination) { return Wait-AsmTask ($response.Content.ReadAsStringAsync()) $clock $Timeout }
        $inputStream = Wait-AsmTask ($response.Content.ReadAsStreamAsync()) $clock $Timeout
        $outputStream = [IO.File]::Create($Destination)
        $total = if ($Size -gt 0) { $Size } else { $response.Content.Headers.ContentLength }
        $received = 0L
        $buffer = [byte[]]::new(65536)
        $display = { Update-AsmDownload $received $total $clock.Elapsed.TotalSeconds }
        while ($true) {
            $count = Wait-AsmTask ($inputStream.ReadAsync($buffer, 0, $buffer.Length, $cancel.Token)) $clock $Timeout $display
            if ($count -eq 0) { break }
            $outputStream.Write($buffer, 0, $count)
            $received += $count
            & $display
        }
        Update-AsmDownload $received $total $clock.Elapsed.TotalSeconds -Force
    } finally {
        $cancel.Cancel()
        if ($outputStream) { $outputStream.Dispose() }
        if ($inputStream) { $inputStream.Dispose() }
        if ($response) { $response.Dispose() }
        $request.Dispose()
        $client.Dispose()
        $cancel.Dispose()
        Stop-AsmActivity
    }
}

function Show-AsmHelp([string]$Topic) {
    if (-not $Topic) {
        Write-AsmLine
        if ((Get-AsmWidth) -ge 26) {
            foreach ($line in @('  ____ __________ ___', ' / __ `/ ___/ __ `__ \', '/ /_/ (__  ) / / / / /', '\__,_/____/_/ /_/ /_/')) {
                Write-AsmLine ('  ' + $line) 'Accent'
            }
        } else { Write-AsmLine '  asm' 'Accent' }
        Write-AsmLine
        Write-AsmWrapped ('AIR SDK Manager  ' + $script:AsmVersion) 2 'Muted'
        Write-AsmLine
        Write-AsmWrapped 'Usage: asm <command> [options]' 2
        Write-AsmLine
        $entries = @(
            @('list, ls', 'List installed SDKs and their paths.'),
            @('search [VERSION]', 'Find available stable releases.'),
            @('install VERSION', 'Install a branch, exact build, or latest.'),
            @('update [VERSION]', 'Check updates; a version applies them.'),
            @('help [COMMAND]', 'Show general or command help.'),
            @('--version, -v', 'Print the asm version.')
        )
        foreach ($entry in $entries) {
            if ((Get-AsmWidth) -ge 70) {
                Write-AsmLine ('  ' + (Format-AsmText $entry[0].PadRight(20) 'Accent') + $entry[1])
            } else {
                Write-AsmWrapped $entry[0] 2 'Accent'
                Write-AsmWrapped $entry[1] 4 'Muted'
            }
        }
        Write-AsmLine
        Write-AsmWrapped 'Start: asm search 51.4' 2 'Accent'
        Write-AsmWrapped 'Help:  asm <command> --help' 2 'Muted'
        Write-AsmLine
        return
    }
    $usage = switch ($Topic) {
        'list' { 'asm list' }
        'search' { 'asm search [VERSION]' }
        'install' { 'asm install VERSION [--accept-license]' }
        'update' { 'asm update [VERSION] [--all] [--check] [--accept-license]' }
    }
    Write-AsmLine
    Write-AsmWrapped ('Usage: ' + $usage) 2 'Accent'
    Write-AsmLine
    $lines = switch ($Topic) {
        'list' { @('Alias: asm ls', 'Reads AIR_SDKS from ~/.airsdk/airsdkmanager.cfg.', 'Lists SDK versions and paths in the configured directory.') }
        'search' { @('VERSION is optional: a branch such as 51.4 or an exact build.', 'Lists announced stable releases, newest first.', 'Falls back to the manager catalog when the source is unavailable.') }
        'install' { @('VERSION: a branch such as 51.4, an exact build, or latest.', 'Installs into AIR_SDKS; an installed build is kept.', '--accept-license  Accept the AIR SDK license for this operation.') }
        'update' { @('No arguments      Show installed updates and a newer SDK branch.', 'VERSION           Update matching installed SDKs, such as 51.3.', '--all             Update all installed SDKs with a newer build.', '--check           Preview only, including with VERSION or --all.', '--accept-license  Accept the AIR SDK license for this operation.', 'Updates preserve each SDK path and three-component version.', 'Install new branches separately with asm install.') }
    }
    foreach ($line in $lines) { Write-AsmWrapped $line 2 }
    Write-AsmLine
}
