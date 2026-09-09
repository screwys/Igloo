param(
    [Parameter(Mandatory = $true)] [string] $ProductVersion,
    [Parameter(Mandatory = $true)] [string] $AppDirectory,
    [Parameter(Mandatory = $true)] [string] $RuntimeDirectory,
    [Parameter(Mandatory = $true)] [string] $OutputDirectory
)

$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $true
$compiler = Join-Path $env:ProgramFiles 'Inno Setup 7\ISCC.exe'
$payload = Join-Path ([IO.Path]::GetTempPath()) ('igloo-installer-' + [Guid]::NewGuid())
try {
    New-Item -ItemType Directory -Force "$payload/app/current", "$payload/runtime/current", $OutputDirectory | Out-Null
    Copy-Item -Recurse "$AppDirectory/*" "$payload/app/current/"
    Copy-Item -Recurse "$RuntimeDirectory/*" "$payload/runtime/current/"
    # The tray stays outside the server directories rotated by automatic updates.
    $csharp = Join-Path $env:WINDIR 'Microsoft.NET\Framework64\v4.0.30319\csc.exe'
    $tray = Join-Path $payload 'igloo-tray.exe'
    $icon = Join-Path $PSScriptRoot 'InstallerIcon.ico'
    $source = Join-Path $PSScriptRoot 'IglooTray.cs'
    & $csharp /nologo /target:winexe /platform:x64 "/out:$tray" `
        /reference:System.Windows.Forms.dll /reference:System.Drawing.dll /reference:System.ServiceProcess.dll `
        "/win32icon:$icon" "/resource:$icon,Igloo.ico" $source
    if ($LASTEXITCODE -ne 0) { throw "Tray compiler failed: $LASTEXITCODE" }
    & $compiler "/DProductVersion=$ProductVersion" "/DPayloadDir=$payload" `
        "/O$([IO.Path]::GetFullPath($OutputDirectory))" "$PSScriptRoot/Igloo.iss"
    if ($LASTEXITCODE -ne 0) { throw "Inno Setup compiler failed: $LASTEXITCODE" }
} finally {
    Remove-Item -Recurse -Force $payload -ErrorAction SilentlyContinue
}
