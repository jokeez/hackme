; HackMe Windows installer (Inno Setup 6+) — Miner | Orders (B2B) profiles.
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
DefaultDirName={localappdata}\{#MyAppName}
DefaultGroupName={#MyAppName}
DisableProgramGroupPage=no
PrivilegesRequired=lowest
PrivilegesRequiredOverridesAllowed=dialog commandline
OutputBaseFilename=HackMe-Setup-{#MyAppVersion}
OutputDir=..\..\..\dist\release_{#MyAppVersion}
Compression=lzma2/ultra64
SolidCompression=yes
WizardStyle=modern
WizardSizePercent=125,110
WizardImageFile=wizard-side.bmp
WizardSmallImageFile=wizard-small.bmp
WizardImageStretch=yes
SetupIconFile={#MyAppIconFile}
UninstallDisplayIcon={app}\{#MyAppExeName}
UninstallDisplayName={#MyAppName} {#MyAppVersion}
LicenseFile=LICENSE.txt
InfoBeforeFile=INSTALLER_WELCOME.txt
InfoAfterFile=INSTALLER_AFTER.txt
MinVersion=10.0
ArchitecturesInstallIn64BitMode=x64compatible
VersionInfoVersion=1.0.0.0
VersionInfoProductVersion=1.0.0.0
VersionInfoCompany={#MyAppPublisher}
VersionInfoDescription={#MyAppName} Miner & B2B Orders installer
VersionInfoProductName={#MyAppName}

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

[CustomMessages]
english.WelcomeLabel2=Welcome to HackMe Setup.%n%nChoose Miner (public pool) or Orders (B2B Dig/Hunt) on the next pages. Pool access for miners is preconfigured — no manual token hunt.
english.FinishedLabel=Setup finished. Use the Start menu shortcuts for your selected profile.
english.ProfilePageTitle=How will you use HackMe?
english.ProfilePageSubtitle=One installer — two products. You can open the other profile later from the Start menu.
english.RbMiner=Miner — public Useful PoW pool
english.RbMinerHint=GPU/CPU hashing · Mining dashboard · pool token included · watchdog
english.RbOrders=Orders — B2B Dig campaigns (Hunt = Linux)
english.RbOrdersHint=Local node · Orders dashboard · hackme-fuzzing CLI · no mining worker
english.GpuPageTitle=GPU && mining backend
english.GpuPageSubtitle=Detected graphics hardware. Choose how workerpoh should run (change later in hackme.env).
english.GpuDetected=Detected:
english.GpuTip=Tip:
english.RbAuto=Auto (recommended — detects CUDA / OpenCL / CPU)
english.RbCuda=NVIDIA CUDA (GeForce / RTX — OpenCL fallback if CUDA cannot init)
english.CudaMissingNote=workerpoh-cuda.exe is not in this Windows installer.%n%nNVIDIA on Windows will use OpenCL (workerpoh-opencl.exe).%nLinux tarball and HackMe OS ISO include workerpoh-cuda.%n%nRepair env: repair_fair_env.bat in the install folder.
english.RbOpenCL=AMD OpenCL (Radeon — RX 580, etc.)
english.RbCpu=CPU only (no GPU — low hashrate)
english.TaskLaunchMiner=Start HackMe Miner when setup finishes
english.TaskLaunchOrders=Start HackMe Orders when setup finishes
english.TaskToolchains=Install Dig toolchains (Zig / TinyGo / Node / Rust) — recommended for Orders
english.GroupAfter=After install
english.GroupStartup=Startup
english.GroupDev=B2B / Dig toolchains

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"; Flags: checkedonce
Name: "launchminer"; Description: "{cm:TaskLaunchMiner}"; GroupDescription: "{cm:GroupAfter}"; Flags: checkedonce
Name: "launchorders"; Description: "{cm:TaskLaunchOrders}"; GroupDescription: "{cm:GroupAfter}"; Flags: unchecked
Name: "autostart"; Description: "Run HackMe Miner when Windows starts (optional)"; GroupDescription: "{cm:GroupStartup}"; Flags: unchecked
Name: "toolchains"; Description: "{cm:TaskToolchains}"; GroupDescription: "{cm:GroupDev}"; Flags: unchecked

[Files]
Source: "..\..\..\dist\release_{#MyAppVersion}\windows\{#MyAppExeName}"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\..\dist\release_{#MyAppVersion}\windows\workerpoh.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\..\dist\release_{#MyAppVersion}\windows\workerpoh-opencl.exe"; DestDir: "{app}"; Flags: ignoreversion skipifsourcedoesntexist
Source: "..\..\..\dist\release_{#MyAppVersion}\windows\workerpoh-cuda.exe"; DestDir: "{app}"; Flags: ignoreversion skipifsourcedoesntexist
; Hybrid Dig process-mode + dedicated diggers — REQUIRED (no skipifsourcedoesntexist:
; silent omit broke Dig OOTB on Setup while ZIP still had the binary).
Source: "..\..\..\dist\release_{#MyAppVersion}\windows\workerfuzz.exe"; DestDir: "{app}"; Flags: ignoreversion
; No-cgo WASM validate child (CUDA/OpenCL workers must not self-reexec under RLIMIT_AS).
Source: "..\..\..\dist\release_{#MyAppVersion}\windows\hackme-wasm-check.exe"; DestDir: "{app}"; Flags: ignoreversion

Source: "..\..\..\dist\release_{#MyAppVersion}\windows\repair_hackme_env.bat"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\..\dist\release_{#MyAppVersion}\windows\repair_fair_env.bat"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\..\dist\release_{#MyAppVersion}\windows\fleetplan.exe"; DestDir: "{app}"; Flags: ignoreversion skipifsourcedoesntexist
Source: "..\..\..\dist\release_{#MyAppVersion}\windows\minersign.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\..\dist\release_{#MyAppVersion}\windows\pool.miner.token"; DestDir: "{app}"; Flags: ignoreversion
; Never ship pool.orders.token in public installers (was live admin credential).
; Operators paste a scoped publish token into pool.orders.token after install.
Source: "..\..\..\dist\release_{#MyAppVersion}\windows\hackme.ico"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\..\dist\release_{#MyAppVersion}\windows\detect_gpu.ps1"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\..\dist\release_{#MyAppVersion}\windows\write_hackme_env.ps1"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\..\dist\release_{#MyAppVersion}\windows\update_hackme_miner.ps1"; DestDir: "{app}"; Flags: ignoreversion skipifsourcedoesntexist
Source: "..\..\..\dist\release_{#MyAppVersion}\windows\update_hackme_miner.bat"; DestDir: "{app}"; Flags: ignoreversion skipifsourcedoesntexist
Source: "..\..\..\dist\release_{#MyAppVersion}\windows\setup_hackme_miner.bat"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\..\dist\release_{#MyAppVersion}\windows\start_hackme_miner.bat"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\..\dist\release_{#MyAppVersion}\windows\start_hackme_orders.bat"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\..\dist\release_{#MyAppVersion}\windows\autostart_pool_worker.bat"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\..\dist\release_{#MyAppVersion}\windows\watchdog_pool_worker.bat"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\..\dist\release_{#MyAppVersion}\windows\run_watchdog_hidden.vbs"; DestDir: "{app}"; Flags: ignoreversion
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
Source: "..\..\..\dist\release_{#MyAppVersion}\windows\install_from_code_toolchains.bat"; DestDir: "{app}"; Flags: ignoreversion skipifsourcedoesntexist
Source: "..\..\..\dist\release_{#MyAppVersion}\windows\install_from_code_toolchains.ps1"; DestDir: "{app}"; Flags: ignoreversion skipifsourcedoesntexist
Source: "..\..\..\dist\release_{#MyAppVersion}\windows\hackme-fuzzing.exe"; DestDir: "{app}"; Flags: ignoreversion skipifsourcedoesntexist
Source: "..\..\..\dist\release_{#MyAppVersion}\windows\hackme-fuzzing-build.exe"; DestDir: "{app}"; Flags: ignoreversion skipifsourcedoesntexist
Source: "Install-HackMe.ps1"; DestDir: "{app}"; Flags: ignoreversion skipifsourcedoesntexist
Source: "HackMe-Install.cmd"; DestDir: "{app}"; Flags: ignoreversion skipifsourcedoesntexist
Source: "detect_gpu.ps1"; Flags: dontcopy

[Dirs]
Name: "{app}\logs"; Permissions: users-modify
Name: "{app}\data"; Permissions: users-modify
Name: "{app}\logs\orders"; Permissions: users-modify

[Icons]
Name: "{group}\{#MyAppName} Miner"; Filename: "{app}\start_hackme_miner.bat"; WorkingDir: "{app}"; IconFilename: "{app}\{#MyAppExeName}"; Comment: "Public pool miner — hackme.tech"
Name: "{group}\{#MyAppName} Orders"; Filename: "{app}\start_hackme_orders.bat"; WorkingDir: "{app}"; IconFilename: "{app}\{#MyAppExeName}"; Comment: "B2B Dig orders — Hunt coverage runs on Linux pool"
Name: "{group}\{#MyAppName} Dashboard"; Filename: "http://127.0.0.1:8080/"; IconFilename: "{app}\{#MyAppExeName}"
Name: "{group}\Configure miner"; Filename: "{app}\setup_hackme_miner.bat"; WorkingDir: "{app}"; IconFilename: "{app}\{#MyAppExeName}"
Name: "{group}\Check for updates"; Filename: "{app}\update_hackme_miner.bat"; WorkingDir: "{app}"; IconFilename: "{app}\{#MyAppExeName}"; Comment: "Fetch latest.json and update HackMe (keeps hackme.env)"
Name: "{group}\Repair miner env"; Filename: "{app}\repair_fair_env.bat"; WorkingDir: "{app}"; IconFilename: "{app}\{#MyAppExeName}"; Comment: "Refresh hackme.env (fair pool settings)"
Name: "{group}\Readme — first steps"; Filename: "{app}\MINER_WINDOWS_ONE_CLICK.md"; IconFilename: "{app}\{#MyAppExeName}"
Name: "{group}\{cm:UninstallProgram,{#MyAppName}}"; Filename: "{uninstallexe}"
Name: "{autodesktop}\{#MyAppName} Miner"; Filename: "{app}\start_hackme_miner.bat"; Tasks: desktopicon; Check: IsMinerProfile; WorkingDir: "{app}"; IconFilename: "{app}\{#MyAppExeName}"
Name: "{autodesktop}\{#MyAppName} Orders"; Filename: "{app}\start_hackme_orders.bat"; Tasks: desktopicon; Check: IsOrdersProfile; WorkingDir: "{app}"; IconFilename: "{app}\{#MyAppExeName}"

[Registry]
Root: HKCU; Subkey: "Software\{#MyAppPublisher}\{#MyAppName}"; ValueType: string; ValueName: "Version"; ValueData: "{#MyAppVersion}"; Flags: uninsdeletekey
Root: HKCU; Subkey: "Software\{#MyAppPublisher}\{#MyAppName}"; ValueType: string; ValueName: "InstallPath"; ValueData: "{app}"; Flags: uninsdeletekey
Root: HKCU; Subkey: "Software\{#MyAppPublisher}\{#MyAppName}"; ValueType: string; ValueName: "InstallProfile"; ValueData: "{code:GetInstallProfile}"; Flags: uninsdeletekey
Root: HKCU; Subkey: "Software\{#MyAppPublisher}\{#MyAppName}"; ValueType: string; ValueName: "GpuBackend"; ValueData: "{code:GetGpuBackendChoice}"; Flags: uninsdeletekey
Root: HKCU; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; ValueType: string; ValueName: "HackMeNode"; ValueData: """{app}\hackme_autostart_boot.bat"""; Flags: uninsdeletevalue; Tasks: autostart

[Run]
Filename: "{app}\start_hackme_miner.bat"; Description: "Launch {#MyAppName} Miner"; Flags: nowait postinstall skipifsilent; Tasks: launchminer; Check: IsMinerProfile
Filename: "{app}\start_hackme_orders.bat"; Description: "Launch {#MyAppName} Orders"; Flags: nowait postinstall skipifsilent; Tasks: launchorders; Check: IsOrdersProfile

[UninstallDelete]
Type: filesandordirs; Name: "{app}\logs"
Type: filesandordirs; Name: "{app}\toolchains"
Type: dirifempty; Name: "{app}"

[Code]
var
  ProfilePage: TWizardPage;
  RbMiner, RbOrders: TNewRadioButton;
  LblMinerHint, LblOrdersHint, LblProfileNote: TNewStaticText;
  InstallProfile: String;
  GpuPage: TWizardPage;
  LblDetected, LblTip: TLabel;
  RbAuto, RbCuda, RbOpenCL, RbCpu: TRadioButton;
  GpuSummary, GpuTip, GpuBackendChoice: String;
  GpuDetectDone: Boolean;

function GetGpuBackendChoice(Param: String): String;
begin
  Result := GpuBackendChoice;
end;

function GetInstallProfile(Param: String): String;
begin
  Result := InstallProfile;
end;

function IsMinerProfile: Boolean;
begin
  Result := (InstallProfile = 'miner');
end;

function IsOrdersProfile: Boolean;
begin
  Result := (InstallProfile = 'orders');
end;

function BoolStr(Ok: Boolean): String;
begin
  if Ok then Result := '1' else Result := '0';
end;

function PosFrom(const SubStr, S: String; Start: Integer): Integer;
var
  T: String;
begin
  if Start < 1 then Start := 1;
  T := Copy(S, Start, MaxInt);
  Result := Pos(SubStr, T);
  if Result <> 0 then
    Result := Result + Start - 1;
end;

function ExtractJsonFirstTip(const Json: String): String;
var
  P, Q: Integer;
begin
  Result := '';
  P := Pos('"tips":', Json);
  if P = 0 then Exit;
  P := PosFrom('"', Json, P + 7);
  if P = 0 then Exit;
  Q := PosFrom('"', Json, P + 1);
  if Q > P then Result := Copy(Json, P + 1, Q - P - 1);
end;

function GpuSummaryLooksNvidia(const S: String): Boolean;
var
  L: String;
begin
  L := LowerCase(S);
  Result := (Pos('nvidia', L) > 0) or (Pos('geforce', L) > 0) or (Pos('rtx', L) > 0) or (Pos('gtx', L) > 0);
end;

procedure ApplyGpuDetectTip(const Json: String);
var
  Tip: String;
begin
  Tip := ExtractJsonFirstTip(Json);
  if Tip <> '' then
    GpuTip := Tip
  else if GpuSummaryLooksNvidia(GpuSummary) then
    GpuTip := 'NVIDIA on Windows: OpenCL path. HackMe OS / Linux: CUDA for max GH/s.'
  else
    GpuTip := 'Pick Auto, NVIDIA CUDA, AMD OpenCL, or CPU-only below.';
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
    begin
      GpuSummary := Copy(S, 1, 200);
      ApplyGpuDetectTip(String(S));
    end
    else
      GpuSummary := 'GPU detected';
  end;
end;

procedure RefreshGpuPageText;
begin
  if Assigned(LblDetected) then
    LblDetected.Caption := ExpandConstant('{cm:GpuDetected}') + ' ' + GpuSummary;
  if Assigned(LblTip) then
    LblTip.Caption := ExpandConstant('{cm:GpuTip}') + ' ' + GpuTip;
end;

procedure SyncProfileChoice;
begin
  if Assigned(RbOrders) and RbOrders.Checked then
    InstallProfile := 'orders'
  else
    InstallProfile := 'miner';
end;

procedure ProfileRadioClick(Sender: TObject);
begin
  SyncProfileChoice;
end;

procedure ApplyProfileTaskDefaults;
begin
  SyncProfileChoice;
  if IsOrdersProfile then
  begin
    WizardSelectTasks('launchorders,toolchains,!launchminer,!autostart');
  end
  else
  begin
    WizardSelectTasks('launchminer,!launchorders');
  end;
end;

procedure InitializeWizard;
var
  TopY: Integer;
begin
  InstallProfile := 'miner';
  GpuBackendChoice := 'auto';

  ProfilePage := CreateCustomPage(wpWelcome,
    ExpandConstant('{cm:ProfilePageTitle}'),
    ExpandConstant('{cm:ProfilePageSubtitle}'));

  TopY := 8;
  RbMiner := TNewRadioButton.Create(ProfilePage);
  RbMiner.Parent := ProfilePage.Surface;
  RbMiner.Left := 0;
  RbMiner.Top := TopY;
  RbMiner.Width := ProfilePage.SurfaceWidth;
  RbMiner.Height := 22;
  RbMiner.Caption := ExpandConstant('{cm:RbMiner}');
  RbMiner.Checked := True;
  RbMiner.OnClick := @ProfileRadioClick;
  RbMiner.Font.Style := [fsBold];

  LblMinerHint := TNewStaticText.Create(ProfilePage);
  LblMinerHint.Parent := ProfilePage.Surface;
  LblMinerHint.Left := 20;
  LblMinerHint.Top := TopY + 24;
  LblMinerHint.Width := ProfilePage.SurfaceWidth - 24;
  LblMinerHint.Height := 36;
  LblMinerHint.AutoSize := False;
  LblMinerHint.WordWrap := True;
  LblMinerHint.Caption := ExpandConstant('{cm:RbMinerHint}');
  LblMinerHint.Font.Color := clGray;

  TopY := TopY + 72;
  RbOrders := TNewRadioButton.Create(ProfilePage);
  RbOrders.Parent := ProfilePage.Surface;
  RbOrders.Left := 0;
  RbOrders.Top := TopY;
  RbOrders.Width := ProfilePage.SurfaceWidth;
  RbOrders.Height := 22;
  RbOrders.Caption := ExpandConstant('{cm:RbOrders}');
  RbOrders.OnClick := @ProfileRadioClick;
  RbOrders.Font.Style := [fsBold];

  LblOrdersHint := TNewStaticText.Create(ProfilePage);
  LblOrdersHint.Parent := ProfilePage.Surface;
  LblOrdersHint.Left := 20;
  LblOrdersHint.Top := TopY + 24;
  LblOrdersHint.Width := ProfilePage.SurfaceWidth - 24;
  LblOrdersHint.Height := 48;
  LblOrdersHint.AutoSize := False;
  LblOrdersHint.WordWrap := True;
  LblOrdersHint.Caption := ExpandConstant('{cm:RbOrdersHint}');
  LblOrdersHint.Font.Color := clGray;

  LblProfileNote := TNewStaticText.Create(ProfilePage);
  LblProfileNote.Parent := ProfilePage.Surface;
  LblProfileNote.Left := 0;
  LblProfileNote.Top := TopY + 88;
  LblProfileNote.Width := ProfilePage.SurfaceWidth;
  LblProfileNote.Height := 48;
  LblProfileNote.AutoSize := False;
  LblProfileNote.WordWrap := True;
  LblProfileNote.Caption := 'Both profiles share one install folder. Start menu always includes Miner and Orders shortcuts.';
  LblProfileNote.Font.Color := clGray;

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

  GpuDetectDone := False;
end;

function ShouldSkipPage(PageID: Integer): Boolean;
begin
  Result := False;
  if Assigned(GpuPage) and (PageID = GpuPage.ID) then
    Result := IsOrdersProfile;
end;

function NextButtonClick(CurPageID: Integer): Boolean;
begin
  Result := True;
  if Assigned(ProfilePage) and (CurPageID = ProfilePage.ID) then
  begin
    SyncProfileChoice;
    ApplyProfileTaskDefaults;
  end;
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
  begin
    if not GpuDetectDone then
    begin
      GpuDetectDone := True;
      try
        ExtractTemporaryFile('detect_gpu.ps1');
        RunGpuDetect;
      except
        GpuSummary := '(GPU detect skipped)';
      end;
    end;
    RefreshGpuPageText;
  end;
end;

procedure CurStepChanged(CurStep: TSetupStep);
var
  ResultCode: Integer;
  AppDir, PsWrite, PsDetect, Cmd: String;
  ShowCudaNote: Boolean;
begin
  if CurStep = ssPostInstall then
  begin
    SyncProfileChoice;
    AppDir := ExpandConstant('{app}');

    if IsMinerProfile then
    begin
      PsDetect := AppDir + '\detect_gpu.ps1';
      PsWrite := AppDir + '\write_hackme_env.ps1';
      if FileExists(PsDetect) then
      begin
        Cmd := '-NoProfile -ExecutionPolicy Bypass -File "' + PsDetect + '" -OutFile "' + AppDir + '\gpu_detect.json"';
        Exec('powershell.exe', Cmd, AppDir, SW_HIDE, ewWaitUntilTerminated, ResultCode);
      end;
      ShowCudaNote := (not FileExists(AppDir + '\workerpoh-cuda.exe')) and
        (GpuSummaryLooksNvidia(GpuSummary) or (GpuBackendChoice = 'cuda'));
      if ShowCudaNote then
        MsgBox(ExpandConstant('{cm:CudaMissingNote}'), mbInformation, MB_OK);
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

    if WizardIsTaskSelected('toolchains') then
    begin
      if FileExists(AppDir + '\install_from_code_toolchains.ps1') then
      begin
        Cmd := '-NoProfile -ExecutionPolicy Bypass -File "' + AppDir + '\install_from_code_toolchains.ps1" -AppDir "' + AppDir + '"';
        Exec('powershell.exe', Cmd, AppDir, SW_SHOW, ewWaitUntilTerminated, ResultCode);
      end;
    end;
  end;
end;
