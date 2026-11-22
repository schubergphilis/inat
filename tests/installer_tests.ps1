#requires -Version 5.1
<#
.SYNOPSIS
    Unit tests for install.ps1 and uninstall.ps1 (plan 0002 section 4.4).

.DESCRIPTION
    Exercises every purely-local helper: anonymous downloads, release
    URLs, target triple detection, install-dir resolution, tag
    filtering, hash extraction, version canonicalisation, and PATH
    mutation. End-to-end install against a real release is out of scope
    here -- a reviewer runs the one-liner on a fresh Windows host (spec
    0002 section 13, spec 0027 section 6).

    Runner: no Pester dependency -- inline $script:Pass / $script:Fail
    counters with coloured output, mirroring tests/installer_tests.sh.
#>

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$repoRoot = Split-Path -Parent $PSScriptRoot
$script:Pass = 0
$script:Fail = 0

function Assert-Equal {
    param([string]$Label, $Actual, $Expected)
    if ($Actual -ceq $Expected) {
        Write-Host "  [OK]   $Label" -ForegroundColor Green
        $script:Pass++
    } else {
        Write-Host "  [FAIL] $Label" -ForegroundColor Red
        Write-Host "         expected: '$Expected'"
        Write-Host "         actual:   '$Actual'"
        $script:Fail++
    }
}

function Assert-Throws {
    param([string]$Label, [scriptblock]$Block, [string]$MessagePattern = '.*')
    $threw = $false
    $msg = ''
    try {
        & $Block | Out-Null
    } catch {
        $threw = $true
        $msg = $_.Exception.Message
    }
    if ($threw -and $msg -match $MessagePattern) {
        Write-Host "  [OK]   $Label" -ForegroundColor Green
        $script:Pass++
    } elseif ($threw) {
        Write-Host "  [FAIL] $Label" -ForegroundColor Red
        Write-Host "         threw '$msg' -- did not match /$MessagePattern/"
        $script:Fail++
    } else {
        Write-Host "  [FAIL] $Label" -ForegroundColor Red
        Write-Host "         block did not throw"
        $script:Fail++
    }
}

# --- Load install.ps1 in test mode ----------------------------------

$env:INAT_INSTALL_TEST = '1'
. (Join-Path $repoRoot 'install.ps1')

# --- Invoke-ReleaseDownload is anonymous ----------------------------
# Shadow Invoke-WebRequest to record its arguments; a token left in the
# environment must never reach the request.

function Invoke-WebRequest { $script:IwrArgs = @($args) }
$env:INAT_TOKEN = 'should-not-be-sent'
$script:IwrArgs = @()
Invoke-ReleaseDownload -Url 'https://example.invalid/asset' -OutFile 'unused'
$sent = ($script:IwrArgs | ForEach-Object { "$_" }) -join ' '
Assert-Equal 'Invoke-ReleaseDownload sends no headers' ($sent -match 'Headers|should-not-be-sent') $false
Assert-Equal 'Invoke-ReleaseDownload requests the URL' ($sent -match 'example\.invalid/asset') $true
Remove-Item Env:\INAT_TOKEN
Remove-Item Function:\Invoke-WebRequest

# --- GitHub URLs ----------------------------------------------------

