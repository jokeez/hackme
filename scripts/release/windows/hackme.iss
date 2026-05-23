; HackMe Windows installer (Inno Setup 6+).
; Build: VERSION=x.y.z bash scripts/release/make_release_bundle.sh && bash scripts/release/windows/build_installer.sh x.y.z

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
WizardSizePercent=120,105
ArchitecturesInstallIn64BitMode=x64compatible
UninstallDisplayIcon={app}\{#MyAppExeName}
UninstallDisplayName={#MyAppName} Miner {#MyAppVersion}
SetupIconFile={#MyAppIconFile}
LicenseFile=LICENSE.txt
InfoBeforeFile=INSTALLER_WELCOME.txt
InfoAfterFile=INSTALLER_AFTER.txt
MinVersion=10.0
VersionInfoVersion=1.0.0.0
VersionInfoProductVersion=1.0.0.0
VersionInfoCompany={#MyAppPublisher}
VersionInfoDescription={#MyAppName} public pool miner installer
VersionInfoProductName={#MyAppName}
; Modern look (teal accent where supported by IS 6.7+)
[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"
Name: "russian"; MessagesFile: "compiler:Languages\Russian.isl"

[CustomMessages]
english.WelcomeLabel2=Install the official HackMe pool miner for hackme.tech.%n%nPool access is preconfigured — no manual token hunt.%n%nOn the next pages you can confirm GPU settings and shortcuts.
russian.WelcomeLabel2=Установка официального майнера HackMe для пула hackme.tech.%n%nТокен пула уже в комплекте — искать вручную не нужно.%n%nДалее — настройка GPU и ярлыков.
english.GpuPageTitle=GPU && mining backend
english.GpuPageSubtitle=We detected your graphics hardware. Choose how workerpoh should run (you can change later in hackme.env).
russian.GpuPageTitle=Видеокарта и backend
russian.GpuPageSubtitle=Определено оборудование. Выберите режим workerpoh (позже можно изменить в hackme.env).
english.GpuDetected=Detected:
russian.GpuDetected=Обнаружено:
english.GpuTip=Tip:
russian.GpuTip=Подсказка:
english.RbAuto=Auto (recommended — detects CUDA / OpenCL / CPU)
russian.RbAuto=Авто (рекомендуется — CUDA / OpenCL / CPU)
english.RbCuda=NVIDIA CUDA (GeForce / RTX)
russian.RbCuda=NVIDIA CUDA (GeForce / RTX)
english.RbOpenCL=AMD OpenCL (Radeon — RX 580, etc.)
russian.RbOpenCL=AMD OpenCL (Radeon — RX 580 и др.)
english.RbCpu=CPU only (no GPU — low hashrate)
russian.RbCpu=Только CPU (без GPU — низкий GH/s)

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"; Flags: checkedonce
Name: "launchminer"; Description: "Start HackMe Miner when setup finishes"; GroupDescription: "After install"; Flags: checkedonce
Name: "autostart"; Description: "Run HackMe node when Windows starts (optional)"; GroupDescription: "Startup"; Flags: unchecked

[Files]
Source: "..\..\..\dist\release_{#MyAppVersion}\windows\{#MyAppExeName}"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\..\dist\release_{#MyAppVersion}\windows\workerpoh.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\..\dist\release_{#MyAppVersion}\windows\workerpoh-opencl.exe"; DestDir: "{app}"; Flags: ignoreversion skipifsourcedoesntexist
Source: "..\..\..\dist\release_{#MyAppVersion}\windows\fleetplan.exe"; DestDir: "{app}"; Flags: ignoreversion skipifsourcedoesntexist
Source: "..\..\..\dist\release_{#MyAppVersion}\windows\minersign.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\..\dist\release_{#MyAppVersion}\windows\pool.miner.token"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\..\dist\release_{#MyAppVersion}\windows\hackme.ico"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\..\dist\release_{#MyAppVersion}\windows\detect_gpu.ps1"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\..\dist\release_{#MyAppVersion}\windows\write_hackme_env.ps1"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\..\dist\release_{#MyAppVersion}\windows\setup_hackme_miner.bat"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\..\dist\release_{#MyAppVersion}\windows\start_hackme_miner.bat"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\..\dist\release_{#MyAppVersion}\windows\autostart_pool_worker.bat"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\..\dist\release_{#MyAppVersion}\windows\watchdog_pool_worker.bat"; DestDir: "{app}"; Flags: ignoreversion
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
Source: "detect_gpu.ps1"; Flags: dontcopy

[Dirs]
Name: "{app}\logs"; Permissions: users-modify
Name: "{app}\data"; Permissions: users-modify

[Icons]
Name: "{group}\{#MyAppName} Miner"; Filename: "{app}\start_hackme_miner.bat"; WorkingDir: "{app}"; IconFilename: "{app}\{#MyAppExeName}"; Comment: "Public pool miner — hackme.tech"
Name: "{group}\{#MyAppName} Dashboard"; Filename: "http://127.0.0.1:8080/#mining"; IconFilename: "{app}\{#MyAppExeName}"
Name: "{group}\Configure miner"; Filename: "{app}\setup_hackme_miner.bat"; WorkingDir: "{app}"; IconFilename: "{app}\{#MyAppExeName}"
Name: "{group}\Readme — first steps"; Filename: "{app}\MINER_WINDOWS_ONE_CLICK.md"; IconFilename: "{app}\{#MyAppExeName}"
Name: "{group}\{cm:UninstallProgram,{#MyAppName}}"; Filename: "{uninstallexe}"
Name: "{autodesktop}\{#MyAppName} Miner"; Filename: "{app}\start_hackme_miner.bat"; Tasks: desktopicon; WorkingDir: "{app}"; IconFilename: "{app}\{#MyAppExeName}"

[Registry]
Root: HKLM; Subkey: "Software\{#MyAppPublisher}\{#MyAppName}"; ValueType: string; ValueName: "Version"; ValueData: "{#MyAppVersion}"; Flags: uninsdeletekey
Root: HKLM; Subkey: "Software\{#MyAppPublisher}\{#MyAppName}"; ValueType: string; ValueName: "InstallPath"; ValueData: "{app}"; Flags: uninsdeletekey
Root: HKLM; Subkey: "Software\{#MyAppPublisher}\{#MyAppName}"; ValueType: string; ValueName: "GpuBackend"; ValueData: "{code:GetGpuBackendChoice}"; Flags: uninsdeletekey
Root: HKLM; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; ValueType: string; ValueName: "HackMeNode"; ValueData: """{app}\hackme_autostart_boot.bat"""; Flags: uninsdeletevalue; Tasks: autostart

[Run]
Filename: "{app}\start_hackme_miner.bat"; Description: "Launch {#MyAppName} Miner"; Flags: nowait postinstall skipifsilent; Tasks: launchminer

[UninstallDelete]
Type: filesandordirs; Name: "{app}\logs"
Type: dirifempty; Name: "{app}"

[Code]
var
  GpuPage: TWizardPage;
  LblDetected, LblTip: TLabel;
  RbAuto, RbCuda, RbOpenCL, RbCpu: TRadioButton;
  GpuSummary, GpuTip, GpuBackendChoice: String;

function GetGpuBackendChoice(Param: String): String;
begin
  Result := GpuBackendChoice;
end;

function BoolStr(Ok: Boolean): String;
begin
  if Ok then Result := '1' else Result := '0';
end;

procedure RunGpuDetect;
var
  TmpJson, Ps1, Cmd: String;
  S: AnsiString;
  ResultCode: Integer;
begin
  GpuSummary := '(detecting...)';
  GpuTip := 'Choose Auto if unsure.';
  TmpJson := ExpandConstant('{tmp}\hackme_gpu_detect.json');
  Ps1 := ExpandConstant('{tmp}\detect_gpu.ps1');
  if not FileExists(Ps1) then Exit;
  Cmd := '-NoProfile -ExecutionPolicy Bypass -File "' + Ps1 + '" -OutFile "' + TmpJson + '"';
  if Exec('powershell.exe', Cmd, '', SW_HIDE, ewWaitUntilTerminated, ResultCode) and FileExists(TmpJson) then
  begin
    if LoadStringFromFile(TmpJson, S) then
      GpuSummary := Copy(S, 1, 200)
    else
      GpuSummary := 'GPU detected';
    GpuTip := 'Pick Auto, NVIDIA CUDA, AMD OpenCL, or CPU-only below.';
  end;
end;

procedure RefreshGpuPageText;
begin
  if Assigned(LblDetected) then
    LblDetected.Caption := ExpandConstant('{cm:GpuDetected}') + ' ' + GpuSummary;
  if Assigned(LblTip) then
    LblTip.Caption := ExpandConstant('{cm:GpuTip}') + ' ' + GpuTip;
end;

procedure InitializeWizard;
begin
  GpuBackendChoice := 'auto';
  GpuPage := CreateCustomPage(wpSelectTasks, ExpandConstant('{cm:GpuPageTitle}'), ExpandConstant('{cm:GpuPageSubtitle}'));

  LblDetected := TLabel.Create(GpuPage);
  LblDetected.Parent := GpuPage.Surface;
  LblDetected.Left := 0;
  LblDetected.Top := 0;
  LblDetected.Width := GpuPage.SurfaceWidth;
  LblDetected.AutoSize := False;
  LblDetected.WordWrap := True;
  LblDetected.Caption := ExpandConstant('{cm:GpuDetected}') + ' ...';

  LblTip := TLabel.Create(GpuPage);
  LblTip.Parent := GpuPage.Surface;
  LblTip.Left := 0;
  LblTip.Top := 40;
  LblTip.Width := GpuPage.SurfaceWidth;
  LblTip.AutoSize := False;
  LblTip.WordWrap := True;
  LblTip.Font.Style := [fsItalic];

  RbAuto := TRadioButton.Create(GpuPage);
  RbAuto.Parent := GpuPage.Surface;
  RbAuto.Left := 0;
  RbAuto.Top := 90;
  RbAuto.Width := GpuPage.SurfaceWidth;
  RbAuto.Caption := ExpandConstant('{cm:RbAuto}');
  RbAuto.Checked := True;

  RbCuda := TRadioButton.Create(GpuPage);
  RbCuda.Parent := GpuPage.Surface;
  RbCuda.Left := 0;
  RbCuda.Top := 118;
  RbCuda.Width := GpuPage.SurfaceWidth;
  RbCuda.Caption := ExpandConstant('{cm:RbCuda}');

  RbOpenCL := TRadioButton.Create(GpuPage);
  RbOpenCL.Parent := GpuPage.Surface;
  RbOpenCL.Left := 0;
  RbOpenCL.Top := 146;
  RbOpenCL.Width := GpuPage.SurfaceWidth;
  RbOpenCL.Caption := ExpandConstant('{cm:RbOpenCL}');

  RbCpu := TRadioButton.Create(GpuPage);
  RbCpu.Parent := GpuPage.Surface;
  RbCpu.Left := 0;
  RbCpu.Top := 174;
  RbCpu.Width := GpuPage.SurfaceWidth;
  RbCpu.Caption := ExpandConstant('{cm:RbCpu}');

  ExtractTemporaryFile('detect_gpu.ps1');
  RunGpuDetect;
  RefreshGpuPageText;
end;

function NextButtonClick(CurPageID: Integer): Boolean;
begin
  Result := True;
  if Assigned(GpuPage) and (CurPageID = GpuPage.ID) then
  begin
    if RbCuda.Checked then GpuBackendChoice := 'cuda'
    else if RbOpenCL.Checked then GpuBackendChoice := 'opencl'
    else if RbCpu.Checked then GpuBackendChoice := 'cpu'
    else GpuBackendChoice := 'auto';
  end;
end;

procedure CurPageChanged(CurPageID: Integer);
begin
  if Assigned(GpuPage) and (CurPageID = GpuPage.ID) then
    RefreshGpuPageText;
end;

procedure CurStepChanged(CurStep: TSetupStep);
var
  ResultCode: Integer;
  AppDir, PsWrite, PsDetect, Cmd: String;
begin
  if CurStep = ssPostInstall then
  begin
    AppDir := ExpandConstant('{app}');
    PsDetect := AppDir + '\detect_gpu.ps1';
    PsWrite := AppDir + '\write_hackme_env.ps1';
    if FileExists(PsDetect) then
    begin
      Cmd := '-NoProfile -ExecutionPolicy Bypass -File "' + PsDetect + '" -OutFile "' + AppDir + '\gpu_detect.json"';
      Exec('powershell.exe', Cmd, AppDir, SW_HIDE, ewWaitUntilTerminated, ResultCode);
    end;
    if FileExists(PsWrite) then
    begin
      Cmd := '-NoProfile -ExecutionPolicy Bypass -File "' + PsWrite + '" -InstallDir "' + AppDir + '" -GpuBackend ' + GpuBackendChoice + ' -NonInteractive';
      if not Exec('powershell.exe', Cmd, AppDir, SW_SHOW, ewWaitUntilTerminated, ResultCode) then
        MsgBox('HackMe: could not write hackme.env. Run Configure miner from the Start menu.', mbError, MB_OK);
    end
    else
    begin
      Cmd := '/c set HACKME_SETUP_NONINTERACTIVE=1&& set HACKME_GPU_BACKEND_CHOICE=' + GpuBackendChoice + '&& call "' + AppDir + '\setup_hackme_miner.bat"';
      Exec(ExpandConstant('{cmd}'), Cmd, AppDir, SW_SHOW, ewWaitUntilTerminated, ResultCode);
    end;
  end;
end;
