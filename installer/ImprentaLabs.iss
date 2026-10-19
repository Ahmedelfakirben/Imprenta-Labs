; Imprenta Labs — instalador (Inno Setup 6)
; Compilar:  ISCC.exe installer\ImprentaLabs.iss   ->  installer\Output\ImprentaLabs-Setup-2.0.0.exe

#define AppName "Imprenta Labs"
#define AppVersion "2.0.0"
#define AppPublisher "Vektor Labs"
#define AppExe "ImprentaLabs.exe"

[Setup]
AppId={{8B2F6C1E-5D3A-4B7E-9A41-7C0E1F2A9D10}
AppName={#AppName}
AppVersion={#AppVersion}
AppPublisher={#AppPublisher}
DefaultDirName={autopf}\{#AppName}
DefaultGroupName={#AppName}
DisableProgramGroupPage=yes
OutputDir=Output
OutputBaseFilename=ImprentaLabs-Setup-{#AppVersion}
SetupIconFile=..\assets\icon.ico
UninstallDisplayIcon={app}\{#AppExe}
WizardImageFile=wizard-large.bmp
WizardSmallImageFile=wizard-small.bmp
Compression=lzma2/max
SolidCompression=yes
PrivilegesRequired=admin
ArchitecturesInstallIn64BitMode=x64compatible
ArchitecturesAllowed=x64compatible
AppMutex=Global\ImprentaLabsAgent
CloseApplications=yes
WizardStyle=modern

[Languages]
Name: "spanish"; MessagesFile: "compiler:Languages\Spanish.isl"
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "startup"; Description: "Iniciar {#AppName} con Windows (recomendado)"; GroupDescription: "Opciones:"
Name: "desktopicon"; Description: "Crear acceso directo en el escritorio"; GroupDescription: "Opciones:"; Flags: unchecked

[Files]
Source: "..\{#AppExe}"; DestDir: "{app}"; Flags: ignoreversion
Source: "trusted_keys.json"; DestDir: "{app}"; Flags: ignoreversion skipifsourcedoesntexist
Source: "..\client\imprenta-labs.js"; DestDir: "{app}\client"; Flags: ignoreversion
Source: "..\README.md"; DestDir: "{app}"; Flags: ignoreversion skipifsourcedoesntexist

[Icons]
Name: "{group}\{#AppName}"; Filename: "{app}\{#AppExe}"
Name: "{group}\Desinstalar {#AppName}"; Filename: "{uninstallexe}"
Name: "{autodesktop}\{#AppName}"; Filename: "{app}\{#AppExe}"; Tasks: desktopicon

[Registry]
Root: HKLM; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; ValueType: string; ValueName: "ImprentaLabs"; ValueData: """{app}\{#AppExe}"""; Flags: uninsdeletevalue; Tasks: startup

[Run]
Filename: "{app}\{#AppExe}"; Description: "Iniciar {#AppName} ahora"; Flags: nowait postinstall skipifsilent runasoriginaluser

[UninstallRun]
Filename: "taskkill.exe"; Parameters: "/F /IM {#AppExe}"; Flags: runhidden; RunOnceId: "KillAgent"

[Code]
var
  SitePage: TInputQueryWizardPage;

procedure InitializeWizard;
begin
  SitePage := CreateInputQueryPage(wpSelectTasks,
    'Sitio web del ERP',
    '¿Desde qué sitio se va a imprimir?',
    'Escribe la dirección de tu ERP para que pueda imprimir sin preguntar. ' +
    'Puedes dejarlo vacío y permitirlo después, cuando aparezca el aviso.' + #13#10 +
    'Ejemplos:  https://erp.miempresa.com   o   https://*.miempresa.com');
  SitePage.Add('Dirección del ERP:', False);
end;

function JsonEscape(const S: String): String;
begin
  Result := S;
  StringChangeEx(Result, '\', '\\', True);
  StringChangeEx(Result, '"', '\"', True);
end;

procedure CurStepChanged(CurStep: TSetupStep);
var
  Site, Json: String;
begin
  if CurStep = ssPostInstall then
  begin
    Site := Trim(SitePage.Values[0]);
    while (Length(Site) > 0) and (Site[Length(Site)] = '/') do
      Delete(Site, Length(Site), 1);
    if Site <> '' then
      Json := '{' + #13#10 + '  "allowed_origins": ["' + JsonEscape(Site) + '"],' + #13#10
    else
      Json := '{' + #13#10 + '  "allowed_origins": [],' + #13#10;
    Json := Json + '  "width_dots": 576,' + #13#10 +
                   '  "retry_every_seconds": 15,' + #13#10 +
                   '  "max_age_hours": 24' + #13#10 + '}' + #13#10;
    SaveStringToFile(ExpandConstant('{app}\config.json'), Json, False);
  end;
end;
