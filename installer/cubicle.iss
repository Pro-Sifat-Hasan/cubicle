; Cubicle Windows installer (Inno Setup 6).
; Built by scripts/build.ps1:  ISCC /DAppVersion=0.2.0 /DStageDir=..\build\package\stage installer\cubicle.iss
;
; Per-user install: no administrator prompt, installs in seconds, and the
; office runs under your own account (your data stays in %LOCALAPPDATA%\Cubicle).

#ifndef AppVersion
  #define AppVersion "0.2.0"
#endif
#ifndef StageDir
  #define StageDir "..\build\package\stage"
#endif

#define AppName "Cubicle"
#define AppExe "Cubicle.exe"
#define AppUrl "https://github.com/Pro-Sifat-Hasan/cubicle"

[Setup]
AppId={{6B1C8E2A-4C7D-4F39-9C3B-2E8A1D5F7C10}
AppName={#AppName}
AppVersion={#AppVersion}
AppVerName={#AppName} {#AppVersion}
AppPublisher=Cubicle contributors
AppPublisherURL={#AppUrl}
AppSupportURL={#AppUrl}/issues
AppUpdatesURL={#AppUrl}/releases
DefaultDirName={localappdata}\Programs\{#AppName}
DefaultGroupName={#AppName}
DisableProgramGroupPage=yes
DisableDirPage=auto
PrivilegesRequired=lowest
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
OutputBaseFilename=Cubicle-Setup-{#AppVersion}
SetupIconFile=..\desktop\src\Cubicle.Desktop\Assets\cubicle.ico
UninstallDisplayIcon={app}\{#AppExe}
LicenseFile=..\LICENSE
Compression=lzma2/ultra64
SolidCompression=yes
WizardStyle=modern
CloseApplications=force
RestartApplications=no
MinVersion=10.0.17763

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "autostart"; Description: "Start Cubicle when I sign in (recommended, so routines run every day)"; GroupDescription: "Background:"
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"; Flags: unchecked

[Files]
Source: "{#StageDir}\*"; DestDir: "{app}"; Flags: ignoreversion recursesubdirs createallsubdirs

[Icons]
Name: "{group}\{#AppName}"; Filename: "{app}\{#AppExe}"
Name: "{group}\Uninstall {#AppName}"; Filename: "{uninstallexe}"
Name: "{userdesktop}\{#AppName}"; Filename: "{app}\{#AppExe}"; Tasks: desktopicon

[Registry]
Root: HKCU; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; ValueType: string; ValueName: "Cubicle"; \
    ValueData: """{app}\{#AppExe}"" --background"; Flags: uninsdeletevalue; Tasks: autostart

[Run]
Filename: "{app}\{#AppExe}"; Description: "Open Cubicle now"; Flags: nowait postinstall skipifsilent

[Code]
// Stops Cubicle processes running from this install folder only, so a portable
// copy or a development build elsewhere keeps running.
procedure StopOfficeIn(Dir: String);
var
  ResultCode: Integer;
begin
  Exec(ExpandConstant('{sys}\WindowsPowerShell\v1.0\powershell.exe'),
    '-NoProfile -ExecutionPolicy Bypass -Command "Get-Process Cubicle,cubicle-gateway,cubicle-agents -ErrorAction SilentlyContinue | ' +
    'Where-Object { $_.Path -like ''' + Dir + '\*'' } | Stop-Process -Force"',
    '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
end;

// Stop a running office before files are replaced during an upgrade.
function PrepareToInstall(var NeedsRestart: Boolean): String;
begin
  StopOfficeIn(ExpandConstant('{app}'));
  Result := '';
end;

// Your office data (%LOCALAPPDATA%\Cubicle) is kept on uninstall unless you choose to remove it.
procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
var
  DataDir: String;
begin
  if CurUninstallStep = usUninstall then
    StopOfficeIn(ExpandConstant('{app}'));
  if CurUninstallStep = usPostUninstall then
  begin
    DataDir := ExpandConstant('{localappdata}\Cubicle');
    if DirExists(DataDir) and (MsgBox('Also delete your Cubicle data (tasks, routines, encrypted keys) in ' + DataDir + '?',
        mbConfirmation, MB_YESNO or MB_DEFBUTTON2) = IDYES) then
      DelTree(DataDir, True, True, True);
  end;
end;
