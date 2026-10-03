param(
    [Parameter(Mandatory = $true)] [string] $OutputDirectory,
    [string] $VCRuntimeDirectory
)

$ErrorActionPreference = "Stop"
$PSNativeCommandUseErrorActionPreference = $true
$root = Resolve-Path (Join-Path $PSScriptRoot "../..")
$lockPath = Join-Path $PSScriptRoot "windows-runtime.lock.json"
$lock = Get-Content -Raw $lockPath | ConvertFrom-Json
$output = [System.IO.Path]::GetFullPath($OutputDirectory)
$downloads = Join-Path $output ".downloads"

if (-not $VCRuntimeDirectory) {
    $vswhere = Join-Path ${env:ProgramFiles(x86)} "Microsoft Visual Studio/Installer/vswhere.exe"
    if (-not (Test-Path $vswhere)) { throw "Visual Studio C++ is required, or set VCRuntimeDirectory to its x64 redistributable folder" }
    $visualStudio = & $vswhere -latest -products '*' -requires Microsoft.VisualStudio.Component.VC.Tools.x86.x64 -property installationPath
    if (-not $visualStudio) { throw "Visual Studio C++ redistributable files are unavailable" }
    $crt = Get-ChildItem -Path "$visualStudio/VC/Redist/MSVC/*/x64/Microsoft.VC*.CRT" -Directory |
        Sort-Object { [version]$_.Parent.Parent.Name } -Descending | Select-Object -First 1
    if (-not $crt) { throw "Visual Studio did not contain an x64 C++ runtime" }
    $VCRuntimeDirectory = $crt.FullName
}
foreach ($name in @("msvcp140.dll", "vcruntime140.dll", "vcruntime140_1.dll")) {
    if (-not (Test-Path (Join-Path $VCRuntimeDirectory $name))) {
        throw "Visual C++ runtime folder is missing $name"
    }
}

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
$postgresArchive = Get-LockedArtifact "postgresql"

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

$postgresExtract = Join-Path $downloads "postgresql"
Expand-Archive -Path $postgresArchive -DestinationPath $postgresExtract
$postgresRoot = Join-Path $postgresExtract "pgsql"
$postgresOutput = Join-Path $output "postgresql"
New-Item -ItemType Directory -Force $postgresOutput | Out-Null
foreach ($directory in @("bin", "lib", "share")) {
    Copy-Item -Recurse (Join-Path $postgresRoot $directory) $postgresOutput
}
Copy-Item (Join-Path $postgresRoot "server_license.txt") $postgresOutput
Copy-Item (Join-Path $postgresRoot "commandlinetools_3rd_party_licenses.txt") $postgresOutput

$postgresBin = Join-Path $postgresOutput "bin"
Get-ChildItem -Path $VCRuntimeDirectory -Filter "*.dll" | Copy-Item -Destination $postgresBin

Copy-Item $lockPath (Join-Path $output "windows-runtime.lock.json")
@"
Igloo vendors these programs as separate executables. Their source and license
terms remain with their respective projects:

- yt-dlp: https://github.com/yt-dlp/yt-dlp
- gallery-dl: https://github.com/mikf/gallery-dl
- FFmpeg Windows builds: https://github.com/BtbN/FFmpeg-Builds
- Deno: https://github.com/denoland/deno
- PostgreSQL: https://www.postgresql.org/ and https://www.enterprisedb.com/download-postgresql-binaries
- Microsoft Visual C++ runtime: https://learn.microsoft.com/en-us/cpp/windows/redistributing-visual-cpp-files

Exact versions and artifact hashes are recorded in windows-runtime.lock.json.
"@ | Set-Content -Encoding utf8 (Join-Path $output "THIRD-PARTY-NOTICES.txt")

Remove-Item -Recurse -Force $downloads
Write-Host "Windows runtime $($lock.revision) prepared at $output"
