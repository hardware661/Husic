param([switch]$LyricsOnly, [switch]$WindowOnly, [switch]$FontOnly, [switch]$RestoreOnly, [switch]$StartupOnly, [switch]$SecondaryOnly, [switch]$ResetOnly, [switch]$AudioOnly)
$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false)
& (Join-Path $PSScriptRoot 'build.ps1') -QA
if ($LASTEXITCODE -ne 0) { throw 'QA build failed' }
$musicRun = 'qa-' + [Guid]::NewGuid().ToString('N')
$musicReportFolder = Join-Path $PSScriptRoot ('build\' + $musicRun)
New-Item -ItemType Directory -Path $musicReportFolder -Force | Out-Null
$musicBuiltTestExe = Join-Path $PSScriptRoot 'build\qa-tools\Music.QA.exe'
$musicTestHash = (Get-FileHash $musicBuiltTestExe -Algorithm SHA256).Hash.ToLowerInvariant()
# Start the hash-verified QA executable from a local, isolated directory.
# Running executable code through the WSL UNC share otherwise asks Windows for
# a file-security confirmation on every phase. No security setting is changed.
$musicLocalTools = Join-Path ([Environment]::GetFolderPath('LocalApplicationData')) ('Music\QA\Tools\' + $musicTestHash.Substring(0, 16))
New-Item -ItemType Directory -Path $musicLocalTools -Force | Out-Null
$musicTestExe = Join-Path $musicLocalTools 'Music.QA.exe'
if (-not (Test-Path $musicTestExe) -or (Get-FileHash $musicTestExe -Algorithm SHA256).Hash.ToLowerInvariant() -ne $musicTestHash) {
    Copy-Item -LiteralPath $musicBuiltTestExe -Destination $musicTestExe -Force
}
if ((Get-FileHash $musicTestExe -Algorithm SHA256).Hash.ToLowerInvariant() -ne $musicTestHash) { throw 'Local QA executable checksum mismatch' }
$musicPhases = if($AudioOnly){@('audio')} elseif($ResetOnly){@('reset-seed','reset-verify')} elseif($SecondaryOnly){@('secondary','secondary-reopen')} elseif($StartupOnly){@('startup')} elseif($RestoreOnly){@('restore')} elseif ($FontOnly) { @('font','font-reopen') } elseif ($WindowOnly) { @('window','window-reopen') } elseif ($LyricsOnly) { @('lyrics','lyrics-reopen') } else { @('first','reopen') }
$musicPreviousWebSource = [Environment]::GetEnvironmentVariable('HUSIC_QA_WEB_SOURCE', 'Process')
try {
    $env:HUSIC_QA_WEB_SOURCE = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..\web\src'))
    foreach ($musicPhase in $musicPhases) {
    $musicReport = Join-Path $musicReportFolder ($musicPhase + '.json')
    $musicProcess = Start-Process -FilePath $musicTestExe -WorkingDirectory $musicLocalTools -ArgumentList @(('"' + $musicReport + '"'), $musicPhase) -PassThru
    if (-not $musicProcess.WaitForExit(45000)) {
        # This process object is the exact QA instance just started here.
        $musicProcess.Kill()
        throw ('Native QA timed out in phase ' + $musicPhase)
    }
    if (-not (Test-Path $musicReport)) { throw ('Native QA wrote no report in phase ' + $musicPhase) }
    $musicResult = Get-Content $musicReport -Raw -Encoding UTF8 | ConvertFrom-Json
    if ($musicProcess.ExitCode -ne 0 -or -not $musicResult.ok) {
        $musicResult | ConvertTo-Json -Compress
        throw ('Native QA failed in phase ' + $musicPhase)
    }
    $musicResult | Add-Member -NotePropertyName phase -NotePropertyValue $musicPhase
    $musicResult | ConvertTo-Json -Compress
    Start-Sleep -Milliseconds 900
    }
} finally {
    [Environment]::SetEnvironmentVariable('HUSIC_QA_WEB_SOURCE', $musicPreviousWebSource, 'Process')
}
[ordered]@{ ok = $true; run = $musicRun; windowsNative = $true; testProfileIsolated = $true } | ConvertTo-Json -Compress
