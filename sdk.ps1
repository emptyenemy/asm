param([ValidateSet('list', 'search', 'update', 'install')][string]$Command = 'list')

$ErrorActionPreference = 'Stop'
[Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
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
        [Console]::Error.WriteLine("Warning: Cannot read SDK description in {0}: {1}", $Directory.FullName, $_.Exception.Message)
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
            Sort-Object Version
    )
    $sdks
}

function Show-InstalledSdks {
    $sdks = @(Get-InstalledSdks)
    if ($sdks.Count -eq 0) { 'No local AIR SDK versions found.' }
    else { foreach ($sdk in $sdks) { '{0,-14} {1}' -f $sdk.Version, $sdk.Path } }
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
    $html = (Invoke-WebRequest -UseBasicParsing -Uri 'https://airsdk.dev/news/archive' -TimeoutSec 15 -UserAgent 'asm/1.0.0').Content
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
            $response = Invoke-RestMethod -Uri $uri -Method Get -TimeoutSec 15 -UserAgent 'asm/1.0.0'
            if ($response.errorType) { throw "AIR SDK API returned $($response.errorType)." }
            if ($response.releases -isnot [array]) { throw 'AIR SDK API response has no releases array.' }
            $versions = @(Convert-Releases $response.releases)
        } else { $versions = @(Get-NewsVersions) }
    } catch {
        $apiError = $_.Exception.Message
        $cache = Get-CachedCatalog
        if (-not $cache) { throw "Cannot load AIR SDK catalog: $apiError" }
        [Console]::Error.WriteLine("Warning: {0} Using cached AIR SDK Manager catalog: {1}", $apiError, $cache.File)
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
    if ($matches.Count -eq 0) { 'No matching AIR SDK versions found.' }
    else { foreach ($version in $matches) { $version.ToString() } }
}


function Get-WindowsPackage([version]$Version) {
    $catalog = Invoke-RestMethod -Uri 'https://shockpkg.github.io/packages/api/1/packages.json' -TimeoutSec 15
    if ($catalog.packages -isnot [array]) { throw 'Invalid shockpkg SDK catalog.' }
    $package = $catalog.packages | Where-Object { $_.name -eq "air-sdk-$Version-windows-compiler" } | Select-Object -First 1
    if (-not $package) { throw "No Windows SDK download found for $Version." }
    [Console]::Error.WriteLine('Using the shockpkg download mirror with SHA-256 verification.')
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
        $manifest = Invoke-RestMethod -Uri ($endpoint.TrimEnd('/') + '/releases/' + $Version + '?types=production') -TimeoutSec 15
        if ($manifest.name -ne $Version.ToString() -or $manifest.type -ne 'production') {
            throw "Invalid manifest for AIR SDK $Version."
        }
        return $manifest
    }
}

function Get-ArchiveHash([string]$File) {
    $stream = [IO.File]::OpenRead($File)
    $algorithm = [Security.Cryptography.SHA256]::Create()
    try { [BitConverter]::ToString($algorithm.ComputeHash($stream)).Replace('-', '').ToLowerInvariant() }
    finally { $stream.Dispose(); $algorithm.Dispose() }
}