Assert-Equal 'ReleasesUrl is the GitHub API' $script:ReleasesUrl `
    'https://api.github.com/repos/schubergphilis/inat/releases?per_page=50'
Assert-Equal 'Get-AssetUrl is a GitHub release download' `
    (Get-AssetUrl -Tag 'v0.9.0' -FileName 'inat-0.9.0-x86_64-pc-windows-msvc.zip') `
    'https://github.com/schubergphilis/inat/releases/download/v0.9.0/inat-0.9.0-x86_64-pc-windows-msvc.zip'

# --- Resolve-TargetTriple -------------------------------------------

Assert-Equal 'Resolve-TargetTriple AMD64'  (Resolve-TargetTriple -Architecture 'AMD64') 'x86_64-pc-windows-msvc'
Assert-Equal 'Resolve-TargetTriple ARM64'  (Resolve-TargetTriple -Architecture 'ARM64') 'aarch64-pc-windows-msvc'
Assert-Equal 'Resolve-TargetTriple amd64 (case)' (Resolve-TargetTriple -Architecture 'amd64') 'x86_64-pc-windows-msvc'
Assert-Throws 'Resolve-TargetTriple rejects x86' {
    Resolve-TargetTriple -Architecture 'X86'
} 'unsupported'

# --- Resolve-InstallDirectory ---------------------------------------

Assert-Equal 'Resolve-InstallDirectory -ParamDir wins' `
    (Resolve-InstallDirectory -ParamDir 'C:\p' -EnvDir 'C:\e' -LocalAppData 'C:\Users\u\AppData\Local') `
    'C:\p'

Assert-Equal 'Resolve-InstallDirectory -EnvDir when no ParamDir' `
    (Resolve-InstallDirectory -ParamDir '' -EnvDir 'C:\e' -LocalAppData 'C:\Users\u\AppData\Local') `
    'C:\e'

Assert-Equal 'Resolve-InstallDirectory default from LOCALAPPDATA' `
    (Resolve-InstallDirectory -ParamDir '' -EnvDir '' -LocalAppData 'C:\Users\u\AppData\Local') `
    'C:\Users\u\AppData\Local\Programs\inat\bin'

Assert-Throws 'Resolve-InstallDirectory fails without LOCALAPPDATA' {
    Resolve-InstallDirectory -ParamDir '' -EnvDir '' -LocalAppData ''
} 'LOCALAPPDATA'

# --- Select-LatestTag -----------------------------------------------

$tags = @('v0.3.0-rc.1', 'v0.2.0', 'v0.2.0-rc.0', 'v0.1.0')
Assert-Equal 'Select-LatestTag stable'      (Select-LatestTag -Tags $tags) 'v0.2.0'
Assert-Equal 'Select-LatestTag pre-release' (Select-LatestTag -Tags $tags -IncludePreRelease) 'v0.3.0-rc.1'
Assert-Equal 'Select-LatestTag empty -> null' (Select-LatestTag -Tags @()) $null

# --- Get-ExpectedHash -----------------------------------------------

$sums = @(
    '00aabb  other-file.txt',
    'a1b2c3d4  inat-0.2.0-x86_64-pc-windows-msvc.zip',
    'ff00ee *inat-0.2.0-x86_64-unknown-linux-gnu.tar.gz'
)
Assert-Equal 'Get-ExpectedHash finds zip (two-space form)' `
    (Get-ExpectedHash -SumsLines $sums -FileName 'inat-0.2.0-x86_64-pc-windows-msvc.zip') `
    'a1b2c3d4'

Assert-Equal 'Get-ExpectedHash finds tar.gz (star form)' `
    (Get-ExpectedHash -SumsLines $sums -FileName 'inat-0.2.0-x86_64-unknown-linux-gnu.tar.gz') `
    'ff00ee'

Assert-Equal 'Get-ExpectedHash missing file -> empty' `
    (Get-ExpectedHash -SumsLines $sums -FileName 'not-there.zip') `
    ''

# --- ConvertTo-CanonicalVersion -------------------------------------

$v = ConvertTo-CanonicalVersion -Version '0.2.0'
Assert-Equal 'canonical bare -> Tag'  $v.Tag    'v0.2.0'
Assert-Equal 'canonical bare -> Num'  $v.Number '0.2.0'

$v = ConvertTo-CanonicalVersion -Version 'v0.2.0-rc.0'
Assert-Equal 'canonical v-prefixed -> Tag'  $v.Tag    'v0.2.0-rc.0'
Assert-Equal 'canonical v-prefixed -> Num'  $v.Number '0.2.0-rc.0'

# --- Resolve-Version with stubbed fetcher ---------------------------

# GitHub shape: prerelease is a flag independent of the tag, so v0.4.0
# (flagged) and v0.3.0-rc.1 ('-' tag) are both skipped for stable.
$releasesJson = @'
[
  {"tag_name": "v0.4.0", "prerelease": true, "author": {"login": "github-actions[bot]"}},
  {"tag_name": "v0.3.0-rc.1", "prerelease": true},
  {"tag_name": "v0.2.0", "prerelease": false},
  {"tag_name": "v0.2.0-rc.0", "prerelease": true},
  {"tag_name": "v0.1.0", "prerelease": false}
]
'@
$stubPath = New-TemporaryFile
Set-Content -Path $stubPath -Value $releasesJson -Encoding UTF8

$script:FetchedUrl = ''
$stubFetcher = {
    param($Url, $Out)
    $script:FetchedUrl = $Url
    Copy-Item -Force $stubPath $Out
}

$picked = Resolve-Version -Explicit '' -Fetcher $stubFetcher
Assert-Equal 'Resolve-Version stable skips flagged pre-release' $picked 'v0.2.0'
Assert-Equal 'Resolve-Version fetches the releases URL' $script:FetchedUrl $script:ReleasesUrl

$picked = Resolve-Version -Explicit '' -PreReleaseAllowed -Fetcher $stubFetcher
Assert-Equal 'Resolve-Version pre-release via stub' $picked 'v0.4.0'

$picked = Resolve-Version -Explicit 'v0.99.0' -Fetcher {
    param($u, $o); throw 'fetcher should not run'
}
Assert-Equal 'Resolve-Version honours -Explicit' $picked 'v0.99.0'

Remove-Item -Force $stubPath

# --- Update-UserPath (writes user PATH -- we isolate via a save/restore) ---

$savedUserPath = [Environment]::GetEnvironmentVariable('PATH', 'User')
try {
    $fakeDir = 'C:\__inat_test_sentinel_dir'
    $before = [Environment]::GetEnvironmentVariable('PATH', 'User')
    if (-not $before) { $before = '' }

    $a = Update-UserPath -InstallDir $fakeDir
    Assert-Equal 'Update-UserPath appends new dir' $a 'appended:User PATH'

    $after = [Environment]::GetEnvironmentVariable('PATH', 'User')
    $hasSentinel = $after.Split(';') -contains $fakeDir
    Assert-Equal 'Update-UserPath persisted the sentinel' $hasSentinel $true

    $a = Update-UserPath -InstallDir $fakeDir
    Assert-Equal 'Update-UserPath idempotent (already-present)' $a 'already-present'

    $a = Update-UserPath -InstallDir 'C:\__other_sentinel__' -NoModify
    Assert-Equal 'Update-UserPath -NoModify returns skipped' $a 'skipped'
} finally {
    # Always restore the original user PATH, even on test failure.
    [Environment]::SetEnvironmentVariable('PATH', $savedUserPath, 'User')
}

# --- uninstall.ps1 Remove-FromPath ----------------------------------

$env:INAT_UNINSTALL_TEST = '1'
. (Join-Path $repoRoot 'uninstall.ps1')

Assert-Equal 'Remove-FromPath drops exact match' `
    (Remove-FromPath -CurrentPath 'C:\a;C:\b;C:\c' -DirToRemove 'C:\b') `
    'C:\a;C:\c'

Assert-Equal 'Remove-FromPath drops trailing-slash variant' `
    (Remove-FromPath -CurrentPath 'C:\a;C:\b\;C:\c' -DirToRemove 'C:\b') `
    'C:\a;C:\c'

Assert-Equal 'Remove-FromPath no-op when absent' `
    (Remove-FromPath -CurrentPath 'C:\a;C:\b;C:\c' -DirToRemove 'C:\z') `
    'C:\a;C:\b;C:\c'

Assert-Equal 'Remove-FromPath empty input -> empty' `
    (Remove-FromPath -CurrentPath '' -DirToRemove 'C:\b') `
    ''

# --- Summary --------------------------------------------------------

Write-Host ''
Write-Host "$($script:Pass) passed, $($script:Fail) failed"
if ($script:Fail -gt 0) { exit 1 }
