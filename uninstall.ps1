#requires -Version 5.1
<#
.SYNOPSIS
    inat uninstaller for Windows (PowerShell 5.1+).

.DESCRIPTION
    Implements docs/specs/0002-ci-cd-and-install.md section 9.5 for Windows:
      1. Remove the inat.exe binary from InstallDir.
      2. Remove InstallDir from the user PATH env var (User scope).
      3. Leave project .inat/ directories and %USERPROFILE%\.inat\
         untouched -- user data is preserved.

.PARAMETER InstallDir
    Directory the binary lives in. Default
    $env:LOCALAPPDATA\Programs\inat\bin.

.NOTES
    Environment:
      INAT_INSTALL_DIR    Overrides the default install directory.
      INAT_UNINSTALL_TEST Set to 1 to suppress main execution (tests).

    Exit codes: 0 success, 1 unknown error, 2 unsupported flag.
#>
[CmdletBinding()]
param(
    [string]$InstallDir
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

function Stop-Uninstall {
    param([string]$Message, [int]$Code = 1)
    Write-Host "error: $Message" -ForegroundColor Red
    exit $Code
}

function Resolve-InstallDirectoryForUninstall {
    param(
        [AllowEmptyString()][string]$ParamDir,
        [AllowEmptyString()][string]$EnvDir,
        [AllowEmptyString()][string]$LocalAppData
    )
    if ($ParamDir) { return $ParamDir }
    if ($EnvDir)   { return $EnvDir }
    if (-not $LocalAppData) {
        throw 'LOCALAPPDATA is not set; pass -InstallDir or set INAT_INSTALL_DIR.'
    }
    return (Join-Path $LocalAppData 'Programs\inat\bin')
}

# Pure helper: given a PATH string and a directory, return the PATH
# with that directory removed (case-insensitive, preserves order).
function Remove-FromPath {
    param(
        [AllowEmptyString()][string]$CurrentPath,
        [Parameter(Mandatory)][string]$DirToRemove
    )
    if (-not $CurrentPath) { return '' }
    $parts = $CurrentPath.Split(';', [StringSplitOptions]::RemoveEmptyEntries)
    $kept  = $parts | Where-Object { $_ -and ($_.TrimEnd('\') -ne $DirToRemove.TrimEnd('\')) }
    return ($kept -join ';')
}

function Invoke-InatUninstall {
    param([AllowEmptyString()][string]$InstallDirParam)

    try {
        $dir = Resolve-InstallDirectoryForUninstall `
            -ParamDir $InstallDirParam `
            -EnvDir $env:INAT_INSTALL_DIR `
            -LocalAppData $env:LOCALAPPDATA
    } catch {
        Stop-Uninstall $_.Exception.Message 2
    }

    $bin = Join-Path $dir 'inat.exe'
    $binAction = 'absent'
    if (Test-Path $bin) {
        Remove-Item -Force $bin
        Write-Host "removed $bin"
        $binAction = 'removed'
    } else {
        Write-Host "no binary at $bin (already gone?)"
    }

    $currentUser = [Environment]::GetEnvironmentVariable('PATH', 'User')
    $currentStr  = if ($currentUser) { $currentUser } else { '' }
    $updated     = Remove-FromPath -CurrentPath $currentStr -DirToRemove $dir
    $pathAction  = 'unchanged'
    if ($updated -ne $currentStr) {
        [Environment]::SetEnvironmentVariable('PATH', $updated, 'User')
        $pathAction = 'removed-from-User-PATH'
    }

    Write-Host ''
    Write-Host 'inat uninstalled.'
    Write-Host "  binary:       $binAction ($bin)"
    Write-Host "  PATH action:  $pathAction"
    if ($pathAction -eq 'removed-from-User-PATH') {
        Write-Host '  -> open a new shell to drop the PATH entry.'
    }
    Write-Host ''
    Write-Host 'Note: project-level .inat\ directories and %USERPROFILE%\.inat\'
    Write-Host '      are preserved -- remove them manually for a fully clean slate.'
}

if ($env:INAT_UNINSTALL_TEST -ne '1') {
    Invoke-InatUninstall -InstallDirParam $InstallDir
}
