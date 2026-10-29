param([switch]$LyricsOnly, [switch]$WindowOnly, [switch]$FontOnly, [switch]$RestoreOnly, [switch]$StartupOnly, [switch]$SecondaryOnly, [switch]$ResetOnly, [switch]$AudioOnly)
$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false)
& (Join-Path $PSScriptRoot 'build.ps1') -QA
if ($LASTEXITCODE -ne 0) { throw 'QA build failed' }
$husicRun = 'qa-' + [Guid]::NewGuid().ToString('N')
$husicReportFolder = Join-Path $PSScriptRoot ('build\' + $husicRun)
New-Item -ItemType Directory -Path $husicReportFolder -Force | Out-Null
$husicBuiltTestExe = Join-Path $PSScriptRoot 'build\qa-tools\Husic.QA.exe'
$husicTestHash = (Get-FileHash $husicBuiltTestExe -Algorithm SHA256).Hash.ToLowerInvariant()
# Start the hash-verified QA executable from a local, isolated directory.
# Running executable code through the WSL UNC share otherwise asks Windows for
# a file-security confirmation on every phase. No security setting is changed.
$husicLocalTools = Join-Path ([Environment]::GetFolderPath('LocalApplicationData')) ('Husic\QA\Tools\' + $husicTestHash.Substring(0, 16))
New-Item -ItemType Directory -Path $husicLocalTools -Force | Out-Null
$husicTestExe = Join-Path $husicLocalTools 'Husic.QA.exe'
if (-not (Test-Path $husicTestExe) -or (Get-FileHash $husicTestExe -Algorithm SHA256).Hash.ToLowerInvariant() -ne $husicTestHash) {
    Copy-Item -LiteralPath $husicBuiltTestExe -Destination $husicTestExe -Force
}
if ((Get-FileHash $husicTestExe -Algorithm SHA256).Hash.ToLowerInvariant() -ne $husicTestHash) { throw 'Local QA executable checksum mismatch' }
$husicPhases = if($AudioOnly){@('audio')} elseif($ResetOnly){@('reset-seed','reset-verify')} elseif($SecondaryOnly){@('secondary','secondary-reopen')} elseif($StartupOnly){@('startup')} elseif($RestoreOnly){@('restore')} elseif ($FontOnly) { @('font','font-reopen') } elseif ($WindowOnly) { @('window','window-reopen') } elseif ($LyricsOnly) { @('lyrics','lyrics-reopen') } else { @('first','reopen') }
$husicPreviousWebSource = [Environment]::GetEnvironmentVariable('HUSIC_QA_WEB_SOURCE', 'Process')
try {
    $env:HUSIC_QA_WEB_SOURCE = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..\web\src'))
    foreach ($husicPhase in $husicPhases) {
    $husicReport = Join-Path $husicReportFolder ($husicPhase + '.json')
    $husicProcess = Start-Process -FilePath $husicTestExe -WorkingDirectory $husicLocalTools -ArgumentList @(('"' + $husicReport + '"'), $husicPhase) -PassThru
    if (-not $husicProcess.WaitForExit(45000)) {
        # This process object is the exact QA instance just started here.
        $husicProcess.Kill()
        throw ('Native QA timed out in phase ' + $husicPhase)
    }
    if (-not (Test-Path $husicReport)) { throw ('Native QA wrote no report in phase ' + $husicPhase) }
    $husicResult = Get-Content $husicReport -Raw -Encoding UTF8 | ConvertFrom-Json
    if ($husicProcess.ExitCode -ne 0 -or -not $husicResult.ok) {
        $husicResult | ConvertTo-Json -Compress
        throw ('Native QA failed in phase ' + $husicPhase)
    }
    $husicResult | Add-Member -NotePropertyName phase -NotePropertyValue $husicPhase
    $husicResult | ConvertTo-Json -Compress
    Start-Sleep -Milliseconds 900
    }
} finally {
    [Environment]::SetEnvironmentVariable('HUSIC_QA_WEB_SOURCE', $husicPreviousWebSource, 'Process')
}
[ordered]@{ ok = $true; run = $husicRun; windowsNative = $true; testProfileIsolated = $true } | ConvertTo-Json -Compress
