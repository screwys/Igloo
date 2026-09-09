param([Parameter(Mandatory = $true)] [string] $Installer)

# Runs on a disposable Windows runner: installs real payloads and starts the service.
$ErrorActionPreference = 'Stop'
$installerPath = (Resolve-Path $Installer).Path
$settingsKey = 'HKLM:\Software\Igloo'
$config = Join-Path $env:ProgramData 'Igloo\config'
if ((Test-Path $settingsKey) -or (Test-Path $config) -or (Get-Service Igloo -ErrorAction SilentlyContinue)) {
    throw 'Installer tests require a Windows runner without an existing Igloo installation.'
}
# LocalService must be able to resolve ancestors of its data directory.
$root = Join-Path $env:ProgramData ('igloo-installer-test-' + [Guid]::NewGuid())
$privateRoot = Join-Path $env:TEMP ('igloo-installer-denied-' + [Guid]::NewGuid())
$app = Join-Path $root 'application'
$data = Join-Path $root 'data'
$media = Join-Path $data 'media'
New-Item -ItemType Directory -Force $root | Out-Null

function Assert([bool] $Condition, [string] $Message) {
    if (-not $Condition) { throw $Message }
}

function Run-Setup([string] $Tasks) {
    $arguments = "/VERYSILENT /SUPPRESSMSGBOXES /NORESTART /LOG=`"$root\setup.log`" /DIR=`"$app`""
    if ($Tasks) { $arguments += " /TASKS=`"$Tasks`"" }
    if (-not (Test-Path $settingsKey)) { $arguments += " /DATADIR=`"$data`" /MEDIADIR=`"$media`"" }
    $process = Start-Process $installerPath -ArgumentList $arguments -Wait -PassThru
    Assert ($process.ExitCode -eq 0) "Setup failed: $($process.ExitCode)"
    $settings = Get-ItemProperty $settingsKey
    Assert ($settings.DataDirectory -eq $data) 'Data folder did not persist.'
    Assert ($settings.MediaDirectory -eq $media) 'Media folder did not persist.'
    Assert (Test-Path "$app\app\current\igloo.exe") 'Application layout is missing.'
    Assert (Test-Path "$app\runtime\current\ffmpeg.exe") 'Runtime layout is missing.'
}

function Run-Uninstall([int] $Mode) {
    $process = Start-Process "$app\unins000.exe" -ArgumentList "/VERYSILENT /SUPPRESSMSGBOXES /NORESTART /UNINSTALLMODE=$Mode" -Wait -PassThru
    Assert ($process.ExitCode -eq 0) "Uninstall failed: $($process.ExitCode)"
    Assert (-not (Test-Path "$app\app\current\igloo.exe")) 'Uninstall left the server executable.'
    Assert (-not (Get-Service Igloo -ErrorAction SilentlyContinue)) 'Uninstall left the service.'
    Assert (-not (Get-NetFirewallRule -Name 'Igloo-TCP', 'Igloo-UDP' -ErrorAction SilentlyContinue)) 'Uninstall left firewall rules.'
}

function Wait-Healthy {
    $deadline = (Get-Date).AddSeconds(60)
    do {
        try {
            $response = Invoke-WebRequest 'http://127.0.0.1:5001/api/health/live' -UseBasicParsing -TimeoutSec 2
            if ($response.StatusCode -eq 200) { return }
        } catch { Start-Sleep -Milliseconds 500 }
    } while ((Get-Date) -lt $deadline)
    throw 'Tray server did not become healthy.'
}

