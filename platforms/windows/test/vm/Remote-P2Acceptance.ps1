# P2-44 remote acceptance runner.
#
# Runs inside the logged-in interactive desktop as an elevated scheduled task.
# It preflights the VM, extracts the supplied bundle, runs acceptance-test.ps1
# (which installs and restores the test build), and writes environment.json,
# remote-summary.json and failure screenshots under -WorkRoot.
#
# It never edits product code and never kills the user's daily engine.
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$WorkRoot,
    [string]$ResultDir,
    [string]$Suite = 'p2-mixed,wpf,notepad,terminal,browser,webview2,search,wpf32,native32',
    [string]$BundleZip,
    [string]$BundleSha256,
    [switch]$PreflightOnly,
    [int]$TimeoutMinutes = 150
)
$ErrorActionPreference = 'Stop'
$identity = [Security.Principal.WindowsIdentity]::GetCurrent()
$logs = Join-Path $WorkRoot 'logs'
$validationRoot = if ($ResultDir) { $ResultDir } else { Join-Path $WorkRoot 'validation' }
$logPath = Join-Path $logs 'remote.log'
$summaryPath = Join-Path $logs 'remote-summary.json'
$environmentPath = Join-Path $logs 'environment.json'
$errors = New-Object System.Collections.Generic.List[string]
$skipped = New-Object System.Collections.Generic.List[string]

function Log([string]$text) {
    $line = ('{0} {1}' -f (Get-Date).ToString('HH:mm:ss'), $text)
    Add-Content -LiteralPath $logPath -Value $line -Encoding UTF8
    Write-Host $line
}
function Write-Json([string]$path, $value) {
    New-Item -ItemType Directory -Force -Path (Split-Path -Parent $path) | Out-Null
    $value | ConvertTo-Json -Depth 12 | Set-Content -LiteralPath $path -Encoding UTF8
}
function Test-Admin {
    $principal = [Security.Principal.WindowsPrincipal]::new($identity)
    return $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
}
function File-Version([string]$path) {
    if ($path -and (Test-Path -LiteralPath $path)) {
        try { return (Get-Item -LiteralPath $path).VersionInfo.FileVersion } catch { return 'unknown' }
    }
    return ''
}
function Registry-Version([string]$path, [string]$name) {
    try {
        $item = Get-ItemProperty -LiteralPath $path -ErrorAction Stop
        return [string]$item.$name
    } catch { return '' }
}

Add-Type @'
using System;
using System.Runtime.InteropServices;
public static class P2SessionState {
    [DllImport("wtsapi32.dll", SetLastError=true)]
    static extern bool WTSQuerySessionInformation(IntPtr server, int sessionId, int infoClass, out IntPtr buffer, out int bytes);
    [DllImport("wtsapi32.dll")]
    static extern void WTSFreeMemory(IntPtr buffer);
    public static int ConnectState(int sessionId) {
        IntPtr buffer; int bytes;
        // WTSConnectState = 8; WTSActive = 0, WTSConnected = 1, WTSDisconnected = 4.
        if (!WTSQuerySessionInformation(IntPtr.Zero, sessionId, 8, out buffer, out bytes)) return -1;
        try { return Marshal.ReadInt32(buffer); } finally { WTSFreeMemory(buffer); }
    }
}
'@

