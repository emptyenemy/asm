$ErrorActionPreference = 'Stop'
$configFile = Join-Path $env:USERPROFILE '.airsdk\airsdkmanager.cfg'

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

try {
    if (-not [IO.File]::Exists($configFile)) {
        throw "AIR SDK Manager settings not found: $configFile"
    }
    $sdkDirectory = $null
    foreach ($line in [IO.File]::ReadAllLines($configFile, [Text.Encoding]::UTF8)) {
        if ($line -match '^\s*AIR_SDKS\s*=(.*)$') {
            $sdkDirectory = $Matches[1].Trim().Trim('"')
        }
    }
    if (-not $sdkDirectory) { throw "AIR_SDKS is not set in $configFile" }
    if (-not [IO.Directory]::Exists($sdkDirectory)) {
        throw "SDK directory does not exist: $sdkDirectory"
    }

    $sdks = @(
        Get-ChildItem -LiteralPath $sdkDirectory -Directory |
            ForEach-Object { Read-Sdk $_ } |
            Sort-Object Version
    )
    if ($sdks.Count -eq 0) {
        'No local AIR SDK versions found.'
    } else {
        foreach ($sdk in $sdks) { '{0,-14} {1}' -f $sdk.Version, $sdk.Path }
    }
    exit 0
} catch {
    [Console]::Error.WriteLine("Error: {0}", $_.Exception.Message)
    exit 1
}