try {
    $process = Start-Process $installerPath -ArgumentList (
        "/VERYSILENT /SUPPRESSMSGBOXES /NORESTART /LOG=`"$root\setup.log`" /DIR=`"$app`" " +
        "/DATADIR=`"$privateRoot\data`" /MEDIADIR=`"$privateRoot\media`" /TASKS=runsystem"
    ) -Wait -PassThru
    Assert ($process.ExitCode -eq 1) 'Service configuration failure was not reported by setup.'
    $startupError = Get-WinEvent -FilterHashtable @{LogName = 'Application'; ProviderName = 'Igloo'} -MaxEvents 1
    Assert ($startupError.Message -match 'Access is denied') 'The denied-access fixture failed for an unexpected reason.'
    Run-Uninstall 2
    Remove-Item $settingsKey -Recurse -Force

    Run-Setup 'desktopicon'
    Assert ((Get-ItemProperty $settingsKey).RunMode -eq 2) 'Basic installation was not selected.'
    Assert ((Get-ItemProperty $settingsKey).AutomaticUpdates -eq 0) 'Unchecked updates were enabled.'
    Assert (-not (Get-Service Igloo -ErrorAction SilentlyContinue)) 'Basic installation installed a service.'
    Assert (Test-Path "$env:PUBLIC\Desktop\Igloo.lnk") 'Desktop shortcut is missing.'
    $shortcut = (New-Object -ComObject WScript.Shell).CreateShortcut("$env:PUBLIC\Desktop\Igloo.lnk")
    Assert ($shortcut.TargetPath -eq "$app\igloo-tray.exe") 'Shortcut does not open the tray.'
    # Load the same controller used by tray menu actions without locking the installed executable.
    Add-Type -AssemblyName System.Windows.Forms, System.Drawing, System.ServiceProcess
    $assembly = [Reflection.Assembly]::Load([IO.File]::ReadAllBytes("$app\igloo-tray.exe"))
    $icon = $assembly.GetManifestResourceStream('Igloo.ico')
    Assert ($null -ne $icon -and $icon.Length -gt 0) 'Igloo tray icon is missing.'
    $icon.Dispose()
    $controller = $assembly.CreateInstance('Igloo.Windows.ServerController')
    $tray = Start-Process "$app\igloo-tray.exe" -ArgumentList '--background' -PassThru
    Wait-Healthy
    Assert (-not $tray.HasExited) 'Tray exited after starting the server.'
    $duplicate = Start-Process "$app\igloo-tray.exe" -ArgumentList '--background' -PassThru -Wait
    Assert ($duplicate.ExitCode -eq 0) 'Opening the tray twice failed.'
    Assert (@(Get-Process igloo-user).Count -eq 1) 'Opening the tray twice started duplicate servers.'
    $server = Get-Process igloo-user
    $null = $server.Handle
    $controller.Stop()
    Assert ($server.HasExited -and $server.ExitCode -eq 0) 'Tray stop did not shut down the server cleanly.'
    $server.Dispose()
    Move-Item "$app\app\current" "$app\app\previous"
    Move-Item "$app\app\previous" "$app\app\current"
    Assert (-not $tray.HasExited) 'Tray could not remain open while server files rotated.'
    $controller.Start()
    Wait-Healthy
    $controller.StartAtLogin = $false
    Assert (-not $controller.StartAtLogin) 'Tray could not disable login startup.'
    $controller.Stop()
    foreach ($folder in @($data, $media, $config)) { Set-Content "$folder\saved.txt" 'saved content' }
    Run-Uninstall 0
    foreach ($folder in @($data, $media, $config)) { Assert (Test-Path "$folder\saved.txt") 'Default uninstall deleted saved content.' }

    # Import the login preference from an installation made before the tray existed.
    Set-ItemProperty $settingsKey RunMode 1
    Set-ItemProperty 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run' Igloo "`"$app\app\current\igloo-user.exe`""
    Run-Setup 'updates'
    $startup = Get-ItemProperty 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run'
    Assert ($startup.Igloo -eq "`"$app\igloo-tray.exe`" --background") 'Login startup did not migrate to the tray.'
    Assert ((Get-ItemProperty $settingsKey).AutomaticUpdates -eq 1) 'Updates selection was lost.'
    Assert (-not (Test-Path "$env:PUBLIC\Desktop\Igloo.lnk")) 'Unchecked desktop shortcut remains.'
    Run-Setup ''
    Assert ((Get-ItemProperty $settingsKey).RunMode -eq 2) 'Reinstall forgot the basic installation.'

    Run-Setup 'runsystem'
    $service = Get-CimInstance Win32_Service -Filter "Name = 'Igloo'"
    Assert ($service.State -eq 'Running') 'System service did not start.'
    Assert ($service.StartName -eq 'NT AUTHORITY\LocalService') 'Wrong service account.'
    Assert ($service.StartMode -eq 'Auto') 'Service is not automatic.'
    $controller = $assembly.CreateInstance('Igloo.Windows.ServerController')
    Assert ($controller.ServiceMode) 'Tray did not select the installed service.'
    $controller.Stop()
    Assert ((Get-Service Igloo).Status -eq 'Stopped') 'Tray did not stop the service.'
    $controller.Start()
    Wait-Healthy
    Assert (-not (Get-Process igloo-user -ErrorAction SilentlyContinue)) 'Service tray started a second user server.'
    Run-Uninstall 1
    Assert (-not (Test-Path "$data\saved.txt")) 'Data-only uninstall retained application data.'
    Assert (Test-Path "$media\saved.txt") 'Data-only uninstall deleted nested media.'
    Assert (Test-Path "$config\saved.txt") 'Data-only uninstall deleted settings.'

    Run-Setup '!runsystem'
    Run-Uninstall 2
    foreach ($folder in @($data, $media, $config)) { Assert (-not (Test-Path $folder)) 'Remove-everything retained a storage root.' }
    Write-Host 'Installer install, reconfigure, service, and retention checks passed.'
} catch {
    Get-Content "$root\setup.log", "$env:TEMP\igloo-installer-lifecycle.log" -ErrorAction SilentlyContinue
    throw
} finally {
    if (Test-Path "$app\unins000.exe") { Run-Uninstall 2 }
    Remove-Item $settingsKey, 'HKCU:\Software\Igloo' -Recurse -Force -ErrorAction SilentlyContinue
    Remove-Item $root, $privateRoot -Recurse -Force -ErrorAction SilentlyContinue
}