function Get-Preflight {
    $os = Get-CimInstance Win32_OperatingSystem
    $build = [int]$os.BuildNumber
    $sessionId = [Diagnostics.Process]::GetCurrentProcess().SessionId
    $state = [P2SessionState]::ConnectState($sessionId)
    $explorer = @(Get-Process explorer -ErrorAction SilentlyContinue)
    $currentExplorer = @($explorer | Where-Object { $_.SessionId -eq $sessionId })
    $admin = Test-Admin
    $programFilesX86 = [Environment]::GetFolderPath('ProgramFilesX86')
    $wt = Join-Path $env:LOCALAPPDATA 'Microsoft\WindowsApps\wt.exe'
    $edge = Join-Path $programFilesX86 'Microsoft\Edge\Application\msedge.exe'
    $notepad = Join-Path $env:WINDIR 'System32\notepad.exe'
    $syswow64 = Join-Path $env:WINDIR 'SysWOW64\WindowsPowerShell\v1.0\powershell.exe'
    $searchSandbox = Join-Path $env:LOCALAPPDATA 'Packages\MicrosoftWindows.Client.CBS_cw5n1h2txyewy\AC'
    $webview = @(
        (Registry-Version 'HKLM:\SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}' 'pv'),
        (Registry-Version 'HKLM:\SOFTWARE\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}' 'pv')
    ) | Where-Object { $_ } | Select-Object -First 1
    $apps = [ordered]@{
        notepad = [ordered]@{ path = $notepad; exists = (Test-Path -LiteralPath $notepad); version = (File-Version $notepad) }
        terminal = [ordered]@{ path = $wt; exists = (Test-Path -LiteralPath $wt); version = (File-Version $wt) }
        edge = [ordered]@{ path = $edge; exists = (Test-Path -LiteralPath $edge); version = (File-Version $edge) }
        webview2 = [ordered]@{ runtime_version = [string]$webview; exists = [bool]$webview }
        search_box = [ordered]@{ available = ($build -ge 22000 -and (Test-Path -LiteralPath $searchSandbox)); sandbox = $searchSandbox }
        syswow64_powershell = [ordered]@{ path = $syswow64; exists = (Test-Path -LiteralPath $syswow64) }
    }
    $engine = @(Get-Process 0c-server, 0c-ui, zuin-server, zuin-ui -ErrorAction SilentlyContinue)
    $disk = Get-CimInstance Win32_LogicalDisk -Filter "DeviceID='C:'"
    $freeGb = if ($disk) { [math]::Round($disk.FreeSpace / 1GB, 1) } else { 0 }
    $blockers = New-Object System.Collections.Generic.List[string]
    if (-not $admin) { $blockers.Add('the interactive task is not elevated') }
    if (-not [Environment]::UserInteractive) { $blockers.Add('the task is not running in an interactive window station') }
    if ($currentExplorer.Count -eq 0) { $blockers.Add('no explorer.exe in the interactive session') }
    if ($state -ne 0) { $blockers.Add("the interactive session is not active (WTS state $state); unlock the VM screen") }
    if (-not $apps.notepad.exists) { $blockers.Add('notepad.exe is missing') }
    if (-not $apps.terminal.exists) { $blockers.Add('Windows Terminal (wt.exe) is missing') }
    if (-not $apps.edge.exists) { $blockers.Add('Microsoft Edge is missing') }
    if (-not $apps.syswow64_powershell.exists) { $blockers.Add('the 32-bit PowerShell is missing') }
    if (-not $apps.webview2.exists) { Log 'webview2 runtime missing; the webview2 suite is expected to skip' }
    if ($build -lt 22000) { Log 'Windows 10: the Windows 11 search box suite will be skipped' }
    if ($engine.Count -gt 0) {
        $names = ($engine | ForEach-Object { "$($_.ProcessName)($($_.Id))" }) -join ', '
        $blockers.Add("close the daily 0c engine/candidate UI before acceptance: $names")
    }
    if ($freeGb -lt 5) { $blockers.Add("less than 5 GB free on C: ($freeGb GB)") }
    return [ordered]@{
        schema = 1
        when = (Get-Date).ToString('o')
        computer = $env:COMPUTERNAME
        user = $identity.Name
        os = $os.Caption
        version = $os.Version
        build = $build
        arch = $env:PROCESSOR_ARCHITECTURE
        powershell = $PSVersionTable.PSVersion.ToString()
        admin = $admin
        session_id = $sessionId
        session_state = $state
        interactive = ($state -eq 0 -and $currentExplorer.Count -gt 0)
        explorer = @($currentExplorer | ForEach-Object { $_.Id })
        free_gb = $freeGb
        apps = $apps
        engine_processes = @($engine | ForEach-Object { [ordered]@{ name = $_.ProcessName; id = $_.Id; path = $_.Path } })
        preflight_ok = ($blockers.Count -eq 0)
        blockers = @($blockers)
    }
}

