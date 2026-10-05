param([ValidateSet('list', 'search')][string]$Command = 'list')

$ErrorActionPreference = 'Stop'
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

function Show-InstalledSdks {
    if (-not [IO.File]::Exists($configFile)) {
        throw "AIR SDK Manager settings not found: $configFile"
    }
    $sdkDirectory = Get-ManagerSetting 'AIR_SDKS'
    if (-not $sdkDirectory) { throw "AIR_SDKS is not set in $configFile" }
    if (-not [IO.Directory]::Exists($sdkDirectory)) {
        throw "SDK directory does not exist: $sdkDirectory"
    }
    $sdks = @(
        Get-ChildItem -LiteralPath $sdkDirectory -Directory |
            ForEach-Object { Read-Sdk $_ } |
            Sort-Object Version
    )
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

function Search-Sdks {
    $filter = $env:ASM_SEARCH_VERSION
    if ($filter) {
        if ($filter -notmatch '^\d+(\.\d+){0,3}$') { throw 'Expected a version such as 51.4 or 51.4.1.1.' }
        $filter = ($filter.Split('.') | ForEach-Object { ([int]$_).ToString() }) -join '.'
    }
    $endpoint = Get-ManagerSetting 'API_ENDPOINT'
    if (-not $endpoint) { $endpoint = 'https://api.airsdk.harman.com' }
    $uri = $endpoint.TrimEnd('/') + '/releases?types=production'
    try {
        [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
        $response = Invoke-RestMethod -Uri $uri -Method Get -TimeoutSec 15 -UserAgent 'asm/1.0.0'
        if ($response.errorType) { throw "AIR SDK API returned $($response.errorType)." }
        if ($response.releases -isnot [array]) { throw 'AIR SDK API response has no releases array.' }
        $versions = @(Convert-Releases $response.releases)
    } catch {
        $apiError = $_.Exception.Message
        $cache = Get-CachedCatalog
        if (-not $cache) { throw "Cannot load AIR SDK catalog: $apiError" }
        [Console]::Error.WriteLine("Warning: {0} Using cached AIR SDK Manager catalog: {1}", $apiError, $cache.File)
        $versions = $cache.Versions
    }
    $matches = @(
        $versions | Where-Object {
            -not $filter -or $_.ToString() -eq $filter -or $_.ToString().StartsWith($filter + '.')
        } | Sort-Object -Unique -Descending
    )
    if ($matches.Count -eq 0) { 'No matching AIR SDK versions found.' }
    else { foreach ($version in $matches) { $version.ToString() } }
}

try {
    switch ($Command) {
        'list' { Show-InstalledSdks }
        'search' { Search-Sdks }
    }
    exit 0
} catch {
    [Console]::Error.WriteLine("Error: {0}", $_.Exception.Message)
    exit 1
}
