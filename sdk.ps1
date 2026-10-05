param([ValidateSet('list', 'search', 'update', 'install', 'help', 'version')][string]$Command = 'list', [string]$Topic = '')

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
[Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
. (Join-Path $PSScriptRoot 'terminal.ps1')
$configFile = Join-Path $env:USERPROFILE '.airsdk\airsdkmanager.cfg'

function Get-ManagerSetting([string]$Name) {
    if (-not [IO.File]::Exists($configFile)) { return }
    $value = $null
    $pattern = '^\s*' + [regex]::Escape($Name) + '\s*=(.*)$'
    foreach ($line in [IO.File]::ReadAllLines($configFile, [Text.Encoding]::UTF8)) {
        if ($line -match $pattern) { $value = $Matches[1].Trim().Trim('"') }
    }
    $value
}

function Read-Sdk([IO.DirectoryInfo]$Directory) {
    $descriptionFile = Join-Path $Directory.FullName 'air-sdk-description.xml'
    if (-not [IO.File]::Exists($descriptionFile)) { return }

    try {
        $document = [Xml.XmlDocument]::new()
        $document.XmlResolver = $null
        $document.Load($descriptionFile)
        if ($document.DocumentElement.LocalName -ne 'air-sdk-description') {
            throw 'Expected air-sdk-description.'
        }
        $versionNode = $document.DocumentElement.SelectSingleNode('*[local-name()="version"]')
        $buildNode = $document.DocumentElement.SelectSingleNode('*[local-name()="build"]')
        $number = $versionNode.InnerText.Trim()
        if ($number -notmatch '^\d+\.\d+\.\d+(\.\d+)?$') { throw 'Invalid SDK version.' }
        if ($number.Split('.').Count -eq 3 -and $buildNode) {
            $build = $buildNode.InnerText.Trim()
            if ($build -notmatch '^\d+$') { throw 'Invalid SDK build.' }
            $number += ".$build"
        }
        [pscustomobject]@{ Version = [version]$number; Path = $Directory.FullName }
    } catch {
        Write-AsmLine ("Warning: Cannot read SDK description in {0}: {1}" -f $Directory.FullName, $_.Exception.Message) -Stderr
    }
}

function Get-SdkDirectory {
    if (-not [IO.File]::Exists($configFile)) {
        throw "AIR SDK Manager settings not found: $configFile"
    }
    $sdkDirectory = Get-ManagerSetting 'AIR_SDKS'
    if (-not $sdkDirectory) { throw "AIR_SDKS is not set in $configFile" }
    [IO.Path]::GetFullPath($sdkDirectory)
}

function Get-InstalledSdks {
    $sdkDirectory = Get-SdkDirectory
    if (-not [IO.Directory]::Exists($sdkDirectory)) {
        throw "SDK directory does not exist: $sdkDirectory"
    }
    $sdks = @(
        Get-ChildItem -LiteralPath $sdkDirectory -Directory |
            Where-Object { $_.Name -notlike '.asm-*' } |
            ForEach-Object { Read-Sdk $_ } |
            Sort-Object Version -Descending
    )
    $sdks
}

function Show-InstalledSdks {
    $sdks = @(Get-InstalledSdks)
    Write-AsmHeading 'Installed AIR SDKs'
    if ($sdks.Count -eq 0) { Write-AsmLine 'No local AIR SDK versions found.' }
    else {
        $rows = @(foreach ($sdk in $sdks) { ,@($sdk.Version.ToString(), $sdk.Path) })
        Write-AsmRows $rows @('Version', 'Path')
    }
}

function Convert-Releases($Releases) {
    foreach ($release in $Releases) {
        if ($release.type -and $release.type -ne 'production') { continue }
        if ($release.name -notmatch '^\d+\.\d+\.\d+\.\d+$') {
            throw 'Invalid version in AIR SDK catalog.'
        }
        [version]$release.name
    }
}

function Get-CachedCatalog {
    $managerDirectory = Split-Path $configFile -Parent
    $files = @(
        Get-Item -LiteralPath (Join-Path $managerDirectory 'airsdkmanager.db') -ErrorAction SilentlyContinue
        Get-ChildItem -LiteralPath $managerDirectory -Filter 'airsdkmanager.db.backup*' -File -ErrorAction SilentlyContinue |
            Sort-Object LastWriteTimeUtc -Descending
    )
    foreach ($file in $files) {
        try {
            $catalog = [IO.File]::ReadAllText($file.FullName, [Text.Encoding]::UTF8) | ConvertFrom-Json
            $releases = @(
                foreach ($field in @('availableSDKs', 'latestSDKs', 'installableSDKs')) {
                    foreach ($sdk in $catalog.$field) {
                        if ($sdk.build.name) { $sdk.build }
                    }
                }
            )
            $versions = @(Convert-Releases $releases)
            if ($versions.Count) {
                return [pscustomobject]@{ Versions = $versions; File = $file.FullName }
            }
        } catch { continue }
    }
}

function Get-NewsVersions {
    $html = Invoke-AsmRequest 'https://airsdk.dev/news/archive' 'Checking AIR SDK releases'
    $previews = @('51.0.0.2', '51.0.0.4')
    $pattern = '(?is)<a\b[^>]*\bhref="/news/\d{4}/\d{2}/\d{2}/[^"\s]+"[^>]*>(?<title>.*?)</a>'
    $versions = @(
        foreach ($link in [regex]::Matches($html, $pattern)) {
            $title = [Net.WebUtility]::HtmlDecode([regex]::Replace($link.Groups['title'].Value, '<[^>]*>', ''))
            if ($title -notmatch '\bRelease\s+(\d+\.\d+\.\d+\.\d+)\b') { continue }
            $number = $Matches[1]
            if ($title -match '\b(beta|alpha|preview|pre[ -]?release)\b' -or $number -in $previews) { continue }
            [version]$number
        }
    )
    if (-not $versions.Count) { throw 'The AIR SDK announcement archive contains no recognized releases.' }
    $versions | Sort-Object -Unique -Descending
}

function Get-ReleaseVersions {
    $endpoint = Get-ManagerSetting 'API_ENDPOINT'
    try {
        if ($endpoint) {
            $uri = $endpoint.TrimEnd('/') + '/releases?types=production'
            $response = (Invoke-AsmRequest $uri 'Checking AIR SDK releases') | ConvertFrom-Json
            if ($response.errorType) { throw "AIR SDK API returned $($response.errorType)." }
            if ($response.releases -isnot [array]) { throw 'AIR SDK API response has no releases array.' }
            $versions = @(Convert-Releases $response.releases)
        } else { $versions = @(Get-NewsVersions) }
    } catch {
        $apiError = $_.Exception.Message
        $cache = Get-CachedCatalog
        if (-not $cache) { throw "Cannot load AIR SDK catalog: $apiError" }
        Write-AsmLine ("Warning: {0} Using cached AIR SDK Manager catalog: {1}" -f $apiError, $cache.File) -Stderr
        $versions = $cache.Versions
    }
    $versions
}

function Search-Sdks {
    $filter = $env:ASM_SEARCH_VERSION
    if ($filter) {
        if ($filter -notmatch '^\d+(\.\d+){0,3}$') { throw 'Expected a version such as 51.4 or 51.4.1.1.' }
        $filter = ($filter.Split('.') | ForEach-Object { ([int]$_).ToString() }) -join '.'
    }
    $versions = @(Get-ReleaseVersions)
    $matches = @(
        $versions | Where-Object {
            -not $filter -or $_.ToString() -eq $filter -or $_.ToString().StartsWith($filter + '.')
        } | Sort-Object -Unique -Descending
    )
    Write-AsmHeading 'Available AIR SDKs'
    if ($matches.Count -eq 0) { Write-AsmLine 'No matching AIR SDK versions found.' }
    else {
        foreach ($version in $matches) {
            $prefix = if ($script:AsmInteractive) { '  ' } else { '' }
            Write-AsmLine ($prefix + $version) 'Accent'
        }
        if ($script:AsmInteractive) {
            Write-AsmLine
            Write-AsmWrapped ('Install: asm install ' + $matches[0]) 2 'Muted'
        }
    }
}


function Get-WindowsPackage([version]$Version) {
    $catalog = (Invoke-AsmRequest 'https://shockpkg.github.io/packages/api/1/packages.json' 'Finding the Windows SDK download') | ConvertFrom-Json
    if ($catalog.packages -isnot [array]) { throw 'Invalid shockpkg SDK catalog.' }
    $package = $catalog.packages | Where-Object { $_.name -eq "air-sdk-$Version-windows-compiler" } | Select-Object -First 1
    if (-not $package) { throw "No Windows SDK download found for $Version." }
    Write-AsmLine 'Using the shockpkg download mirror with SHA-256 verification.' 'Muted' -Stderr
    [pscustomobject]@{
        name = $Version.ToString()
        type = 'production'
        urls = [pscustomobject]@{ AIR_Win = [pscustomobject]@{ url = $package.source; checksum = $package.sha256; fileSize = $package.size } }
    }
}

function Get-SdkManifest([version]$Version) {
    $endpoint = Get-ManagerSetting 'API_ENDPOINT'
    if (-not $endpoint) { return Get-WindowsPackage $Version }
    $managerDirectory = Split-Path $configFile -Parent
    $files = @(
        Get-Item -LiteralPath (Join-Path $managerDirectory 'airsdkmanager.db') -ErrorAction SilentlyContinue
        Get-ChildItem -LiteralPath $managerDirectory -Filter 'airsdkmanager.db.backup*' -File -ErrorAction SilentlyContinue |
            Sort-Object LastWriteTimeUtc -Descending
    )
    foreach ($file in $files) {
        try {
            $catalog = [IO.File]::ReadAllText($file.FullName, [Text.Encoding]::UTF8) | ConvertFrom-Json
            foreach ($field in @('latestSDKs', 'installableSDKs', 'availableSDKs')) {
                foreach ($sdk in $catalog.$field) {
                    $build = $sdk.build
                    if ($build.name -eq $Version.ToString() -and $build.type -eq 'production' -and
                        (($build.components -and @($build.components.PSObject.Properties).Count) -or $build.urls.AIR_Win.checksum)) {
                        return $build
                    }
                }
            }
        } catch { continue }
    }
    $endpoint = Get-ManagerSetting 'API_ENDPOINT'
    if ($endpoint) {
        $manifest = (Invoke-AsmRequest ($endpoint.TrimEnd('/') + '/releases/' + $Version + '?types=production') 'Loading the SDK manifest') | ConvertFrom-Json
        if ($manifest.name -ne $Version.ToString() -or $manifest.type -ne 'production') {
            throw "Invalid manifest for AIR SDK $Version."
        }
        return $manifest
    }
}

function Get-ArchiveHash([string]$File) {
    $stream = [IO.File]::OpenRead($File)
    $algorithm = [Security.Cryptography.SHA256]::Create()
    Start-AsmActivity 'Verifying SHA-256'
    try {
        $buffer = [byte[]]::new(1048576)
        while (($count = $stream.Read($buffer, 0, $buffer.Length)) -gt 0) {
            $algorithm.TransformBlock($buffer, 0, $count, $buffer, 0) | Out-Null
            Update-AsmActivity
        }
        $algorithm.TransformFinalBlock([byte[]]::new(0), 0, 0) | Out-Null
        [BitConverter]::ToString($algorithm.Hash).Replace('-', '').ToLowerInvariant()
    } finally { $stream.Dispose(); $algorithm.Dispose(); Stop-AsmActivity }
}

function Get-SdkArchive([string]$Url, [string]$Checksum, [long]$Size, [string]$Name, [string]$Destination, [string]$Method = 'Get') {
    $Checksum = $Checksum.Trim()
    if ($Checksum -notmatch '^[a-fA-F0-9]{64}$') { throw "Invalid SHA-256 for $Name." }
    $file = Get-ChildPath (Join-Path $Destination ('.asm-download-' + [guid]::NewGuid().ToString('N') + '.zip')) $Destination
    $temporary = $file + '.download'
    try {
        if ($script:AsmInteractive) { Write-AsmLine ("Downloading $Name") 'Accent' -Stderr }
        Invoke-AsmRequest $Url "Downloading $Name" $temporary $Size $Method 600
        if ($Size -gt 0 -and (Get-Item -LiteralPath $temporary).Length -ne $Size) { throw "Download size mismatch for $Name." }
        if ((Get-ArchiveHash $temporary) -ne $Checksum) { throw "SHA-256 mismatch for $Name." }
        [IO.File]::Move($temporary, $file)
        $file
    } finally {
        if ([IO.File]::Exists($temporary)) { Remove-Item -LiteralPath $temporary -Force }
    }
}

function Get-ChildPath([string]$Path, [string]$Root) {
    $absolute = [IO.Path]::GetFullPath($Path)
    $prefix = [IO.Path]::GetFullPath($Root).TrimEnd('\', '/') + [IO.Path]::DirectorySeparatorChar
    if (-not $absolute.StartsWith($prefix, [StringComparison]::OrdinalIgnoreCase)) {
        throw "Path is outside the SDK directory: $Path"
    }
    $absolute
}

function Expand-SdkArchive([string]$File, [string]$Destination) {
    $archive = $null
    Start-AsmActivity 'Extracting the SDK'
    try {
        Add-Type -AssemblyName System.IO.Compression.FileSystem
        $archive = [IO.Compression.ZipFile]::OpenRead($File)
        $buffer = [byte[]]::new(1048576)
        foreach ($entry in $archive.Entries) {
            Update-AsmActivity
            if ($entry.FullName -match ':|^[/\\]') { throw "Invalid ZIP entry: $($entry.FullName)" }
            $target = Get-ChildPath (Join-Path $Destination $entry.FullName) $Destination
            if ($entry.FullName.EndsWith('/') -or $entry.FullName.EndsWith('\')) {
                [IO.Directory]::CreateDirectory($target) | Out-Null
            } else {
                [IO.Directory]::CreateDirectory([IO.Path]::GetDirectoryName($target)) | Out-Null
                $source = $entry.Open()
                $destinationStream = $null
                try {
                    $destinationStream = [IO.File]::Create($target)
                    while (($count = $source.Read($buffer, 0, $buffer.Length)) -gt 0) {
                        $destinationStream.Write($buffer, 0, $count)
                        Update-AsmActivity
                    }
                } finally {
                    if ($destinationStream) { $destinationStream.Dispose() }
                    $source.Dispose()
                }
            }
        }
    } finally {
        if ($archive) { $archive.Dispose() }
        if ([IO.File]::Exists($File)) { Remove-Item -LiteralPath $File -Force }
        Stop-AsmActivity
    }
}

function Build-Sdk([version]$Version, [string]$Destination) {
    $manifest = Get-SdkManifest $Version
    $components = @($manifest.components.PSObject.Properties | Where-Object { $_ -and $_.Name -notin @('linux', 'macos') })
    $endpoint = Get-ManagerSetting 'API_ENDPOINT'
    if (-not $endpoint) { $endpoint = 'https://api.airsdk.harman.com' }
    if ($components.Count) {
        foreach ($component in $components) {
            $details = $component.Value
            if (-not $details.version) { throw "Missing component version: $($component.Name)" }
            $url = $endpoint.TrimEnd('/') + '/releases/components/' + [uri]::EscapeDataString($component.Name) + '/' + [uri]::EscapeDataString($details.version)
            $archive = Get-SdkArchive $url $details.checksum $details.fileSize $component.Name $Destination 'Post'
            Expand-SdkArchive $archive $Destination
        }
    } else {
        $details = $manifest.urls.AIR_Win
        if (-not $details.url) { throw "No Windows archive in manifest for $Version." }
        $url = $details.url
        if ($url.StartsWith('/')) { $url = 'https://airsdk.harman.com' + $url }
        if ($url.StartsWith('https://airsdk.harman.com/')) {
            $separator = if ($url.Contains('?')) { '&' } else { '?' }
            $url += $separator + 'license=accepted'
        }
        $archive = Get-SdkArchive $url $details.checksum $details.fileSize "AIR SDK $Version" $Destination
        Expand-SdkArchive $archive $Destination
    }
    if (-not [IO.File]::Exists((Join-Path $Destination 'bin\adt.bat')) -or
        -not [IO.File]::Exists((Join-Path $Destination 'lib\adt.jar'))) {
        throw "Downloaded files do not contain a Windows AIR SDK: $Version"
    }
    $number = '{0}.{1}.{2}' -f $Version.Major, $Version.Minor, $Version.Build
    $description = [xml]('<air-sdk-description><name>AIR ' + $number + '</name><version>' + $number + '</version><build>' + $Version.Revision + '</build></air-sdk-description>')
    $description.Save((Join-Path $Destination 'air-sdk-description.xml'))
}

function Install-SdkUpdate($Sdk, [version]$Version) {
    $root = Get-SdkDirectory
    $current = Get-ChildPath $Sdk.Path $root
    if ((Get-Item -LiteralPath $current).Attributes -band [IO.FileAttributes]::ReparsePoint) {
        throw "SDK updates require a regular directory: $current"
    }
    $stage = Get-ChildPath (Join-Path $root ('.asm-update-' + [guid]::NewGuid().ToString('N'))) $root
    $backup = Get-ChildPath (Join-Path $root ('.asm-old-' + [guid]::NewGuid().ToString('N'))) $root
    [IO.Directory]::CreateDirectory($stage) | Out-Null
    try {
        Build-Sdk $Version $stage
        foreach ($name in @('adt.cfg', 'adt.lic')) {
            $source = Join-Path $current ('lib\' + $name)
            if ([IO.File]::Exists($source)) { Copy-Item -LiteralPath $source -Destination (Join-Path $stage ('lib\' + $name)) -Force }
        }
        $original = Read-Sdk (Get-Item -LiteralPath $current)
        if (-not $original -or $original.Version -ne $Sdk.Version) { throw "SDK changed while downloading: $current" }
        [IO.Directory]::Move($current, $backup)
        try { [IO.Directory]::Move($stage, $current) }
        catch {
            if ([IO.Directory]::Exists($current) -or [IO.File]::Exists($current)) {
                throw "Cannot replace SDK: $current. Original SDK remains at $backup."
            }
            try { [IO.Directory]::Move($backup, $current) }
            catch { throw "Cannot restore SDK. Original SDK remains at $backup. $($_.Exception.Message)" }
            throw
        }
        $safeBackup = Get-ChildPath $backup $root
        Remove-Item -LiteralPath $safeBackup -Recurse -Force
        Write-AsmLine ("Updated $($Sdk.Version) -> $Version") 'Accent'
        Write-AsmLine ("Path: $current") 'Muted'
    } finally {
        if ([IO.Directory]::Exists($stage)) {
            $safeStage = Get-ChildPath $stage $root
            Remove-Item -LiteralPath $safeStage -Recurse -Force
        }
    }
}

function Install-Sdk {
    $request = $env:ASM_INSTALL_VERSION
    if (-not $request) { throw 'Usage: asm install VERSION [--accept-license]. Run asm help install.' }
    if ($request -ne 'latest' -and $request -notmatch '^\d+(\.\d+){0,3}$') {
        throw 'Expected a version such as 51.4, 51.4.1.1 or latest.'
    }
    $root = Get-SdkDirectory
    if ($request -match '^\d+\.\d+\.\d+\.\d+$') { $version = [version]$request }
    else {
        if ($request -ne 'latest') {
            $request = ($request.Split('.') | ForEach-Object { ([int]$_).ToString() }) -join '.'
        }
        $version = Get-ReleaseVersions | Where-Object {
            $request -eq 'latest' -or $_.ToString().StartsWith($request + '.')
        } | Sort-Object -Descending | Select-Object -First 1
        if (-not $version) { throw "No AIR SDK version matches $request. Run asm search." }
    }
    if ([IO.Directory]::Exists($root)) {
        $installed = Get-InstalledSdks | Where-Object { $_.Version -eq $version } | Select-Object -First 1
        if ($installed) { Write-AsmLine ("AIR SDK $version is already installed at $($installed.Path)"); return }
    }
    $destination = Get-ChildPath (Join-Path $root ('AIRSDK_' + $version)) $root
    if ([IO.Directory]::Exists($destination) -or [IO.File]::Exists($destination)) {
        throw "Installation path is already occupied: $destination"
    }
    if (-not $env:ASM_ACCEPT_LICENSE -and (Get-ManagerSetting 'HAS_ACCEPTED_LICENSE') -ne 'true') {
        throw 'Accept the AIR SDK license using --accept-license, or use AIR SDK Manager first.'
    }
    [IO.Directory]::CreateDirectory($root) | Out-Null
    $stage = Get-ChildPath (Join-Path $root ('.asm-install-' + [guid]::NewGuid().ToString('N'))) $root
    $lock = $null
    try {
        $lockFile = Join-Path $root '.asm-update.lock'
        $lock = [IO.FileStream]::new($lockFile, [IO.FileMode]::OpenOrCreate, [IO.FileAccess]::ReadWrite, [IO.FileShare]::None, 4096, [IO.FileOptions]::DeleteOnClose)
        $installed = Get-InstalledSdks | Where-Object { $_.Version -eq $version } | Select-Object -First 1
        if ($installed) { Write-AsmLine ("AIR SDK $version is already installed at $($installed.Path)"); return }
        if ([IO.Directory]::Exists($destination) -or [IO.File]::Exists($destination)) {
            throw "Installation path is already occupied: $destination"
        }
        [IO.Directory]::CreateDirectory($stage) | Out-Null
        Write-AsmHeading ("Install AIR SDK $version")
        Write-AsmLine ("Destination: $destination") 'Muted'
        Build-Sdk $version $stage
        [IO.Directory]::Move($stage, $destination)
        Write-AsmLine ("Installed AIR SDK $version") 'Accent'
        Write-AsmLine ("Path: $destination") 'Muted'
    } finally {
        if ([IO.Directory]::Exists($stage)) {
            $safeStage = Get-ChildPath $stage $root
            Remove-Item -LiteralPath $safeStage -Recurse -Force
        }
        if ($lock) { $lock.Dispose() }
    }
}

function Update-Sdks {
    $filter = $env:ASM_UPDATE_VERSION
    if ($filter -and $filter -notmatch '^\d+(\.\d+){0,3}$') {
        throw 'Expected an installed SDK version such as 51.3 or 51.3.4.1.'
    }
    if ($filter) { $filter = ($filter.Split('.') | ForEach-Object { ([int]$_).ToString() }) -join '.' }
    $sdks = @(Get-InstalledSdks | Where-Object {
        -not $filter -or $_.Version.ToString() -eq $filter -or $_.Version.ToString().StartsWith($filter + '.')
    })
    if (-not $sdks.Count -and $filter) { throw "No installed SDK matches $filter. Run asm list." }
    $versions = @(Get-ReleaseVersions)
    $updates = @(
        foreach ($sdk in $sdks) {
            $latest = $versions | Where-Object {
                $_.Major -eq $sdk.Version.Major -and $_.Minor -eq $sdk.Version.Minor -and
                $_.Build -eq $sdk.Version.Build -and $_ -gt $sdk.Version
            } | Sort-Object -Descending | Select-Object -First 1
            if ($latest) { [pscustomobject]@{ Sdk = $sdk; Available = $latest } }
        }
    )
    if ($updates.Count) {
        Write-AsmHeading 'Available updates'
        $rows = @(foreach ($update in $updates) { ,@($update.Sdk.Version.ToString(), $update.Available.ToString(), $update.Sdk.Path) })
        Write-AsmRows $rows @('Installed', 'Available', 'Path')
    } elseif ($sdks.Count) { Write-AsmLine 'Installed SDKs are up to date.' }
    else { Write-AsmLine 'No local AIR SDK versions found.' }
    if (-not $filter) {
        $latestRelease = $versions | Sort-Object -Descending | Select-Object -First 1
        $newestInstalled = $sdks | Sort-Object Version -Descending | Select-Object -First 1
        $installedBranch = $sdks | Where-Object {
            $_.Version.Major -eq $latestRelease.Major -and $_.Version.Minor -eq $latestRelease.Minor -and
            $_.Version.Build -eq $latestRelease.Build
        }
        if ($latestRelease -and (-not $newestInstalled -or $latestRelease -gt $newestInstalled.Version) -and -not $installedBranch) {
            Write-AsmLine
            Write-AsmLine ("New AIR SDK available: $latestRelease") 'Accent'
            Write-AsmLine ('Install: asm install {0}.{1}' -f $latestRelease.Major, $latestRelease.Minor)
        }
    }
    $apply = ($filter -or $env:ASM_UPDATE_ALL) -and -not $env:ASM_UPDATE_CHECK
    if (-not $updates.Count -or -not $apply) {
        if ($updates.Count -and $script:AsmInteractive) {
            Write-AsmLine
            $action = if ($filter) { $filter } else { '--all' }
            Write-AsmWrapped ("Apply: asm update $action") 2 'Accent'
        }
        return
    }
    if (-not $env:ASM_ACCEPT_LICENSE -and (Get-ManagerSetting 'HAS_ACCEPTED_LICENSE') -ne 'true') {
        throw 'Accept the AIR SDK license using --accept-license, or use AIR SDK Manager first.'
    }
    $lock = $null
    try {
        $lockFile = Join-Path (Get-ManagerSetting 'AIR_SDKS') '.asm-update.lock'
        $lock = [IO.FileStream]::new($lockFile, [IO.FileMode]::OpenOrCreate, [IO.FileAccess]::ReadWrite, [IO.FileShare]::None, 4096, [IO.FileOptions]::DeleteOnClose)
        foreach ($update in $updates) { Install-SdkUpdate $update.Sdk $update.Available }
    } finally { if ($lock) { $lock.Dispose() } }
}

try {
    switch ($Command) {
        'list' { Show-InstalledSdks }
        'search' { Search-Sdks }
        'update' { Update-Sdks }
        'install' { Install-Sdk }
        'help' { Show-AsmHelp $Topic }
        'version' { Write-AsmLine $script:AsmVersion 'Accent' }
    }
    exit 0
} catch {
    Write-AsmLine ("Error: {0}" -f $_.Exception.Message) -Stderr
    exit 1
}