function Get-FileList([string]$root) {
    if (-not (Test-Path -LiteralPath $root)) { return @() }
    return @(Get-ChildItem -LiteralPath $root -Recurse -File | ForEach-Object { $_.FullName })
}

function Get-SuiteResults([string]$root) {
    if (-not (Test-Path -LiteralPath $root)) { return @() }
    $out = @()
    foreach ($file in (Get-ChildItem -LiteralPath $root -Recurse -File -Filter '*-result.json' | Sort-Object FullName)) {
        if ($file.Name -in @('acceptance-result.json', 'vm-run-summary.json')) { continue }
        try {
            $data = Get-Content -LiteralPath $file.FullName -Raw -Encoding UTF8 | ConvertFrom-Json
            $out += [ordered]@{
                suite = $file.Directory.Name
                file = $file.FullName
                passed = [bool]$data.passed
                skipped = [string]$data.skipped
                error = [string]$data.error
                checks = @($data.checks).Count
            }
        } catch {
            $out += [ordered]@{ suite = $file.Directory.Name; file = $file.FullName; passed = $false; error = $_.Exception.Message; checks = 0 }
        }
    }
    return $out
}

New-Item -ItemType Directory -Force -Path $logs | Out-Null
New-Item -ItemType Directory -Force -Path $validationRoot | Out-Null

