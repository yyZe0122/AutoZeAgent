param(
    [string]$Repository = $env:AUTOZEAGENT_REPOSITORY,
    [string]$Version = $env:AUTOZEAGENT_VERSION,
    [string]$InstallDir = $env:AUTOZEAGENT_INSTALL_DIR
)

Set-StrictMode -Version 2.0
$ErrorActionPreference = 'Stop'

if ([string]::IsNullOrWhiteSpace($Repository)) {
    $Repository = 'yyZe0122/AutoZeAgent'
}

if ([string]::IsNullOrWhiteSpace($Version)) {
    $Version = 'latest'
}
if ([string]::IsNullOrWhiteSpace($InstallDir)) {
    if ([string]::IsNullOrWhiteSpace($env:LOCALAPPDATA)) {
        $InstallDir = Join-Path $env:USERPROFILE 'AutoZeAgent\bin'
    } else {
        $InstallDir = Join-Path $env:LOCALAPPDATA 'Programs\AutoZeAgent\bin'
    }
}

$architecture = $env:PROCESSOR_ARCHITEW6432
if ([string]::IsNullOrWhiteSpace($architecture)) {
    $architecture = $env:PROCESSOR_ARCHITECTURE
}
switch ($architecture.ToUpperInvariant()) {
    'AMD64' { $arch = 'amd64' }
    'ARM64' { $arch = 'arm64' }
    default { throw "Unsupported Windows architecture: $architecture" }
}

function Get-ReleaseTag {
    param([string]$Repo, [string]$Requested)
    if ($Requested -ne 'latest') {
        return $Requested
    }
    $api = "https://api.github.com/repos/$Repo/releases/latest"
    try {
        $rel = Invoke-RestMethod -UseBasicParsing -Uri $api
    } catch {
        throw "Could not resolve latest release for $Repo. Set AUTOZEAGENT_VERSION=vX.Y.Z (required for Pre-release-only repos). $_"
    }
    if ([string]::IsNullOrWhiteSpace($rel.tag_name)) {
        throw "Latest release response missing tag_name for $Repo."
    }
    return [string]$rel.tag_name
}

$tag = Get-ReleaseTag -Repo $Repository -Requested $Version
if ($tag.StartsWith('v')) {
    $verNum = $tag.Substring(1)
} else {
    $verNum = $tag
    $tag = "v$tag"
}

$asset = "autozeagent_${verNum}_windows_${arch}.zip"
$baseUrl = "https://github.com/$Repository/releases/download/$tag"

$tempDir = Join-Path ([IO.Path]::GetTempPath()) ('autozeagent-' + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $tempDir | Out-Null
try {
    $archivePath = Join-Path $tempDir $asset
    $checksumPath = Join-Path $tempDir 'checksums.txt'
    Invoke-WebRequest -UseBasicParsing -Uri "$baseUrl/$asset" -OutFile $archivePath
    Invoke-WebRequest -UseBasicParsing -Uri "$baseUrl/checksums.txt" -OutFile $checksumPath

    $expected = $null
    foreach ($line in Get-Content -LiteralPath $checksumPath) {
        $parts = $line.Trim() -split '\s+'
        if ($parts.Count -ge 2 -and $parts[$parts.Count - 1].TrimStart('*') -eq $asset) {
            $expected = $parts[0].ToLowerInvariant()
            break
        }
    }
    if ([string]::IsNullOrWhiteSpace($expected)) {
        throw "No checksum entry found for $asset."
    }

    $actual = (Get-FileHash -Algorithm SHA256 -LiteralPath $archivePath).Hash.ToLowerInvariant()
    if ($actual -ne $expected) {
        throw "Checksum verification failed for $asset."
    }

    Expand-Archive -LiteralPath $archivePath -DestinationPath $tempDir -Force
    New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
    Copy-Item -LiteralPath (Join-Path $tempDir 'autozeagent.exe') -Destination (Join-Path $InstallDir 'autozeagent.exe') -Force
    Copy-Item -LiteralPath (Join-Path $tempDir 'autozeagentd.exe') -Destination (Join-Path $InstallDir 'autozeagentd.exe') -Force
    $azeSource = Join-Path $tempDir 'aze.exe'
    if (Test-Path -LiteralPath $azeSource) {
        Copy-Item -LiteralPath $azeSource -Destination (Join-Path $InstallDir 'aze.exe') -Force
    } else {
        Copy-Item -LiteralPath (Join-Path $tempDir 'autozeagent.exe') -Destination (Join-Path $InstallDir 'aze.exe') -Force
    }

    if ($env:AUTOZEAGENT_NO_PATH_UPDATE -ne '1') {
        $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
        $pathEntries = @()
        if (-not [string]::IsNullOrWhiteSpace($userPath)) {
            $pathEntries = $userPath.Split(';')
        }
        if ($pathEntries -notcontains $InstallDir) {
            $newUserPath = (($pathEntries + $InstallDir) | Where-Object { -not [string]::IsNullOrWhiteSpace($_) }) -join ';'
            [Environment]::SetEnvironmentVariable('Path', $newUserPath, 'User')
        }
        if (($env:Path.Split(';')) -notcontains $InstallDir) {
            $env:Path = $InstallDir + ';' + $env:Path
        }
    }

    Write-Host "AutoZeAgent $tag installed to $InstallDir."
    Write-Host 'Run: autozeagent version'
    Write-Host 'Interactive TUI: aze  (or autozeagent with no arguments)'
    Write-Host 'Open a new terminal if the command is not yet available.'
} finally {
    if (Test-Path -LiteralPath $tempDir) {
        Remove-Item -LiteralPath $tempDir -Recurse -Force
    }
}
