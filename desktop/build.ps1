param([switch]$QA, [string]$ServerOrigin = 'http://127.0.0.1:18788', [string]$LoginOrigin = '', [string]$OutputDirectory = '')
$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false)
$husicRoot = $PSScriptRoot
$husicLock = Get-Content (Join-Path $husicRoot 'packages.lock.json') -Raw | ConvertFrom-Json
$husicPackage = $husicLock.'Microsoft.Web.WebView2'
$husicVendor = Join-Path $husicRoot ('vendor\webview2-' + $husicPackage.version)
$husicArchive = Join-Path $husicRoot ('vendor\microsoft.web.webview2.' + $husicPackage.version + '.nupkg')
if (-not (Test-Path $husicArchive)) { throw 'Run fetch-sdk.py first; dependencies are project-local.' }
if ((Get-FileHash $husicArchive -Algorithm SHA256).Hash.ToLowerInvariant() -ne $husicPackage.sha256) { throw 'SDK checksum mismatch' }
$husicCompiler = Join-Path $env:WINDIR 'Microsoft.NET\Framework64\v4.0.30319\csc.exe'
if (-not (Test-Path $husicCompiler)) { throw '.NET Framework C# compiler is unavailable' }
foreach ($husicOrigin in @($ServerOrigin, $LoginOrigin)) {
    if (-not $husicOrigin -and $husicOrigin -eq $LoginOrigin) { continue }
    $husicUri = [Uri]$husicOrigin
    if (-not $husicUri.IsAbsoluteUri -or $husicUri.UserInfo -or $husicUri.AbsolutePath -ne '/' -or $husicUri.Query -or $husicUri.Fragment -or
        -not ($husicUri.Scheme -eq 'https' -or ($husicUri.Scheme -eq 'http' -and $husicUri.IsLoopback))) { throw 'Use an HTTPS origin, or loopback HTTP.' }
}
if (-not $ServerOrigin) { throw 'ServerOrigin is required' }
$husicOutput = if ($OutputDirectory) { $OutputDirectory } elseif ($QA) { Join-Path $husicRoot 'build\qa-tools' } else { Join-Path $husicRoot 'dist' }
New-Item -ItemType Directory -Path $husicOutput -Force | Out-Null
$husicSettings = Join-Path $husicOutput 'build-settings.json'
[IO.File]::WriteAllText($husicSettings, (@{ ServerOrigin=$ServerOrigin.TrimEnd('/'); LoginOrigin=$LoginOrigin.TrimEnd('/') } | ConvertTo-Json -Compress), [Text.UTF8Encoding]::new($false))
$husicName = if ($QA) { 'Husic.QA.exe' } else { 'Husic.exe' }
$husicExe = Join-Path $husicOutput $husicName
$husicCore = Join-Path $husicVendor 'lib\net462\Microsoft.Web.WebView2.Core.dll'
$husicForms = Join-Path $husicVendor 'lib\net462\Microsoft.Web.WebView2.WinForms.dll'
$husicLoader = Join-Path $husicVendor 'build\native\x64\WebView2Loader.dll'
$husicBrand = Join-Path $husicRoot '..\web\public\brand'
$husicArgs = @('/nologo', '/target:winexe', '/platform:x64', '/optimize+', '/utf8output', '/codepage:65001', '/langversion:5',
    ('/out:' + $husicExe), ('/win32manifest:' + (Join-Path $husicRoot 'app.manifest')),
    ('/win32icon:' + (Join-Path $husicBrand 'husic.ico')),
    '/reference:System.dll', '/reference:System.Core.dll', '/reference:System.Drawing.dll',
    '/reference:System.Windows.Forms.dll', '/reference:System.Web.Extensions.dll', '/reference:System.Security.dll',
    ('/reference:' + $husicCore), ('/reference:' + $husicForms),
    ('/resource:' + $husicCore + ',Husic.SDK.Microsoft.Web.WebView2.Core.dll'),
    ('/resource:' + $husicForms + ',Husic.SDK.Microsoft.Web.WebView2.WinForms.dll'),
    ('/resource:' + $husicLoader + ',Husic.SDK.WebView2Loader.dll'),
    ('/resource:' + (Join-Path $husicBrand 'husic.ico') + ',Husic.Brand.ico'),
    ('/resource:' + (Join-Path $husicBrand 'husic-192.png') + ',Husic.Brand.png'),
    ('/resource:' + (Join-Path $husicRoot 'desktop-bridge.js') + ',Husic.DesktopBridge.js'),
    ('/resource:' + (Join-Path $husicRoot 'desktop-chrome.js') + ',Husic.DesktopChrome.js'),
    ('/resource:' + (Join-Path $husicRoot 'desktop-chrome.css') + ',Husic.DesktopChrome.css'),
    ('/resource:' + (Join-Path $husicRoot '..\LICENSE') + ',Husic.LICENSE.txt'),
    ('/resource:' + $husicSettings + ',Husic.BuildSettings.json'),
    ('/resource:' + (Join-Path $husicVendor 'LICENSE.txt') + ',Husic.WebView2.LICENSE.txt'))
if ($QA) { $husicArgs += '/define:QA' }
$husicArgs += @(Get-ChildItem $husicRoot -Filter '*.cs' | ForEach-Object { $_.FullName })
& $husicCompiler $husicArgs
if ($LASTEXITCODE -ne 0) { throw ('C# build failed: ' + $LASTEXITCODE) }
$husicVersion = [Diagnostics.FileVersionInfo]::GetVersionInfo($husicExe)
[ordered]@{ artifact = $husicName; bytes = (Get-Item $husicExe).Length; sha256 = (Get-FileHash $husicExe -Algorithm SHA256).Hash.ToLowerInvariant(); version = $husicVersion.FileVersion; webviewSDK = $husicPackage.version; portable = $true } | ConvertTo-Json -Compress
