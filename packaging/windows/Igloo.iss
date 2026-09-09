; Inno Setup owns the wizard, task selection, file installation, and uninstaller.
#ifndef ProductVersion
  #error ProductVersion is required
#endif
#ifndef PayloadDir
  #error PayloadDir is required
#endif

[Setup]
AppId={{8D22BFFA-EFA6-4D43-964F-F8826C7AEE71}
AppName=Igloo
AppVersion={#ProductVersion}
AppPublisher=Igloo
DefaultDirName={code:InstallDirectory}
DefaultGroupName=Igloo
DisableDirPage=no
DisableWelcomePage=no
LicenseFile=..\..\LICENSE
WizardStyle=modern
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
PrivilegesRequired=admin
SetupIconFile=InstallerIcon.ico
UninstallDisplayIcon={app}\app\current\igloo-launch.exe
OutputBaseFilename=IglooSetup-x64
Compression=lzma2
SolidCompression=yes
CloseApplications=yes
RestartApplications=no
SetupLogging=yes

[Tasks]
Name: "runsystem"; Description: "System service (starts with Windows)"; GroupDescription: "Run Igloo:"; Flags: exclusive
Name: "runuser"; Description: "At user login"; GroupDescription: "Run Igloo:"; Flags: exclusive unchecked
Name: "runmanual"; Description: "Only when I open Igloo"; GroupDescription: "Run Igloo:"; Flags: exclusive unchecked
Name: "updates"; Description: "Install updates automatically"; GroupDescription: "Additional tasks:"
Name: "desktopicon"; Description: "Create a desktop shortcut"; GroupDescription: "Additional tasks:"

[Dirs]
Name: "{app}"; Permissions: users-modify service-modify
Name: "{code:DataDirectory}"; Permissions: users-modify service-modify; Flags: uninsneveruninstall
Name: "{code:MediaDirectory}"; Permissions: users-modify service-modify; Flags: uninsneveruninstall
Name: "{code:ConfigDirectory}"; Permissions: users-modify service-modify; Flags: uninsneveruninstall

[Files]
Source: "{#PayloadDir}\app\current\*"; DestDir: "{app}\app\current"; Flags: ignoreversion recursesubdirs createallsubdirs
Source: "{#PayloadDir}\runtime\current\*"; DestDir: "{app}\runtime\current"; Flags: ignoreversion recursesubdirs createallsubdirs
Source: "installer-lifecycle.ps1"; DestDir: "{app}\setup"; Flags: ignoreversion
Source: "installer-lifecycle.ps1"; Flags: dontcopy

[Icons]
Name: "{group}\Igloo"; Filename: "{app}\app\current\igloo-launch.exe"
Name: "{commondesktop}\Igloo"; Filename: "{app}\app\current\igloo-launch.exe"; Tasks: desktopicon

[Registry]
Root: HKLM; Subkey: "SYSTEM\CurrentControlSet\Services\EventLog\Application\Igloo"; ValueType: expandsz; ValueName: "EventMessageFile"; ValueData: "{sys}\EventCreate.exe"; Flags: uninsdeletekey
Root: HKLM; Subkey: "SYSTEM\CurrentControlSet\Services\EventLog\Application\Igloo"; ValueType: dword; ValueName: "TypesSupported"; ValueData: "7"
Root: HKLM; Subkey: "Software\Igloo"; ValueType: string; ValueName: "InstallDirectory"; ValueData: "{app}"
Root: HKLM; Subkey: "Software\Igloo"; ValueType: string; ValueName: "DataDirectory"; ValueData: "{code:DataDirectory}"
Root: HKLM; Subkey: "Software\Igloo"; ValueType: string; ValueName: "MediaDirectory"; ValueData: "{code:MediaDirectory}"
Root: HKLM; Subkey: "Software\Igloo"; ValueType: string; ValueName: "ConfigDirectory"; ValueData: "{code:ConfigDirectory}"
Root: HKLM; Subkey: "Software\Igloo"; ValueType: dword; ValueName: "AutomaticUpdates"; ValueData: "{code:AutomaticUpdates}"
Root: HKCU; Subkey: "Software\Igloo"; ValueType: dword; ValueName: "DesktopShortcut"; ValueData: "{code:DesktopShortcut}"
Root: HKCU; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; ValueType: string; ValueName: "Igloo"; ValueData: """{app}\app\current\igloo-user.exe"""; Tasks: runuser; Flags: uninsdeletevalue
Root: HKCU; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; ValueName: "Igloo"; Tasks: not runuser; Flags: deletevalue
Root: HKLM; Subkey: "Software\Igloo"; ValueType: dword; ValueName: "RunMode"; ValueData: "{code:RunMode}"

[Run]
Filename: "{app}\app\current\igloo-launch.exe"; Description: "Open Igloo"; Flags: nowait postinstall skipifsilent runasoriginaluser; Check: ConfigurationSucceeded

[Code]
var
  StoragePage: TInputDirWizardPage;
  SavedConfigDirectory: String;
  UninstallMode: Integer;
  ConfigurationError: String;

function ConfigurationSucceeded: Boolean;
begin
  Result := ConfigurationError = '';
end;

function InstallSetting(Name, Default: String): String;
begin
  if not RegQueryStringValue(HKLM64, 'Software\Igloo', Name, Result) then
    Result := Default;
end;

function InstallDirectory(Param: String): String;
begin
  Result := InstallSetting('InstallDirectory', ExpandConstant('{autopf}\Igloo'));
end;

function DataDirectory(Param: String): String;
begin
  Result := StoragePage.Values[0];
end;

function MediaDirectory(Param: String): String;
begin
  Result := StoragePage.Values[1];
end;

function ConfigDirectory(Param: String): String;
begin
  Result := SavedConfigDirectory;
end;

function RunMode(Param: String): String;
begin
  Result := '0';
  if WizardIsTaskSelected('runuser') then Result := '1';
  if WizardIsTaskSelected('runmanual') then Result := '2';
end;

function AutomaticUpdates(Param: String): String;
begin
  Result := '0';
  if WizardIsTaskSelected('updates') then Result := '1';
end;

function DesktopShortcut(Param: String): String;
begin
  Result := '0';
  if WizardIsTaskSelected('desktopicon') then Result := '1';
end;

procedure InitializeWizard;
var
  Mode, Updates, Desktop: Cardinal;
  I: Integer;
  TaskOverride: Boolean;
  MergeTasks: String;
begin
  StoragePage := CreateInputDirPage(wpSelectDir, 'Storage folders',
    'Where should Igloo store your data and media?',
    'Use a fast disk for the data folder. Media can be stored on a slower disk.',
    False, '');
  StoragePage.Add('Data folder:');
  StoragePage.Add('Media folder:');
  StoragePage.Values[0] := ExpandConstant('{param:DATADIR|' +
    InstallSetting('DataDirectory', ExpandConstant('{commonappdata}\Igloo\data')) + '}');
  StoragePage.Values[1] := ExpandConstant('{param:MEDIADIR|' +
    InstallSetting('MediaDirectory', ExpandConstant('{commonappdata}\Igloo\media')) + '}');
  SavedConfigDirectory := InstallSetting('ConfigDirectory', ExpandConstant('{commonappdata}\Igloo\config'));
  { Inno remembers its tasks itself. Import saved choices when it has no previous installation. }
  TaskOverride := False;
  for I := 1 to ParamCount do
  begin
    if Pos('/TASKS=', Uppercase(ParamStr(I))) = 1 then TaskOverride := True;
    if Pos('/MERGETASKS=', Uppercase(ParamStr(I))) = 1 then
      MergeTasks := RemoveQuotes(Copy(ParamStr(I), Length('/MERGETASKS=') + 1, Length(ParamStr(I))));
  end;
  if not TaskOverride and not RegKeyExists(HKLM64, 'Software\Microsoft\Windows\CurrentVersion\Uninstall\{8D22BFFA-EFA6-4D43-964F-F8826C7AEE71}_is1') then
  begin
    if RegQueryDWordValue(HKLM64, 'Software\Igloo', 'RunMode', Mode) then
    begin
      if Mode = 1 then WizardSelectTasks('runuser');
      if Mode = 2 then WizardSelectTasks('runmanual');
      if not RegQueryDWordValue(HKCU, 'Software\Igloo', 'DesktopShortcut', Desktop) or (Desktop = 0) then
        WizardSelectTasks('!desktopicon');
    end;
    if RegQueryDWordValue(HKLM64, 'Software\Igloo', 'AutomaticUpdates', Updates) and (Updates = 0) then
      WizardSelectTasks('!updates');
  end;
  if MergeTasks <> '' then WizardSelectTasks(MergeTasks);
end;

function UpdateReadyMemo(Space, NewLine, MemoUserInfoInfo, MemoDirInfo,
  MemoTypeInfo, MemoComponentsInfo, MemoGroupInfo, MemoTasksInfo: String): String;
begin
  Result := MemoDirInfo + NewLine + NewLine + MemoGroupInfo + NewLine + NewLine + 'Storage folders:' + NewLine +
    Space + DataDirectory('') + NewLine + Space + MediaDirectory('') +
    NewLine + NewLine + MemoTasksInfo;
end;

function RunLifecycle(Script, Action, Mode: String): Boolean;
var
  ExitCode: Integer;
begin
  Result := Exec(ExpandConstant('{sys}\WindowsPowerShell\v1.0\powershell.exe'),
    '-NoProfile -NonInteractive -ExecutionPolicy Bypass -File "' + Script +
    '" -Action ' + Action + ' -InstallDirectory "' + ExpandConstant('{app}') +
    '" -RunMode ' + Mode, '', SW_HIDE, ewWaitUntilTerminated, ExitCode);
  Result := Result and (ExitCode = 0);
  if not Result then Log(Format('Installer lifecycle %s failed (%d).', [Action, ExitCode]));
end;

function PrepareToInstall(var NeedsRestart: Boolean): String;
begin
  ExtractTemporaryFile('installer-lifecycle.ps1');
  if not RunLifecycle(ExpandConstant('{tmp}\installer-lifecycle.ps1'), 'Prepare', RunMode('')) then
    Result := 'Could not stop the existing Igloo installation. See the Igloo installer lifecycle log in your temporary folder.';
end;

procedure ConfigureInstallation;
begin
  if not SaveStringToFile(AddBackslash(DataDirectory('')) + '.igloo-state-root', '', False) or
     not SaveStringToFile(AddBackslash(MediaDirectory('')) + '.igloo-media-root', '', False) or
     not SaveStringToFile(AddBackslash(ConfigDirectory('')) + '.igloo-config-root', '', False) then
    RaiseException('Could not write Igloo storage markers.');
  if not WizardIsTaskSelected('desktopicon') then
    DeleteFile(ExpandConstant('{commondesktop}\Igloo.lnk'));
  if not RunLifecycle(ExpandConstant('{app}\setup\installer-lifecycle.ps1'), 'Install', RunMode('')) then
    RaiseException('Could not configure Igloo. See the Igloo installer lifecycle log in your temporary folder.');
end;

procedure CurStepChanged(CurStep: TSetupStep);
begin
  if CurStep = ssPostInstall then
  begin
    try
      ConfigureInstallation;
    except
      ConfigurationError := GetExceptionMessage;
      Log(ConfigurationError);
    end;
  end;
end;

procedure CurPageChanged(CurPageID: Integer);
begin
  if (CurPageID = wpFinished) and not ConfigurationSucceeded then
  begin
    WizardForm.FinishedHeadingLabel.Caption := 'Igloo setup could not finish';
    WizardForm.FinishedLabel.Caption := ConfigurationError + #13#10 + #13#10 +
      'Correct the problem and run Setup again, or uninstall Igloo from Windows Settings.';
  end;
end;

function GetCustomSetupExitCode: Integer;
begin
  Result := 0;
  if not ConfigurationSucceeded then Result := 1;
end;

function InitializeUninstall: Boolean;
var
  Choice: Integer;
begin
  UninstallMode := StrToIntDef(ExpandConstant('{param:UNINSTALLMODE|0}'), -1);
  Result := (UninstallMode >= 0) and (UninstallMode <= 2);
  if not Result or UninstallSilent then exit;
  Choice := TaskDialogMsgBox('Uninstall Igloo', 'Keep your data and settings for a future installation?',
    mbConfirmation, MB_YESNOCANCEL, ['Keep data and settings', 'Choose data to remove'], 0);
  Result := Choice <> IDCANCEL;
  if Choice = IDNO then
  begin
    Choice := TaskDialogMsgBox('Remove Igloo data', 'Deleted files cannot be recovered by Igloo.',
      mbConfirmation, MB_YESNOCANCEL, ['Remove application data' + #13#10 + 'Keep media, settings, and uploaded cookies.',
       'Remove everything' + #13#10 + 'Delete data, media, settings, and uploaded cookies.'], 0);
    Result := Choice <> IDCANCEL;
    if Choice = IDYES then UninstallMode := 1;
    if Choice = IDNO then UninstallMode := 2;
  end else UninstallMode := 0;
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
var
  ExitCode: Integer;
begin
  if CurUninstallStep = usUninstall then
  begin
    if not RunLifecycle(ExpandConstant('{app}\setup\installer-lifecycle.ps1'), 'Uninstall', '0') then
      RaiseException('Could not stop Igloo. Uninstall has been stopped.');
    if UninstallMode <> 0 then
      if not Exec(ExpandConstant('{app}\app\current\igloo-uninstall.exe'),
        IntToStr(UninstallMode), '', SW_HIDE, ewWaitUntilTerminated, ExitCode) or (ExitCode <> 0) then
        RaiseException('Could not remove the selected Igloo data. Uninstall has been stopped.');
  end;
end;
