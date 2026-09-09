param(
    [Parameter(Mandatory = $true)] [ValidateSet('Prepare', 'Install', 'Uninstall')] [string] $Action,
    [Parameter(Mandatory = $true)] [string] $InstallDirectory,
    [ValidateRange(0, 2)] [int] $RunMode = 0
)

$ErrorActionPreference = 'Stop'
$createdService = $false
Start-Transcript -Path (Join-Path $env:TEMP 'igloo-installer-lifecycle.log') -Append | Out-Null

function Invoke-CheckedProcess([string] $File, [string] $Arguments) {
    $process = Start-Process -FilePath $File -ArgumentList $Arguments -Wait -PassThru -NoNewWindow
    if ($process.ExitCode -eq 3010) { throw 'Restart Windows to finish removing the previous installation, then run setup again.' }
    if ($process.ExitCode -ne 0) {
        throw "$File exited with code $($process.ExitCode)"
    }
}

function Stop-Igloo {
    $service = Get-Service -Name Igloo -ErrorAction SilentlyContinue
    if ($service) {
        if ($service.Status -ne 'Stopped') {
            Stop-Service -InputObject $service
            $service.WaitForStatus('Stopped', [TimeSpan]::FromSeconds(60))
        }
        $service.Dispose()
    }
    $roots = @($InstallDirectory)
    $settings = Get-ItemProperty 'HKLM:\Software\Igloo' -ErrorAction SilentlyContinue
    if ($settings.InstallDirectory) { $roots += $settings.InstallDirectory }
    Get-CimInstance Win32_Process -Filter "Name = 'igloo.exe' OR Name = 'igloo-user.exe' OR Name = 'igloo-update.exe' OR Name = 'igloo-tray.exe'" |
        Where-Object {
            $path = $_.ExecutablePath
            $path -and ($roots | Where-Object { $path.StartsWith($_.TrimEnd('\') + '\', [StringComparison]::OrdinalIgnoreCase) })
        } | ForEach-Object {
            $process = Get-Process -Id $_.ProcessId -ErrorAction SilentlyContinue
            if ($process) {
                $process | Stop-Process -Force
                $process.WaitForExit()
                $process.Dispose()
            }
        }
}

function Remove-LegacyInstaller {
    # Remove the old Burn owner before Inno takes ownership of the same files.
    foreach ($view in @('HKLM:\Software', 'HKLM:\Software\WOW6432Node')) {
        $key = "$view\Microsoft\Windows\CurrentVersion\Uninstall"
        if (-not (Test-Path $key)) { continue }
        $entries = Get-ChildItem $key | Get-ItemProperty
        foreach ($entry in $entries) {
            if ($entry.BundleUpgradeCode -contains '{1CC6AA3F-B763-48AE-BF8F-C67B70EB823D}') {
                Invoke-CheckedProcess $entry.BundleCachePath '/uninstall /quiet /norestart UNINSTALLMODE=0'
            }
        }
    }
    # Also cover an installation made directly from the old MSI.
    $installer = New-Object -ComObject WindowsInstaller.Installer
    foreach ($product in @($installer.RelatedProducts('{A8E3D470-7E57-49C3-9287-1CF0FC36D4CA}'))) {
        Invoke-CheckedProcess "$env:SystemRoot\System32\msiexec.exe" "/x $product /qn /norestart UNINSTALLMODE=0"
    }
}

try {
    if ($Action -eq 'Prepare') {
        Stop-Igloo
        Remove-LegacyInstaller
    } elseif ($Action -eq 'Install') {
        $service = Get-CimInstance Win32_Service -Filter "Name = 'Igloo'"
        if ($RunMode -eq 0) {
            $settings = @{
                PathName = '"' + (Join-Path $InstallDirectory 'app\current\igloo.exe') + '"'
                StartMode = 'Automatic'
                StartName = 'NT AUTHORITY\LocalService'
            }
            if ($service) {
                $result = Invoke-CimMethod -InputObject $service -MethodName Change -Arguments $settings
            } else {
                $settings.Name = 'Igloo'
                $settings.DisplayName = 'Igloo'
                $settings.ServiceType = [byte]16
                $settings.ErrorControl = [byte]1
                $result = Invoke-CimMethod -ClassName Win32_Service -MethodName Create -Arguments $settings
            }
            if ($result.ReturnValue -ne 0) { throw "Service configuration failed: $($result.ReturnValue)" }
            $createdService = -not $service
            Invoke-CheckedProcess "$env:SystemRoot\System32\sc.exe" 'failure Igloo reset= 86400 actions= restart/10000/restart/10000/restart/10000'
            Invoke-CheckedProcess "$env:SystemRoot\System32\sc.exe" 'sdset Igloo D:(A;;CCLCSWRPWPDTLOCRRC;;;SY)(A;;CCDCLCSWRPWPDTLOCRSDRCWDWO;;;BA)(A;;LCRPWP;;;LS)(A;;LCRPWP;;;BU)'
        } elseif ($service) {
            $result = Invoke-CimMethod -InputObject $service -MethodName Delete
            if ($result.ReturnValue -ne 0) { throw "Service removal failed: $($result.ReturnValue)" }
        }
        foreach ($protocol in @('TCP', 'UDP')) {
            Get-NetFirewallRule -Name "Igloo-$protocol" -ErrorAction SilentlyContinue | Remove-NetFirewallRule
            New-NetFirewallRule -Name "Igloo-$protocol" -DisplayName "Igloo private network $protocol" `
                -Direction Inbound -Action Allow -Protocol $protocol -LocalPort 5001 `
                -Profile Private -RemoteAddress LocalSubnet | Out-Null
        }
        if ($RunMode -eq 0) { Start-Service -Name Igloo }
    } else {
        Stop-Igloo
        $service = Get-CimInstance Win32_Service -Filter "Name = 'Igloo'"
        if ($service) {
            $result = Invoke-CimMethod -InputObject $service -MethodName Delete
            if ($result.ReturnValue -ne 0) { throw "Service removal failed: $($result.ReturnValue)" }
        }
        Get-NetFirewallRule -Name 'Igloo-TCP', 'Igloo-UDP' -ErrorAction SilentlyContinue | Remove-NetFirewallRule
    }
} catch {
    Write-Error $_ -ErrorAction Continue
    if ($Action -eq 'Install') {
        Get-CimInstance Win32_Service -Filter "Name = 'Igloo'" |
            Format-List Name, State, StartName, PathName, ExitCode, ServiceSpecificExitCode
        Get-WinEvent -FilterHashtable @{LogName = 'System'; StartTime = (Get-Date).AddMinutes(-2)} -ErrorAction SilentlyContinue |
            Where-Object { $_.ProviderName -eq 'Service Control Manager' -and $_.Message -match '\bIgloo\b' } |
            Format-List TimeCreated, Id, Message
        Get-WinEvent -FilterHashtable @{LogName = 'Application'; ProviderName = 'Igloo'; StartTime = (Get-Date).AddMinutes(-2)} -ErrorAction SilentlyContinue |
            Format-List TimeCreated, Id, Message
        $settings = Get-ItemProperty 'HKLM:\Software\Igloo' -ErrorAction SilentlyContinue
        if ($settings.DataDirectory) {
            Get-Content (Join-Path $settings.DataDirectory 'logs\server\server.log') -Tail 80 -ErrorAction SilentlyContinue
        }
        Stop-Igloo
        if ($createdService) {
            $service = Get-CimInstance Win32_Service -Filter "Name = 'Igloo'"
            $result = Invoke-CimMethod -InputObject $service -MethodName Delete
            if ($result.ReturnValue -ne 0) { Write-Warning "Could not remove failed service: $($result.ReturnValue)" }
        }
    }
    exit 1
} finally {
    Stop-Transcript | Out-Null
}
