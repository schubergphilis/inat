#requires -Version 5.1
<#
.SYNOPSIS
    inat installer for Windows (PowerShell 5.1+).

.DESCRIPTION
    Implements the Windows half of docs/specs/0002-ci-cd-and-install.md
    section 9, as amended by spec 0027 (GitHub Releases, anonymous
    downloads). Canonical one-liner:

        irm https://github.com/schubergphilis/inat/releases/latest/download/install.ps1 | iex

.PARAMETER Version
    Install a specific release (e.g. v0.2.0) instead of the latest.

.PARAMETER PreRelease
    Include pre-release tags when resolving "latest".

.PARAMETER NoModifyPath
    Do not mutate the user PATH; print the directory to add manually.

.PARAMETER InstallDir
    Override the install directory. Default
    $env:LOCALAPPDATA\Programs\inat\bin.

.NOTES
    Environment:
      INAT_INSTALL_DIR  Overrides the default install directory.
      INAT_INSTALL_TEST Set to 1 to suppress main execution (tests).

    Exit codes (plan 0002 section 4.3):
      0 success
      1 unknown error
      2 unsupported host / unsupported flag
      3 checksum mismatch
      4 network failure after one retry
      5 requested version not found
      6 archive layout unexpected
#>
[CmdletBinding()]
param(
    [string]$Version,
    [switch]$PreRelease,
    [switch]$NoModifyPath,
    [string]$InstallDir
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$ProgressPreference    = 'SilentlyContinue'

# Older Windows PowerShell defaults to SSL3/TLS1; GitHub requires TLS 1.2.
[Net.ServicePointManager]::SecurityProtocol =
    [Net.SecurityProtocolType]::Tls12 -bor [Net.SecurityProtocolType]::Tls11

$script:Repo         = 'schubergphilis/inat'
$script:ReleasesUrl  = "https://api.github.com/repos/$($script:Repo)/releases?per_page=50"
$script:DownloadBase = "https://github.com/$($script:Repo)/releases/download"

$script:ExitUnsupported = 2
$script:ExitChecksum    = 3
$script:ExitNetwork     = 4
$script:ExitNotFound    = 5
$script:ExitArchive     = 6

function Stop-Install {
    param([string]$Message, [int]$Code = 1)
    Write-Host "error: $Message" -ForegroundColor Red
    exit $Code
}

# --- Pure helpers (all input via parameters; safe for unit tests) ---

function Get-AssetUrl {
    param(
        [Parameter(Mandatory)][string]$Tag,
        [Parameter(Mandatory)][string]$FileName
    )
    return "$($script:DownloadBase)/$Tag/$FileName"
}

function Resolve-TargetTriple {
    param([Parameter(Mandatory)][string]$Architecture)
    switch ($Architecture.ToUpperInvariant()) {
        'AMD64' { return 'x86_64-pc-windows-msvc' }
        'ARM64' { return 'aarch64-pc-windows-msvc' }
        default { throw "unsupported Windows architecture: $Architecture" }
    }
}

function Resolve-InstallDirectory {
    param(
        [AllowEmptyString()][string]$ParamDir,
        [AllowEmptyString()][string]$EnvDir,
        [AllowEmptyString()][string]$LocalAppData
    )
    if ($ParamDir)     { return $ParamDir }
    if ($EnvDir)       { return $EnvDir }
    if (-not $LocalAppData) {
        throw 'LOCALAPPDATA is not set; pass -InstallDir or set INAT_INSTALL_DIR.'
    }
    return (Join-Path $LocalAppData 'Programs\inat\bin')
}

function Select-LatestTag {
    param(
        [Parameter(Mandatory)][AllowEmptyCollection()][string[]]$Tags,
        [switch]$IncludePreRelease
    )
    if ($IncludePreRelease) {
        return ($Tags | Select-Object -First 1)
    }
    return ($Tags | Where-Object { $_ -notmatch '-' } | Select-Object -First 1)
}

function Get-ExpectedHash {
    param(
        [Parameter(Mandatory)][string[]]$SumsLines,
        [Parameter(Mandatory)][string]$FileName
    )
    # Lines look like "<hash>  <filename>" or "<hash> *<filename>".
    $escaped = [regex]::Escape($FileName)
    foreach ($line in $SumsLines) {
        if ($line -match "^(?<hash>[0-9a-fA-F]+)\s+\*?$escaped\s*$") {
            return $Matches['hash'].ToLowerInvariant()
        }
    }
    return ''
}

function ConvertTo-CanonicalVersion {
    param([Parameter(Mandatory)][string]$Version)
    $bare = $Version.TrimStart('v', 'V')
    return [pscustomobject]@{ Tag = "v$bare"; Number = $bare }
}

# --- Network helper (anonymous, retries once; tests stub it). ---

function Invoke-ReleaseDownload {
    param(
        [Parameter(Mandatory)][string]$Url,
        [Parameter(Mandatory)][string]$OutFile
    )
    $attempt = 0
    while ($true) {
        $attempt++
        try {
            Invoke-WebRequest -UseBasicParsing `
                              -Uri $Url `
                              -OutFile $OutFile `
                              -ErrorAction Stop | Out-Null
            return
        } catch {
            if ($attempt -ge 2) {
                Stop-Install "download failed after retry: $Url" $script:ExitNetwork
            }
            Start-Sleep -Seconds 2
        }
    }
}

# --- Version resolution (uses the releases API; testable via stubbing). ---

function Resolve-Version {
    param(
        [AllowEmptyString()][string]$Explicit,
        [switch]$PreReleaseAllowed,
        [scriptblock]$Fetcher = $null
    )
    if ($Explicit) { return $Explicit }

    $tmp = New-TemporaryFile
    try {
        if ($Fetcher) {
            & $Fetcher $script:ReleasesUrl $tmp.FullName
        } else {
            Invoke-ReleaseDownload -Url $script:ReleasesUrl -OutFile $tmp.FullName
        }
        $raw = Get-Content -Raw -Path $tmp.FullName
        $data = $raw | ConvertFrom-Json
        # Stable means not flagged prerelease and no '-' in the tag,
        # matching `inat self update` (spec 0027 section 4).
        $tags = @($data |
            Where-Object { $PreReleaseAllowed -or -not $_.prerelease } |
            ForEach-Object { $_.tag_name })
        $picked = Select-LatestTag -Tags $tags -IncludePreRelease:$PreReleaseAllowed
        if (-not $picked) {
            Stop-Install "no release found (pre-release=$PreReleaseAllowed). Try -PreRelease or -Version vX.Y.Z." $script:ExitNotFound
        }
        return $picked
    } finally {
        Remove-Item -Force -ErrorAction SilentlyContinue $tmp
    }
}

# --- PATH handling ---

function Update-UserPath {
    param(
        [Parameter(Mandatory)][string]$InstallDir,
        [switch]$NoModify
    )
    $current = [Environment]::GetEnvironmentVariable('PATH', 'User')
    if (-not $current) { $current = '' }
    $parts = $current.Split(';', [StringSplitOptions]::RemoveEmptyEntries)
    if ($parts -contains $InstallDir) {
        return 'already-present'
    }
    if ($NoModify) {
        Write-Host ""
        Write-Host "PATH mutation skipped (-NoModifyPath). Add this manually:"
        Write-Host "  $InstallDir"
        return 'skipped'
    }
    $new = if ($current) { "$current;$InstallDir" } else { $InstallDir }
    [Environment]::SetEnvironmentVariable('PATH', $new, 'User')
    return 'appended:User PATH'
}

# --- Main pipeline ---

function Invoke-InatInstall {
    param(
        [AllowEmptyString()][string]$VersionParam,
        [switch]$PreReleaseParam,
        [switch]$NoModifyPathParam,
        [AllowEmptyString()][string]$InstallDirParam
    )

    try {
        $target = Resolve-TargetTriple -Architecture $env:PROCESSOR_ARCHITECTURE
    } catch {
        Stop-Install $_.Exception.Message $script:ExitUnsupported
    }

    try {
        $dir = Resolve-InstallDirectory -ParamDir $InstallDirParam `
                                        -EnvDir $env:INAT_INSTALL_DIR `
                                        -LocalAppData $env:LOCALAPPDATA
    } catch {
        Stop-Install $_.Exception.Message $script:ExitUnsupported
    }

    $resolved = Resolve-Version -Explicit $VersionParam `
                                -PreReleaseAllowed:$PreReleaseParam
    $v = ConvertTo-CanonicalVersion -Version $resolved

    $stage = New-Item -ItemType Directory -Path (Join-Path $env:TEMP "inat-install-$([guid]::NewGuid())")
    try {
        $archive = "inat-$($v.Number)-$target.zip"
        $archiveUrl = Get-AssetUrl -Tag $v.Tag -FileName $archive
        $sumsUrl    = Get-AssetUrl -Tag $v.Tag -FileName 'SHA256SUMS'

        Write-Host "==> downloading $archive"
        Invoke-ReleaseDownload -Url $archiveUrl -OutFile (Join-Path $stage.FullName $archive)
        Write-Host "==> downloading SHA256SUMS"
        Invoke-ReleaseDownload -Url $sumsUrl -OutFile (Join-Path $stage.FullName 'SHA256SUMS')

        $sumsLines = Get-Content (Join-Path $stage.FullName 'SHA256SUMS')
        $expected = Get-ExpectedHash -SumsLines $sumsLines -FileName $archive
        if (-not $expected) {
            Stop-Install "no SHA256 for $archive in SHA256SUMS (version $($v.Number) not published for $target?)" $script:ExitNotFound
        }
        $actual = (Get-FileHash -Algorithm SHA256 (Join-Path $stage.FullName $archive)).Hash.ToLowerInvariant()
        if ($actual -ne $expected) {
            Stop-Install "checksum mismatch for ${archive}: expected $expected, got $actual" $script:ExitChecksum
        }
        Write-Host "==> checksum OK"

        Expand-Archive -Path (Join-Path $stage.FullName $archive) -DestinationPath $stage.FullName -Force
        $binSrc = Join-Path $stage.FullName "inat-$($v.Number)-$target\inat.exe"
        if (-not (Test-Path $binSrc)) {
            Stop-Install "archive missing expected file: inat-$($v.Number)-$target\inat.exe" $script:ExitArchive
        }
        New-Item -ItemType Directory -Force -Path $dir | Out-Null
        Copy-Item -Force $binSrc (Join-Path $dir 'inat.exe')

        $pathAction = Update-UserPath -InstallDir $dir -NoModify:$NoModifyPathParam

        Write-Host ""
        Write-Host "inat $($v.Number) installed."
        Write-Host "  binary:       $dir\inat.exe"
        Write-Host "  PATH action:  $pathAction"
        if ($pathAction -like 'appended:*') {
            Write-Host '  -> open a new shell to pick up the PATH change.'
        }
    } finally {
        Remove-Item -Recurse -Force -ErrorAction SilentlyContinue $stage.FullName
    }
}

# Suppress main when dot-sourced for tests.
if ($env:INAT_INSTALL_TEST -ne '1') {
    Invoke-InatInstall -VersionParam $Version `
                       -PreReleaseParam:$PreRelease `
                       -NoModifyPathParam:$NoModifyPath `
                       -InstallDirParam $InstallDir
}
