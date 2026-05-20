; HackMe Windows installer (Inno Setup 6+).
; Build (repo root):
;   VERSION=0.1.0-rc11c bash scripts/release/make_release_bundle.sh
;   bash scripts/release/windows/build_installer.sh 0.1.0-rc11c
; Or on Windows:
;   pwsh -File scripts/release/windows/build_installer.ps1 -Version 0.1.0-rc11c

#ifndef MyAppVersion
  #define MyAppVersion "0.1.0-dev"
#endif

#ifndef MyAppPublisher
  #define MyAppPublisher "HackMe Network"
#endif

#ifndef MyAppURL
  #define MyAppURL "https://hackme.tech"
#endif

#ifndef MyAppIconFile
  #define MyAppIconFile "hackme.ico"
#endif

#define MyAppName "HackMe"
#define MyAppExeName "hackme.exe"
[Setup]
AppId={{1E108B0A-CE35-4C48-A548-3E0424A402E2}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppVerName={#MyAppName} {#MyAppVersion}
AppPublisher={#MyAppPublisher}
AppPublisherURL={#MyAppURL}
AppSupportURL={#MyAppURL}
AppUpdatesURL={#MyAppURL}/downloads.html
DefaultDirName={autopf}\{#MyAppName}
DefaultGroupName={#MyAppName}
DisableProgramGroupPage=no
PrivilegesRequired=admin
PrivilegesRequiredOverridesAllowed=dialog
OutputBaseFilename=HackMe-Setup-{#MyAppVersion}
OutputDir=..\..\..\dist\release_{#MyAppVersion}
Compression=lzma2/ultra64
SolidCompression=yes
WizardStyle=modern
WizardSizePercent=120,100
ArchitecturesInstallIn64BitMode=x64compatible
UninstallDisplayIcon={app}\{#MyAppExeName}
UninstallDisplayName={#MyAppName} Miner {#MyAppVersion}
SetupIconFile={#MyAppIconFile}
LicenseFile=LICENSE.txt
InfoBeforeFile=INSTALLER_WELCOME.txt
MinVersion=10.0
VersionInfoVersion=1.0.0.0
VersionInfoProductVersion=1.0.0.0
VersionInfoCompany={#MyAppPublisher}
VersionInfoDescription={#MyAppName} public pool miner installer
VersionInfoProductName={#MyAppName}

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"
Name: "russian"; MessagesFile: "compiler:Languages\Russian.isl"

[CustomMessages]
english.Welcome2=This wizard installs HackMe on your PC for mining on the public pool at hackme.tech.%n%n• Pool token is preconfigured%n• A unique local admin token is created%n• Shortcuts are added to the Start menu and desktop%n%nClick Next to continue.
russian.Welcome2=Мастер установит HackMe для майнинга в публичном пуле hackme.tech.%n%n• Токен пула уже в комплекте%n• Локальный admin-токен создаётся автоматически%n• Ярлыки в меню Пуск и на рабочем столе%n%nНажмите «Далее».

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"; Flags: checkedonce
Name: "launchminer"; Description: "Start HackMe Miner when setup finishes"; GroupDescription: "After install"; Flags: checkedonce
Name: "autostart"; Description: "Run HackMe node when Windows starts (optional)"; GroupDescription: "Startup"; Flags: unchecked

[Files]
Source: "..\..\..\dist\release_{#MyAppVersion}\windows\{#MyAppExeName}"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\..\dist\release_{#MyAppVersion}\windows\workerpoh.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\..\dist\release_{#MyAppVersion}\windows\workerpoh-opencl.exe"; DestDir: "{app}"; Flags: ignoreversion skipifsourcedoesntexist
Source: "..\..\..\dist\release_{#MyAppVersion}\windows\minersign.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\..\dist\release_{#MyAppVersion}\windows\pool.miner.token"; DestDir: "{app}"; Flags: ignoreversion skipifsourcedoesntexist
Source: "..\..\..\dist\release_{#MyAppVersion}\windows\hackme.ico"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\..\dist\release_{#MyAppVersion}\windows\setup_hackme_miner.bat"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\..\dist\release_{#MyAppVersion}\windows\start_hackme_miner.bat"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\..\dist\release_{#MyAppVersion}\windows\autostart_pool_worker.bat"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\..\dist\release_{#MyAppVersion}\windows\start_hackme_public_pool.bat"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\..\dist\release_{#MyAppVersion}\windows\start_hackme_dashboard.bat"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\..\dist\release_{#MyAppVersion}\windows\start_hackme_desktop_mode.bat"; DestDir: "{app}"; Flags: ignoreversion skipifsourcedoesntexist
Source: "..\..\..\dist\release_{#MyAppVersion}\windows\stop_hackme_desktop_mode.bat"; DestDir: "{app}"; Flags: ignoreversion skipifsourcedoesntexist
Source: "..\..\..\dist\release_{#MyAppVersion}\windows\status_hackme_desktop_mode.bat"; DestDir: "{app}"; Flags: ignoreversion skipifsourcedoesntexist
Source: "..\..\..\dist\release_{#MyAppVersion}\windows\hackme_autostart_boot.bat"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\..\dist\release_{#MyAppVersion}\windows\env.public_pool.example"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\..\dist\release_{#MyAppVersion}\windows\MINER_WINDOWS_ONE_CLICK.md"; DestDir: "{app}"; Flags: ignoreversion skipifsourcedoesntexist
Source: "..\..\..\dist\release_{#MyAppVersion}\windows\RELEASE_QUICKSTART.md"; DestDir: "{app}"; Flags: ignoreversion skipifsourcedoesntexist
Source: "..\..\..\dist\release_{#MyAppVersion}\windows\README.md"; DestDir: "{app}"; Flags: ignoreversion skipifsourcedoesntexist

[Dirs]
Name: "{app}\logs"; Permissions: users-modify
Name: "{app}\data"; Permissions: users-modify

[Icons]
Name: "{group}\{#MyAppName} Miner"; Filename: "{app}\start_hackme_miner.bat"; WorkingDir: "{app}"; IconFilename: "{app}\{#MyAppExeName}"; Comment: "Public pool miner — hackme.tech"
Name: "{group}\{#MyAppName} Dashboard"; Filename: "http://127.0.0.1:8080/#mining"; IconFilename: "{app}\{#MyAppExeName}"
Name: "{group}\Readme — first steps"; Filename: "{app}\MINER_WINDOWS_ONE_CLICK.md"; IconFilename: "{app}\{#MyAppExeName}"
Name: "{group}\{cm:UninstallProgram,{#MyAppName}}"; Filename: "{uninstallexe}"
Name: "{autodesktop}\{#MyAppName} Miner"; Filename: "{app}\start_hackme_miner.bat"; Tasks: desktopicon; WorkingDir: "{app}"; IconFilename: "{app}\{#MyAppExeName}"

[Registry]
Root: HKLM; Subkey: "Software\{#MyAppPublisher}\{#MyAppName}"; ValueType: string; ValueName: "Version"; ValueData: "{#MyAppVersion}"; Flags: uninsdeletekey
Root: HKLM; Subkey: "Software\{#MyAppPublisher}\{#MyAppName}"; ValueType: string; ValueName: "InstallPath"; ValueData: "{app}"; Flags: uninsdeletekey
Root: HKLM; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; ValueType: string; ValueName: "HackMeNode"; ValueData: """{app}\hackme_autostart_boot.bat"""; Flags: uninsdeletevalue; Tasks: autostart

[Run]
Filename: "{app}\start_hackme_miner.bat"; Description: "Launch {#MyAppName} Miner"; Flags: nowait postinstall skipifsilent; Tasks: launchminer

[UninstallDelete]
Type: filesandordirs; Name: "{app}\logs"
Type: dirifempty; Name: "{app}"

[Code]
function InitializeSetup: Boolean;
begin
  Result := True;
end;

procedure CurStepChanged(CurStep: TSetupStep);
var
  ResultCode: Integer;
begin
  if CurStep = ssPostInstall then
  begin
    Exec(ExpandConstant('{cmd}'), '/c set HACKME_SETUP_NONINTERACTIVE=1&& call "' +
      ExpandConstant('{app}\setup_hackme_miner.bat') + '"',
      ExpandConstant('{app}'), SW_HIDE, ewWaitUntilTerminated, ResultCode);
  end;
end;
