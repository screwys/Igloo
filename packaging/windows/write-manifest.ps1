param(
    [string] $AppVersion,
    [Parameter(Mandatory = $true)] [string] $RuntimeVersion,
    [Parameter(Mandatory = $true)] [string] $MinimumAppVersion,
    [string] $AppArchive,
    [Parameter(Mandatory = $true)] [string] $RuntimeArchive,
    [Parameter(Mandatory = $true)] [string] $OutputPath
)

$ErrorActionPreference = "Stop"
function Payload([string] $Version, [string] $Path) {
    return [ordered]@{
        version = $Version
        asset = (Split-Path -Leaf $Path)
        sha256 = (Get-FileHash -Algorithm SHA256 $Path).Hash.ToLowerInvariant()
        size = (Get-Item $Path).Length
    }
}

$manifest = [ordered]@{
    schema = 1
    os = "windows"
    arch = "amd64"
    minimum_app_version = $MinimumAppVersion
    runtime = Payload $RuntimeVersion $RuntimeArchive
}
if ($AppArchive) {
    if (-not $AppVersion) { throw "AppVersion is required when AppArchive is provided" }
    $manifest["app"] = Payload $AppVersion $AppArchive
}
$manifest | ConvertTo-Json -Depth 4 | Set-Content -Encoding utf8NoBOM $OutputPath
