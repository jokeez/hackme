; Inno Setup installer for HackMe.
; Build command example:
;   iscc /DMyAppVersion=0.1.0-rc9 /DMyAppPublisher="HackMe Labs" scripts\release\windows\hackme.iss

#ifndef MyAppVersion
  #define MyAppVersion "0.1.0-dev"
#endif

#ifndef MyAppPublisher
  #define MyAppPublisher "HackMe Labs"
#endif

#ifndef MyAppURL
  #define MyAppURL "https://hackme.tech"
#endif

#ifndef MyAppIconFile
  #define MyAppIconFile "dist\release_{#MyAppVersion}\windows\hackme.ico"
#endif

#define MyAppName "HackMe"
#define MyAppExeName "hackme.exe"

[Setup]
AppId={{1E108B0A-CE35-4C48-A548-3E0424A402E2}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppPublisher={#MyAppPublisher}
AppPublisherURL={#MyAppURL}
AppSupportURL={#MyAppURL}
AppUpdatesURL={#MyAppURL}
DefaultDirName={autopf}\{#MyAppName}
DefaultGroupName={#MyAppName}
PrivilegesRequired=admin
DisableProgramGroupPage=yes
OutputBaseFilename=HackMe-Setup-{#MyAppVersion}
OutputDir=dist\release_{#MyAppVersion}\windows
Compression=lzma
SolidCompression=yes
WizardStyle=modern
ArchitecturesInstallIn64BitMode=x64
UninstallDisplayIcon={app}\{#MyAppExeName}
SetupIconFile={#MyAppIconFile}

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"
Name: "russian"; MessagesFile: "compiler:Languages\Russian.isl"

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"
Name: "autostart"; Description: "Start HackMe with Windows (after first desktop-mode setup)"; GroupDescription: "Startup"

[Files]
Source: "dist\release_{#MyAppVersion}\windows\hackme.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "dist\release_{#MyAppVersion}\windows\workerpoh.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "dist\release_{#MyAppVersion}\windows\minersign.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "dist\release_{#MyAppVersion}\windows\start_hackme_dashboard.bat"; DestDir: "{app}"; Flags: ignoreversion
Source: "dist\release_{#MyAppVersion}\windows\start_hackme_public_pool.bat"; DestDir: "{app}"; Flags: ignoreversion
Source: "dist\release_{#MyAppVersion}\windows\start_hackme_desktop_mode.bat"; DestDir: "{app}"; Flags: ignoreversion
Source: "dist\release_{#MyAppVersion}\windows\stop_hackme_desktop_mode.bat"; DestDir: "{app}"; Flags: ignoreversion
Source: "dist\release_{#MyAppVersion}\windows\status_hackme_desktop_mode.bat"; DestDir: "{app}"; Flags: ignoreversion
Source: "dist\release_{#MyAppVersion}\windows\hackme_autostart_boot.bat"; DestDir: "{app}"; Flags: ignoreversion
Source: "dist\release_{#MyAppVersion}\windows\env.public_pool.example"; DestDir: "{app}"; Flags: ignoreversion
Source: "dist\release_{#MyAppVersion}\windows\RELEASE_QUICKSTART.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "dist\release_{#MyAppVersion}\windows\README.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "dist\release_{#MyAppVersion}\windows\EXPLORER_SUBDOMAIN_RUNBOOK.md"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{group}\Start HackMe (public pool)"; Filename: "{app}\start_hackme_public_pool.bat"; IconFilename: "{app}\{#MyAppExeName}"
Name: "{group}\HackMe Desktop Mode"; Filename: "{app}\start_hackme_desktop_mode.bat"; IconFilename: "{app}\{#MyAppExeName}"
Name: "{group}\Start HackMe node only"; Filename: "{app}\start_hackme_dashboard.bat"; IconFilename: "{app}\{#MyAppExeName}"
Name: "{group}\HackMe Dashboard"; Filename: "http://127.0.0.1:8080"
Name: "{group}\HackMe Explorer"; Filename: "http://127.0.0.1:8080/explorer"
Name: "{group}\{cm:UninstallProgram,{#MyAppName}}"; Filename: "{uninstallexe}"
Name: "{autodesktop}\HackMe (public pool)"; Filename: "{app}\start_hackme_public_pool.bat"; Tasks: desktopicon; IconFilename: "{app}\{#MyAppExeName}"

[Registry]
Root: HKLM; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; ValueType: string; ValueName: "HackMe"; ValueData: """{app}\hackme_autostart_boot.bat"""; Flags: uninsdeletevalue; Tasks: autostart

[Run]
Filename: "{app}\start_hackme_public_pool.bat"; Description: "Run HackMe (public pool)"; Flags: nowait postinstall skipifsilent
