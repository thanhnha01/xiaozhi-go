; XiaoZhi PC per-user installer. Bundles the GUI EXE and its native audio DLLs.
#define MyAppName "Tiểu Trí - XiaoZhi PC"
#ifndef MyAppVersion
  #define MyAppVersion "1.3.0"
#endif
[Setup]
AppId={{A36F8A65-5DE4-4F32-B27D-A2E4CA3893CD}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppPublisher=XiaoZhi PC Community
AppPublisherURL=https://github.com/thanhnha01/xiaozhi-go
AppSupportURL=https://github.com/thanhnha01/xiaozhi-go/issues
AppUpdatesURL=https://github.com/thanhnha01/xiaozhi-go/releases
DefaultDirName={localappdata}\Programs\XiaoZhi PC
DefaultGroupName={#MyAppName}
DisableProgramGroupPage=yes
UninstallDisplayName={#MyAppName}
UninstallDisplayIcon={app}\XiaoZhi-PC.exe
OutputDir=..\output
OutputBaseFilename=XiaoZhi-PC-Setup-v{#MyAppVersion}
SetupIconFile=xiaozhi.ico
PrivilegesRequired=lowest
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
CloseApplications=yes
RestartApplications=no
ChangesAssociations=no
CreateUninstallRegKey=yes
UsePreviousAppDir=yes
VersionInfoVersion={#MyAppVersion}.0

[Languages]
Name: "vi"; MessagesFile: "compiler:Languages\Vietnamese.isl"
Name: "en"; MessagesFile: "compiler:Default.isl"

[Files]
Source: "..\dist\XiaoZhi-PC.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "dist\*.dll"; DestDir: "{app}"; Flags: ignoreversion

[Tasks]
Name: "desktopicon"; Description: "Tạo biểu tượng ngoài màn hình (Desktop)"; GroupDescription: "Biểu tượng:"; Flags: checkedonce

[Icons]
Name: "{group}\{#MyAppName}"; Filename: "{app}\XiaoZhi-PC.exe"
Name: "{autodesktop}\{#MyAppName}"; Filename: "{app}\XiaoZhi-PC.exe"; Tasks: desktopicon

[Run]
Filename: "{app}\XiaoZhi-PC.exe"; Description: "Khởi động Tiểu Trí"; Flags: nowait postinstall skipifsilent

[UninstallRun]
Filename: "{cmd}"; Parameters: "/C taskkill /F /IM XiaoZhi-PC.exe >NUL 2>&1"; Flags: runhidden

[UninstallDelete]
; Explicit full-removal policy: device ID/pairing and WebView2 profile are removed.
Type: filesandordirs; Name: "{userappdata}\XiaoZhiPC"
Type: filesandordirs; Name: "{localappdata}\XiaoZhiPC"

[Code]
function InitializeUninstall(): Boolean;
begin
 Result := MsgBox(
   'Gỡ Tiểu Trí sẽ xóa cả dữ liệu thiết bị và liên kết lưu trên máy này.'+#13#10+
   'Nếu cài lại, máy sẽ tạo mã thiết bị mới và cần liên kết lại trên xiaozhi.me.'+#13#10#13#10+
   'Bạn có muốn tiếp tục?',
   mbConfirmation, MB_YESNO) = IDYES;
end;