function Get-SdkArchive([string]$Url, [string]$Checksum, [long]$Size, [string]$Name, [string]$Method = 'Get') {
    $Checksum = $Checksum.Trim()
    if ($Checksum -notmatch '^[a-fA-F0-9]{64}$') { throw "Invalid SHA-256 for $Name." }
    $cacheDirectory = Join-Path (Split-Path $configFile -Parent) 'asm-cache'
    [IO.Directory]::CreateDirectory($cacheDirectory) | Out-Null
    $file = Join-Path $cacheDirectory ($Checksum.ToLowerInvariant() + '.zip')
    if ([IO.File]::Exists($file) -and ($Size -le 0 -or (Get-Item -LiteralPath $file).Length -eq $Size) -and
        (Get-ArchiveHash $file) -eq $Checksum) { return $file }
    $temporary = $file + '.download'
    try {
        Write-Host "Downloading $Name..."
        $arguments = @{ Uri = $Url; Method = $Method; OutFile = $temporary; TimeoutSec = 600; UseBasicParsing = $true }
        if ($Method -eq 'Post') { $arguments.Body = @{ acceptedLicense = 'true' } }
        Invoke-WebRequest @arguments | Out-Null
        if ($Size -gt 0 -and (Get-Item -LiteralPath $temporary).Length -ne $Size) { throw "Download size mismatch for $Name." }
        if ((Get-ArchiveHash $temporary) -ne $Checksum) { throw "SHA-256 mismatch for $Name." }
        Move-Item -LiteralPath $temporary -Destination $file -Force
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
    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $archive = [IO.Compression.ZipFile]::OpenRead($File)
    try {
        foreach ($entry in $archive.Entries) {
            if ($entry.FullName -match ':|^[/\\]') { throw "Invalid ZIP entry: $($entry.FullName)" }
            $target = Get-ChildPath (Join-Path $Destination $entry.FullName) $Destination
            if ($entry.FullName.EndsWith('/') -or $entry.FullName.EndsWith('\')) {
                [IO.Directory]::CreateDirectory($target) | Out-Null
            } else {
                [IO.Directory]::CreateDirectory([IO.Path]::GetDirectoryName($target)) | Out-Null
                [IO.Compression.ZipFileExtensions]::ExtractToFile($entry, $target, $true)
            }
        }
    } finally { $archive.Dispose() }
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
            $archive = Get-SdkArchive $url $details.checksum $details.fileSize $component.Name 'Post'
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
        $archive = Get-SdkArchive $url $details.checksum $details.fileSize "AIR SDK $Version"
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
    $backupDirectory = Get-ChildPath (Join-Path $root '.asm-backups') $root
    if ([IO.Directory]::Exists($backupDirectory) -and
        ((Get-Item -LiteralPath $backupDirectory).Attributes -band [IO.FileAttributes]::ReparsePoint)) {
        throw "Backup directory must not be a link: $backupDirectory"
    }
    $backup = Get-ChildPath (Join-Path $backupDirectory ((Split-Path $current -Leaf) + '-' + $Sdk.Version + '-' + [guid]::NewGuid().ToString('N'))) $root
    [IO.Directory]::CreateDirectory($stage) | Out-Null
    try {
        Build-Sdk $Version $stage
        foreach ($name in @('adt.cfg', 'adt.lic')) {
            $source = Join-Path $current ('lib\' + $name)
            if ([IO.File]::Exists($source)) { Copy-Item -LiteralPath $source -Destination (Join-Path $stage ('lib\' + $name)) -Force }
        }
        $original = Read-Sdk (Get-Item -LiteralPath $current)
        if (-not $original -or $original.Version -ne $Sdk.Version) { throw "SDK changed while downloading: $current" }
        [IO.Directory]::CreateDirectory($backupDirectory) | Out-Null
        Move-Item -LiteralPath $current -Destination $backup
        try { Move-Item -LiteralPath $stage -Destination $current }
        catch {
            if (-not [IO.Directory]::Exists($current)) { Move-Item -LiteralPath $backup -Destination $current }
            throw
        }
        "Updated $($Sdk.Version) -> $Version at $current"
        "Backup: $backup"
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
        if ($installed) { "AIR SDK $version is already installed at $($installed.Path)"; return }
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
        $lock = [IO.File]::Open($lockFile, [IO.FileMode]::OpenOrCreate, [IO.FileAccess]::ReadWrite, [IO.FileShare]::None)
        $installed = Get-InstalledSdks | Where-Object { $_.Version -eq $version } | Select-Object -First 1
        if ($installed) { "AIR SDK $version is already installed at $($installed.Path)"; return }
        if ([IO.Directory]::Exists($destination) -or [IO.File]::Exists($destination)) {
            throw "Installation path is already occupied: $destination"
        }
        [IO.Directory]::CreateDirectory($stage) | Out-Null
        "Installing AIR SDK $version at $destination"
        Build-Sdk $version $stage
        [IO.Directory]::Move($stage, $destination)
        "Installed AIR SDK $version at $destination"
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
    if (-not $sdks.Count) {
        if ($filter) { throw "No installed SDK matches $filter. Run asm list." }
        'No local AIR SDK versions found.'
        return
    }
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
    if (-not $updates.Count) { 'No AIR SDK updates found.'; return }
    '{0,-14} {1,-14} {2}' -f 'Installed', 'Available', 'Path'
    foreach ($update in $updates) {
        '{0,-14} {1,-14} {2}' -f $update.Sdk.Version, $update.Available, $update.Sdk.Path
    }
    $apply = ($filter -or $env:ASM_UPDATE_ALL) -and -not $env:ASM_UPDATE_CHECK
    if (-not $apply) { return }
    if (-not $env:ASM_ACCEPT_LICENSE -and (Get-ManagerSetting 'HAS_ACCEPTED_LICENSE') -ne 'true') {
        throw 'Accept the AIR SDK license using --accept-license, or use AIR SDK Manager first.'
    }
    $lock = $null
    try {
        $lockFile = Join-Path (Get-ManagerSetting 'AIR_SDKS') '.asm-update.lock'
        $lock = [IO.File]::Open($lockFile, [IO.FileMode]::OpenOrCreate, [IO.FileAccess]::ReadWrite, [IO.FileShare]::None)
        foreach ($update in $updates) { Install-SdkUpdate $update.Sdk $update.Available }
    } finally { if ($lock) { $lock.Dispose() } }
}

try {
    switch ($Command) {
        'list' { Show-InstalledSdks }
        'search' { Search-Sdks }
        'update' { Update-Sdks }
        'install' { Install-Sdk }
    }
    exit 0
} catch {
    [Console]::Error.WriteLine("Error: {0}", $_.Exception.Message)
    exit 1
}
