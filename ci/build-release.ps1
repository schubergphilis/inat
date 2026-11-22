# Builds an inat release archive on Windows.
#
# This script is kept for local Windows development workflows; the
# release CI pipeline (spec 0009 M3b) cross-compiles all targets from
# the Linux runner via ci/build-release.sh, so the .ps1 variant is no
# longer invoked in CI.
#
# Usage:
#     .\ci\build-release.ps1 -Target x86_64-pc-windows-msvc
#
# Falls back to $env:GITHUB_REF_NAME if -Tag is not given.
#
# Exit codes:
#   0 — archive built
#   2 — missing arguments
#   3 — go build failed
#   4 — expected binary not found after build
#   5 — unsupported target triple

param(
    [Parameter(Mandatory = $true)][string]$Target,
    [string]$Tag = $env:GITHUB_REF_NAME
)

$ErrorActionPreference = 'Stop'

if (-not $Tag) {
    Write-Error 'no tag provided (-Tag or $env:GITHUB_REF_NAME)'
    exit 2
}

$version = $Tag -replace '^v', ''
$stage   = "inat-$version-$Target"

# Map Rust target triples to GOOS/GOARCH and archive shape.
switch ($Target) {
    'x86_64-unknown-linux-gnu'  { $goos = 'linux';   $goarch = 'amd64'; $binName = 'inat';     $archiveExt = 'tar.gz' }
    'aarch64-unknown-linux-gnu' { $goos = 'linux';   $goarch = 'arm64'; $binName = 'inat';     $archiveExt = 'tar.gz' }
    'x86_64-pc-windows-msvc'    { $goos = 'windows'; $goarch = 'amd64'; $binName = 'inat.exe'; $archiveExt = 'zip'    }
    'x86_64-pc-windows-gnu'     { $goos = 'windows'; $goarch = 'amd64'; $binName = 'inat.exe'; $archiveExt = 'zip'    }
    'x86_64-apple-darwin'       { $goos = 'darwin';  $goarch = 'amd64'; $binName = 'inat';     $archiveExt = 'tar.gz' }
    'aarch64-apple-darwin'      { $goos = 'darwin';  $goarch = 'arm64'; $binName = 'inat';     $archiveExt = 'tar.gz' }
    default {
        Write-Error "unsupported target triple '$Target'"
        exit 5
    }
}
$archive = "$stage.$archiveExt"

if (-not (Test-Path 'VERSION')) {
    Write-Error 'VERSION file not found at repo root'
    exit 2
}
$fileVersion = (Get-Content -Raw VERSION).Trim()
# A pre-release tag (vX.Y.Z-rc.1) builds the X.Y.Z tree.
if ($fileVersion -ne ($version -replace '-.*$', '')) {
    Write-Error "tag version ($version) != VERSION file ($fileVersion)"
    exit 2
}

Write-Host "building inat $version for $Target (GOOS=$goos GOARCH=$goarch)"

$binPath = Join-Path (Get-Location) 'build-tmp' $binName
New-Item -ItemType Directory -Force (Split-Path $binPath -Parent) | Out-Null

$env:CGO_ENABLED = '0'
$env:GOOS        = $goos
$env:GOARCH      = $goarch
Push-Location go
try {
    go build -trimpath -ldflags "-s -w -X main.version=$version" -o $binPath .
    if ($LASTEXITCODE -ne 0) {
        Write-Error 'go build failed'
        exit 3
    }
}
finally {
    Pop-Location
    Remove-Item env:CGO_ENABLED, env:GOOS, env:GOARCH -ErrorAction SilentlyContinue
}

if (-not (Test-Path $binPath)) {
    Write-Error "binary not found at $binPath"
    exit 4
}

if (Test-Path $stage) { Remove-Item $stage -Recurse -Force }
New-Item -ItemType Directory $stage | Out-Null

Copy-Item $binPath  (Join-Path $stage $binName)
Copy-Item LICENSE   (Join-Path $stage 'LICENSE')

@"
inat $version

A CLI kit manager for AI prompt and rule kits. Share skills and rules
across projects via git.

Homepage:      https://github.com/schubergphilis/inat
Documentation: see README.md in the repository
Licence:       Apache-2.0 (bundled as LICENSE alongside this file)
"@ | Set-Content -Encoding utf8 (Join-Path $stage 'README')

if (Test-Path $archive) { Remove-Item $archive -Force }
switch ($archiveExt) {
    'zip'    { Compress-Archive -Path $stage -DestinationPath $archive }
    'tar.gz' { tar -czf $archive $stage }
}
Remove-Item $stage    -Recurse -Force
Remove-Item build-tmp -Recurse -Force -ErrorAction SilentlyContinue

Write-Host "built $archive"
Get-Item $archive | Select-Object Name, Length
