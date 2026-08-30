param(
    [Parameter(Mandatory = $true)] [string] $Version,
    [Parameter(Mandatory = $true)] [string] $RuntimeRevision,
    [Parameter(Mandatory = $true)] [string] $UpdatePublicKey,
    [Parameter(Mandatory = $true)] [string] $OutputDirectory
)

$ErrorActionPreference = "Stop"
$root = Resolve-Path (Join-Path $PSScriptRoot "../..")
$output = [System.IO.Path]::GetFullPath($OutputDirectory)
Remove-Item -Recurse -Force $output -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Force $output | Out-Null

$ldflags = @(
    "-s", "-w",
    "-X", "github.com/screwys/igloo/internal/buildinfo.version=$Version",
    "-X", "github.com/screwys/igloo/internal/buildinfo.bundleRevision=$RuntimeRevision",
    "-X", "github.com/screwys/igloo/internal/windowsupdate.publicKeyBase64=$UpdatePublicKey"
) -join " "

Push-Location $root
try {
    $env:CGO_ENABLED = "0"
    $env:GOOS = "windows"
    $env:GOARCH = "amd64"
    go build -trimpath -ldflags $ldflags -o (Join-Path $output "igloo.exe") ./cmd/igloo
    go build -trimpath -ldflags "-s -w" -o (Join-Path $output "igloo-update.exe") ./cmd/igloo-update

    Copy-Item -Recurse static (Join-Path $output "static")
    Remove-Item -Recurse -Force (Join-Path $output "static/screenshots") -ErrorAction SilentlyContinue
    Remove-Item -Recurse -Force (Join-Path $output "static/js/src") -ErrorAction SilentlyContinue
    Get-ChildItem -Path (Join-Path $output "static") -Recurse -Include "*.test.mjs", "*.map" | Remove-Item -Force
    Copy-Item -Recurse locales (Join-Path $output "locales")
    Copy-Item LICENSE (Join-Path $output "LICENSE.txt")
    @{
        version = $Version
        runtime_revision = $RuntimeRevision
        os = "windows"
        arch = "amd64"
    } | ConvertTo-Json | Set-Content -Encoding utf8 (Join-Path $output "bundle.json")
} finally {
    Pop-Location
}

Write-Host "Igloo Windows application $Version prepared at $output"
