param(
    [Parameter(Mandatory = $true)] [string] $OutputDirectory
)

$ErrorActionPreference = "Stop"
$root = Resolve-Path (Join-Path $PSScriptRoot "../..")
$lockPath = Join-Path $PSScriptRoot "windows-runtime.lock.json"
$lock = Get-Content -Raw $lockPath | ConvertFrom-Json
$output = [System.IO.Path]::GetFullPath($OutputDirectory)
$downloads = Join-Path $output ".downloads"

Remove-Item -Recurse -Force $output -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Force $downloads | Out-Null

function Get-LockedArtifact([string] $Name) {
    $artifact = $lock.artifacts.$Name
    $path = Join-Path $downloads $artifact.output
    Invoke-WebRequest -Uri $artifact.url -OutFile $path
    $actualSize = (Get-Item $path).Length
    if ($actualSize -ne [int64]$artifact.size) {
        throw "$Name size $actualSize does not match lock $($artifact.size)"
    }
    $actualHash = (Get-FileHash -Algorithm SHA256 $path).Hash.ToLowerInvariant()
    if ($actualHash -ne $artifact.sha256) {
        throw "$Name SHA-256 $actualHash does not match lock $($artifact.sha256)"
    }
    return $path
}

$ytDlp = Get-LockedArtifact "yt-dlp"
$galleryDL = Get-LockedArtifact "gallery-dl"
$denoArchive = Get-LockedArtifact "deno"
$ffmpegArchive = Get-LockedArtifact "ffmpeg"

Copy-Item $ytDlp (Join-Path $output "yt-dlp.exe")
Copy-Item $galleryDL (Join-Path $output "gallery-dl.exe")

$denoExtract = Join-Path $downloads "deno"
Expand-Archive -Path $denoArchive -DestinationPath $denoExtract
Copy-Item (Join-Path $denoExtract "deno.exe") (Join-Path $output "deno.exe")

$ffmpegExtract = Join-Path $downloads "ffmpeg"
Expand-Archive -Path $ffmpegArchive -DestinationPath $ffmpegExtract
$ffmpegBin = Get-ChildItem -Path $ffmpegExtract -Directory -Recurse | Where-Object { $_.Name -eq "bin" } | Select-Object -First 1
if (-not $ffmpegBin) {
    throw "FFmpeg archive did not contain a bin directory"
}
Copy-Item (Join-Path $ffmpegBin.FullName "ffmpeg.exe") $output
Copy-Item (Join-Path $ffmpegBin.FullName "ffprobe.exe") $output
Get-ChildItem -Path $ffmpegBin.FullName -Filter "*.dll" | Copy-Item -Destination $output

Copy-Item $lockPath (Join-Path $output "windows-runtime.lock.json")
@"
Igloo vendors these programs as separate executables. Their source and license
terms remain with their respective projects:

- yt-dlp: https://github.com/yt-dlp/yt-dlp
- gallery-dl: https://github.com/mikf/gallery-dl
- FFmpeg Windows builds: https://github.com/BtbN/FFmpeg-Builds
- Deno: https://github.com/denoland/deno

Exact versions and artifact hashes are recorded in windows-runtime.lock.json.
"@ | Set-Content -Encoding utf8 (Join-Path $output "THIRD-PARTY-NOTICES.txt")

Remove-Item -Recurse -Force $downloads
Write-Host "Windows runtime $($lock.revision) prepared at $output"