$environment = $null
$bundle = $null
$acceptance = $null
$fatal = ''
$started = Get-Date
$summary = [ordered]@{
    schema = 1
    task = 'P2-44'
    when = $started.ToString('o')
    computer = $env:COMPUTERNAME
    work_root = $WorkRoot
    preflight_only = [bool]$PreflightOnly
    environment_file = $environmentPath
    acceptance_exit = $null
    acceptance_result = $null
    suite_skipped = @()
    suites = @()
    screenshots = @()
    errors = @()
    fatal = ''
    passed = $false
}
try {
    Log 'preflight'
    $environment = Get-Preflight
    Write-Json $environmentPath $environment
    if ($PreflightOnly) {
        $summary.passed = [bool]$environment.preflight_ok
        Log ('preflight ok={0}' -f $summary.passed)
    } else {
        if (-not $environment.preflight_ok) {
            throw ('preflight failed: ' + (($environment.blockers) -join '; '))
        }
        if (-not $BundleZip) { throw 'BundleZip is required for a full acceptance run' }
        if (-not (Test-Path -LiteralPath $BundleZip)) { throw "bundle not found: $BundleZip" }
        $actual = (Get-FileHash -LiteralPath $BundleZip -Algorithm SHA256).Hash.ToLowerInvariant()
        if ($BundleSha256 -and $actual -ne $BundleSha256.ToLowerInvariant()) {
            throw "bundle SHA-256 mismatch: expected $BundleSha256, got $actual"
        }
        $bundleDir = Join-Path $WorkRoot 'bundle'
        if (Test-Path -LiteralPath $bundleDir) { Remove-Item -LiteralPath $bundleDir -Recurse -Force }
        Log "extracting bundle $actual"
        Expand-Archive -LiteralPath $BundleZip -DestinationPath $bundleDir -Force
        $release = Get-ChildItem -LiteralPath $bundleDir -Recurse -File -Filter 'release.json' | Select-Object -First 1
        if (-not $release) { throw 'bundle has no release.json' }
        $bundleRoot = $release.Directory.FullName
        $releaseData = Get-Content -LiteralPath $release.FullName -Raw -Encoding UTF8 | ConvertFrom-Json
        $bundle = [ordered]@{
            zip = $BundleZip
            sha256 = $actual
            root = $bundleRoot
            version = $releaseData.version
            channel = $releaseData.channel
            os = $releaseData.os
            arch = $releaseData.arch
        }
        Log ('bundle {0} {1}/{2}' -f $releaseData.version, $releaseData.os, $releaseData.arch)

        if (Test-Path -LiteralPath $validationRoot) { Remove-Item -LiteralPath $validationRoot -Recurse -Force }
        New-Item -ItemType Directory -Force -Path $validationRoot | Out-Null
        $acceptanceResult = Join-Path $validationRoot ('acceptance-' + (Get-Date -Format 'yyyyMMdd-HHmmss'))
        $suiteList = @($Suite.Split(',') | Where-Object { $_ })
        if ($environment.build -lt 22000 -and ($suiteList -contains 'search')) {
            $suiteList = @($suiteList | Where-Object { $_ -ne 'search' })
            $summary.suite_skipped += [ordered]@{ suite = 'search'; reason = 'Windows 11 search box required' }
        }
        $acceptanceScript = Join-Path $PSScriptRoot '..\acceptance-test.ps1'
        $acceptanceScript = (Resolve-Path -LiteralPath $acceptanceScript).Path
        $stdout = Join-Path $logs 'acceptance.out.log'
        $stderr = Join-Path $logs 'acceptance.err.log'
        $arguments = '-NoProfile -STA -ExecutionPolicy Bypass -File "' + $acceptanceScript + '" -BundleDir "' + $bundleRoot + '" -Suite ' + ($suiteList -join ',') + ' -ResultDir "' + $acceptanceResult + '"'
        Log ('acceptance: ' + $arguments)
        $process = Start-Process -FilePath (Join-Path $env:WINDIR 'System32\WindowsPowerShell\v1.0\powershell.exe') `
            -ArgumentList $arguments -PassThru -NoNewWindow -RedirectStandardOutput $stdout -RedirectStandardError $stderr
        $deadline = (Get-Date).AddMinutes($TimeoutMinutes)
        while (-not $process.HasExited -and (Get-Date) -lt $deadline) {
            Start-Sleep -Seconds 2
            $process.Refresh()
        }
        if (-not $process.HasExited) {
            Stop-Process -Id $process.Id -Force -ErrorAction SilentlyContinue
            throw "acceptance timed out after $TimeoutMinutes minutes; inspect leftover test windows before rerunning"
        }
        $summary.acceptance_exit = $process.ExitCode
        $summary.acceptance_result = $acceptanceResult
        $summary.suites = @(Get-SuiteResults $acceptanceResult)
        $summary.screenshots = @(Get-FileList $acceptanceResult | Where-Object { $_ -match '\.png$' })
        $acceptanceReport = Join-Path $acceptanceResult 'acceptance-result.json'
        if (Test-Path -LiteralPath $acceptanceReport) {
            $report = Get-Content -LiteralPath $acceptanceReport -Raw -Encoding UTF8 | ConvertFrom-Json
            $summary.acceptance_error = [string]$report.error
            $summary.acceptance_restored = $report.restored
        }
        $summary.passed = ($process.ExitCode -eq 0)
        Log ('acceptance exit {0}; {1} suite result(s)' -f $process.ExitCode, $summary.suites.Count)
    }
} catch {
    $fatal = $_.Exception.ToString()
    $errors.Add($fatal)
    $summary.fatal = $fatal
    Log ('FATAL: ' + $fatal)
} finally {
    $tsfLog = Join-Path $env:LOCALAPPDATA 'Zuin\tsf.log'
    if (Test-Path -LiteralPath $tsfLog) {
        try { Copy-Item -LiteralPath $tsfLog -Destination (Join-Path $logs 'tsf.log') -Force } catch {}
    }
    $failureShot = Join-Path $logs 'desktop-failure.png'
    if (-not $PreflightOnly -and -not $summary.passed -and -not (Test-Path -LiteralPath $failureShot)) {
        try { & (Join-Path $PSScriptRoot 'Capture-Desktop.ps1') -Path $failureShot } catch { Log ('desktop screenshot failed: ' + $_.Exception.Message) }
    }
    if (Test-Path -LiteralPath $failureShot) { $summary.screenshots = @($summary.screenshots) + $failureShot }
    $summary.errors = @($errors)
    $summary.seconds = [int]((Get-Date) - $started).TotalSeconds
    $summary.environment = $environment
    $summary.bundle = $bundle
    $summary.when_finished = (Get-Date).ToString('o')
    Write-Json $summaryPath $summary
    Log ('wrote ' + $summaryPath)
}
# The local harness decides pass/fail from environment.json and the suite
# results. A completed run exits 0 even when acceptance found a product bug.
exit 0
