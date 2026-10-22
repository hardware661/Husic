param([switch]$QA, [string]$ServerOrigin = 'http://127.0.0.1:18788', [string]$LoginOrigin = '', [string]$OutputDirectory = '')
$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false)
$musicRoot = $PSScriptRoot
$musicLock = Get-Content (Join-Path $musicRoot 'packages.lock.json') -Raw | ConvertFrom-Json
$musicPackage = $musicLock.'Microsoft.Web.WebView2'
$musicVendor = Join-Path $musicRoot ('vendor\webview2-' + $musicPackage.version)
$musicArchive = Join-Path $musicRoot ('vendor\microsoft.web.webview2.' + $musicPackage.version + '.nupkg')
if (-not (Test-Path $musicArchive)) { throw 'Run fetch-sdk.py first; dependencies are project-local.' }
if ((Get-FileHash $musicArchive -Algorithm SHA256).Hash.ToLowerInvariant() -ne $musicPackage.sha256) { throw 'SDK checksum mismatch' }
$musicCompiler = Join-Path $env:WINDIR 'Microsoft.NET\Framework64\v4.0.30319\csc.exe'
if (-not (Test-Path $musicCompiler)) { throw '.NET Framework C# compiler is unavailable' }
foreach ($musicOrigin in @($ServerOrigin, $LoginOrigin)) {
    if (-not $musicOrigin -and $musicOrigin -eq $LoginOrigin) { continue }
    $musicUri = [Uri]$musicOrigin
    if (-not $musicUri.IsAbsoluteUri -or $musicUri.UserInfo -or $musicUri.AbsolutePath -ne '/' -or $musicUri.Query -or $musicUri.Fragment -or
        -not ($musicUri.Scheme -eq 'https' -or ($musicUri.Scheme -eq 'http' -and $musicUri.IsLoopback))) { throw 'Use an HTTPS origin, or loopback HTTP.' }
}
if (-not $ServerOrigin) { throw 'ServerOrigin is required' }
$musicOutput = if ($OutputDirectory) { $OutputDirectory } elseif ($QA) { Join-Path $musicRoot 'build\qa-tools' } else { Join-Path $musicRoot 'dist' }
New-Item -ItemType Directory -Path $musicOutput -Force | Out-Null
$musicSettings = Join-Path $musicOutput 'build-settings.json'
[IO.File]::WriteAllText($musicSettings, (@{ ServerOrigin=$ServerOrigin.TrimEnd('/'); LoginOrigin=$LoginOrigin.TrimEnd('/') } | ConvertTo-Json -Compress), [Text.UTF8Encoding]::new($false))
$musicName = if ($QA) { 'Music.QA.exe' } else { 'Husic.exe' }
$musicExe = Join-Path $musicOutput $musicName
$musicCore = Join-Path $musicVendor 'lib\net462\Microsoft.Web.WebView2.Core.dll'
$musicForms = Join-Path $musicVendor 'lib\net462\Microsoft.Web.WebView2.WinForms.dll'
$musicLoader = Join-Path $musicVendor 'build\native\x64\WebView2Loader.dll'
$musicBrand = Join-Path $musicRoot '..\web\public\brand'
$musicArgs = @('/nologo', '/target:winexe', '/platform:x64', '/optimize+', '/utf8output', '/codepage:65001', '/langversion:5',
    ('/out:' + $musicExe), ('/win32manifest:' + (Join-Path $musicRoot 'app.manifest')),
    ('/win32icon:' + (Join-Path $musicBrand 'music.ico')),
    '/reference:System.dll', '/reference:System.Core.dll', '/reference:System.Drawing.dll',
    '/reference:System.Windows.Forms.dll', '/reference:System.Web.Extensions.dll', '/reference:System.Security.dll',
    ('/reference:' + $musicCore), ('/reference:' + $musicForms),
    ('/resource:' + $musicCore + ',Music.SDK.Microsoft.Web.WebView2.Core.dll'),
    ('/resource:' + $musicForms + ',Music.SDK.Microsoft.Web.WebView2.WinForms.dll'),
    ('/resource:' + $musicLoader + ',Music.SDK.WebView2Loader.dll'),
    ('/resource:' + (Join-Path $musicBrand 'music.ico') + ',Music.Brand.ico'),
    ('/resource:' + (Join-Path $musicBrand 'music-192.png') + ',Music.Brand.png'),
    ('/resource:' + (Join-Path $musicRoot 'desktop-bridge.js') + ',Music.DesktopBridge.js'),
    ('/resource:' + (Join-Path $musicRoot 'desktop-chrome.js') + ',Music.DesktopChrome.js'),
    ('/resource:' + (Join-Path $musicRoot 'desktop-chrome.css') + ',Music.DesktopChrome.css'),
    ('/resource:' + (Join-Path $musicRoot '..\LICENSE') + ',Music.LICENSE.txt'),
    ('/resource:' + $musicSettings + ',Music.BuildSettings.json'),
    ('/resource:' + (Join-Path $musicVendor 'LICENSE.txt') + ',Music.WebView2.LICENSE.txt'))
if ($QA) { $musicArgs += '/define:QA' }
$musicArgs += @(Get-ChildItem $musicRoot -Filter '*.cs' | ForEach-Object { $_.FullName })
& $musicCompiler $musicArgs
if ($LASTEXITCODE -ne 0) { throw ('C# build failed: ' + $LASTEXITCODE) }
$musicVersion = [Diagnostics.FileVersionInfo]::GetVersionInfo($musicExe)
[ordered]@{ artifact = $musicName; bytes = (Get-Item $musicExe).Length; sha256 = (Get-FileHash $musicExe -Algorithm SHA256).Hash.ToLowerInvariant(); version = $musicVersion.FileVersion; webviewSDK = $musicPackage.version; portable = $true } | ConvertTo-Json -Compress
