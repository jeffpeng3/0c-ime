# T29b：從 WSL 呼叫 Windows PowerShell 5.1，執行不需要互動桌面的檢查。
# 只讀取登錄與檔案；不安裝、不註冊 TSF、不寫 HKLM、不建立排程工作，也不需要系統管理員。
# 一般模式會把 T29 與 T28 需要的 .ps1 複製到 <ResultRoot>\scripts，再逐項用
# Windows PowerShell 5.1 子行程執行，stdout／stderr／exit code 與 summary 寫到
# <ResultRoot>\results\<timestamp>。直接用法（在 WSL）：
#   /mnt/c/Windows/System32/WindowsPowerShell/v1.0/powershell.exe -NoProfile -NonInteractive `
#     -ExecutionPolicy Bypass -File ./run-from-wsl.ps1
[CmdletBinding()]
param(
    [string]$RepoRoot,
    [string]$ResultRoot = 'E:\freerun\0c-wincheck',
    [string]$ArtifactDir,
    [string]$DataDir,
    [string]$RepoRevision,
    [string]$InternalCheck,
    [string]$ScriptDir,
    [string]$CheckDir
)

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$script:Utf8NoBom = New-Object System.Text.UTF8Encoding($false)
$script:UsageError = $false
try { [Console]::OutputEncoding = $script:Utf8NoBom } catch { }
$OutputEncoding = $script:Utf8NoBom

$Clsid = '{138C35D0-7F08-4CEF-BA90-C34C16301528}'
$PsExe = Join-Path $PSHOME 'powershell.exe'
$SelfPath = $PSCommandPath
if (-not $SelfPath) { $SelfPath = $MyInvocation.MyCommand.Path }

function Write-Utf8File {
    param([string]$Path, [string]$Text)
    $parent = Split-Path -Parent $Path
    if ($parent -and -not (Test-Path -LiteralPath $parent)) {
        New-Item -ItemType Directory -Force -Path $parent | Out-Null
    }
    [IO.File]::WriteAllText($Path, [string]$Text, $script:Utf8NoBom)
}

# 參數、路徑或執行環境的問題回 exit 2；未預期的例外才是 exit 3。
function Stop-WithUsageError {
    param([string]$Message)
    $script:UsageError = $true
    throw $Message
}

function Read-JsonFile {
    param([string]$Path)
    if (-not (Test-Path -LiteralPath $Path)) { return $null }
    try { return (Get-Content -LiteralPath $Path -Raw -Encoding UTF8 | ConvertFrom-Json) } catch { return $null }
}

# WSL 路徑（/mnt/e/...）轉成 Windows 路徑；已經是 Windows 或 UNC 路徑時原樣回傳。
function ConvertTo-WindowsPath {
    param([string]$Path)
    if ([string]::IsNullOrWhiteSpace($Path)) { return $Path }
    if ($Path -match '^[A-Za-z]:[\\/]') { return $Path }
    if ($Path -match '^\\\\') { return $Path }
    if ($Path -match '^/mnt/([A-Za-z])(?:/(.*))?$') {
        $drive = $Matches[1].ToUpperInvariant()
        $rest = ''
        if ($null -ne $Matches[2]) { $rest = $Matches[2].Replace('/', '\') }
        return ($drive + ':\' + $rest)
    }
    if ($Path.StartsWith('/')) {
        $wsl = Join-Path $env:WINDIR 'System32\wsl.exe'
        if (Test-Path -LiteralPath $wsl) {
            $converted = & $wsl wslpath -w $Path 2>$null
            if ($LASTEXITCODE -eq 0 -and $converted) {
                return [string]($converted | Select-Object -First 1)
            }
        }
        Stop-WithUsageError "無法把 WSL 路徑轉成 Windows 路徑：$Path（請改傳 Windows 路徑或 /mnt/<磁碟>/…）"
    }
    return $Path
}

# .NET Framework 沒有 ProcessStartInfo.ArgumentList，這裡只處理本 runner 會用到的參數。
function Quote-Argument {
    param([string]$Value)
    if ($null -eq $Value -or $Value -eq '') { return '""' }
    if ($Value -match '[\s"]') { return '"' + $Value.Replace('"', '\"') + '"' }
    return $Value
}

function Invoke-CapturedProcess {
    param(
        [string]$FilePath,
        [string[]]$Arguments,
        [string]$WorkingDirectory,
        [int]$TimeoutSeconds = 300
    )
    $argLine = (@($Arguments) | ForEach-Object { Quote-Argument ([string]$_) }) -join ' '
    $commandLine = $FilePath + ' ' + $argLine
    $psi = New-Object System.Diagnostics.ProcessStartInfo
    $psi.FileName = $FilePath
    $psi.Arguments = $argLine
    $psi.WorkingDirectory = $WorkingDirectory
    $psi.UseShellExecute = $false
    $psi.CreateNoWindow = $true
    $psi.RedirectStandardOutput = $true
    $psi.RedirectStandardError = $true
    $psi.StandardOutputEncoding = $script:Utf8NoBom
    $psi.StandardErrorEncoding = $script:Utf8NoBom
    $process = New-Object System.Diagnostics.Process
    $process.StartInfo = $psi
    $watch = [Diagnostics.Stopwatch]::StartNew()
    $started = $true
    $startError = ''
    try { [void]$process.Start() } catch { $started = $false; $startError = $_.Exception.Message }
    if (-not $started) {
        $watch.Stop()
        return [pscustomobject]@{
            ExitCode = $null; TimedOut = $false; StartError = $startError
            Stdout = ''; Stderr = $startError; DurationMs = [int]$watch.ElapsedMilliseconds
            CommandLine = $commandLine
        }
    }
    $outTask = $process.StandardOutput.ReadToEndAsync()
    $errTask = $process.StandardError.ReadToEndAsync()
    $timedOut = $false
    if (-not $process.WaitForExit($TimeoutSeconds * 1000)) {
        $timedOut = $true
        try {
            & (Join-Path $env:WINDIR 'System32\taskkill.exe') /PID $process.Id /T /F 2>$null | Out-Null
        } catch { }
        try { [void]$process.WaitForExit(5000) } catch { }
    }
    try { $process.WaitForExit() } catch { }
    $stdout = ''
    $stderr = ''
    try { $stdout = $outTask.Result } catch { }
    try { $stderr = $errTask.Result } catch { }
    $watch.Stop()
    $exitCode = $null
    if (-not $timedOut) {
        try { $exitCode = $process.ExitCode } catch { $exitCode = $null }
    }
    try { $process.Dispose() } catch { }
    return [pscustomobject]@{
        ExitCode = $exitCode; TimedOut = $timedOut; StartError = ''
        Stdout = $stdout; Stderr = $stderr; DurationMs = [int]$watch.ElapsedMilliseconds
        CommandLine = $commandLine
    }
}

function Save-Capture {
    param([string]$Dir, $Capture)
    if (-not (Test-Path -LiteralPath $Dir)) { New-Item -ItemType Directory -Force -Path $Dir | Out-Null }
    Write-Utf8File (Join-Path $Dir 'stdout.txt') ([string]$Capture.Stdout)
    Write-Utf8File (Join-Path $Dir 'stderr.txt') ([string]$Capture.Stderr)
    $exitText = 'unknown'
    if ($Capture.TimedOut) { $exitText = 'timeout' }
    elseif ($null -ne $Capture.ExitCode) { $exitText = [string]$Capture.ExitCode }
    Write-Utf8File (Join-Path $Dir 'exit-code.txt') ($exitText + "`r`n")
    Write-Utf8File (Join-Path $Dir 'command.txt') ([string]$Capture.CommandLine + "`r`n")
}

function Add-RunLog {
    param([string]$Line)
    if (-not $script:LogPath) { return }
    $text = ('{0} {1}{2}' -f (Get-Date).ToString('HH:mm:ss'), $Line, "`r`n")
    [IO.File]::AppendAllText($script:LogPath, $text, $script:Utf8NoBom)
}

function Complete-InternalCheck {
    param([string]$Name, [string]$Status, [string]$Summary, $Details)
    $result = [ordered]@{
        schema  = 1
        check   = $Name
        status  = $Status
        when    = (Get-Date).ToString('o')
        summary = $Summary
        details = $Details
    }
    Write-Utf8File (Join-Path $CheckDir 'result.json') ($result | ConvertTo-Json -Depth 12)
    return $result
}

# T29 的 .ps1 都用 Parser::ParseFile 檢查；錯誤列出檔案、行號、欄號與訊息。
function Invoke-ParserCheck {
    $testDir = Join-Path $RepoRoot 'platforms\windows\test'
    if (-not (Test-Path -LiteralPath $testDir)) { throw "找不到 T29 腳本目錄：$testDir" }
    $files = New-Object System.Collections.ArrayList
    foreach ($file in (Get-ChildItem -LiteralPath $testDir -Filter 'ja-*.ps1' -File | Sort-Object Name)) {
        [void]$files.Add($file)
    }
    foreach ($name in @('acceptance-test.ps1', 'run-from-wsl.ps1')) {
        $path = Join-Path $testDir $name
        if (-not (Test-Path -LiteralPath $path)) { throw "缺少 T29 腳本：$path" }
        [void]$files.Add((Get-Item -LiteralPath $path))
    }
    $rows = @()
    $errorCount = 0
    foreach ($file in $files) {
        $tokens = $null
        $parseErrors = $null
        [void][System.Management.Automation.Language.Parser]::ParseFile($file.FullName, [ref]$tokens, [ref]$parseErrors)
        $errors = @($parseErrors | Where-Object { $null -ne $_ })
        $rows += [ordered]@{
            file         = $file.Name
            path         = $file.FullName
            tokens       = @($tokens).Count
            parse_errors = $errors.Count
        }
        if ($errors.Count -eq 0) {
            Write-Host ('OK   {0} ({1} tokens)' -f $file.Name, @($tokens).Count)
        } else {
            $errorCount += $errors.Count
            foreach ($error in $errors) {
                Write-Host ('{0}:{1}:{2}: {3}' -f $file.FullName, $error.Extent.StartLineNumber, $error.Extent.StartColumnNumber, $error.Message)
            }
        }
    }
    $details = [ordered]@{ files = $rows; errors = $errorCount }
    if ($errorCount -gt 0) {
        return (Complete-InternalCheck -Name 'parser' -Status 'fail' -Summary ('{0} 個 T29 腳本有 {1} 個剖析錯誤' -f $files.Count, $errorCount) -Details $details)
    }
    return (Complete-InternalCheck -Name 'parser' -Status 'pass' -Summary ('{0} 個 T29 腳本剖析通過' -f $files.Count) -Details $details)
}

# Dot-source 複本 ja-common.ps1 並呼叫 Initialize-JaProbe，確認 Add-Type 在 5.1 可編譯。
# GetForegroundWindow／Class 只查詢，不送按鍵、不切換 profile。
function Invoke-InteropCheck {
    $common = Join-Path $ScriptDir 'ja-common.ps1'
    if (-not (Test-Path -LiteralPath $common)) { throw "找不到 ja-common.ps1 複本：$common" }
    . $common
    Initialize-JaProbe
    $probeType = 'JaAcceptanceProbe' -as [type]
    if (-not $probeType) { throw 'Add-Type 沒有產生 JaAcceptanceProbe 型別' }
    $foreground = [JaAcceptanceProbe]::GetForegroundWindow()
    $zeroClass = [JaAcceptanceProbe]::Class([IntPtr]::Zero)
    Write-Host 'Add-Type 編譯成功：JaAcceptanceProbe'
    Write-Host ('GetForegroundWindow = {0}' -f $foreground.ToInt64())
    Write-Host ("Class(IntPtr.Zero) = '{0}'" -f $zeroClass)
    $details = [ordered]@{
        type               = $probeType.FullName
        assembly           = $probeType.Assembly.FullName
        foreground_window  = $foreground.ToInt64()
        zero_window_class  = $zeroClass
        interactive_input  = $false
    }
    return (Complete-InternalCheck -Name 'interop' -Status 'pass' -Summary 'Add-Type 可在 Windows PowerShell 5.1 編譯 JaAcceptanceProbe' -Details $details)
}

function Find-ToolPath {
    param([string]$Name, [string[]]$Fallbacks)
    $command = Get-Command $Name -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($command) { return $command.Source }
    foreach ($path in @($Fallbacks)) {
        if ($path -and (Test-Path -LiteralPath $path)) { return $path }
    }
    return $null
}

function Get-InstallState {
    $receiptPath = Join-Path $env:LOCALAPPDATA 'Zuin\install-receipt.json'
    $receipt = Read-JsonFile $receiptPath
    $inprocKey = "Registry::HKEY_CLASSES_ROOT\CLSID\$Clsid\InprocServer32"
    $dll = ''
    if (Test-Path -LiteralPath $inprocKey) {
        try { $dll = [string](Get-Item -LiteralPath $inprocKey).GetValue('') } catch { $dll = '' }
    }
    $hklmTip = "HKLM:\SOFTWARE\Microsoft\CTF\TIP\$Clsid"
    $hkcuTip = "HKCU:\Software\Microsoft\CTF\TIP\$Clsid"
    $hklmTipPresent = Test-Path -LiteralPath $hklmTip
    $hkcuTipPresent = Test-Path -LiteralPath $hkcuTip
    $receiptInfo = [ordered]@{}
    if ($receipt) {
        foreach ($name in @('version', 'channel', 'directory', 'arch', 'edition')) {
            if ($receipt.PSObject.Properties[$name]) { $receiptInfo[$name] = [string]$receipt.$name }
        }
    }
    return [ordered]@{
        installed      = [bool]($dll -or $hklmTipPresent -or $hkcuTipPresent -or $receipt)
        registered_dll = $dll
        hklm_tip       = $hklmTipPresent
        hkcu_tip       = $hkcuTipPresent
        receipt_path   = $receiptPath
        receipt        = $receiptInfo
    }
}

function Invoke-CapabilitiesCheck {
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = [Security.Principal.WindowsPrincipal]::new($identity)
    $admin = $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)

    $osVersion = [Environment]::OSVersion.Version
    $currentVersion = Get-ItemProperty -LiteralPath 'HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion' -ErrorAction SilentlyContinue
    $buildNumber = $osVersion.Build
    if ($currentVersion -and $currentVersion.CurrentBuildNumber) {
        $parsedBuild = 0
        if ([int]::TryParse([string]$currentVersion.CurrentBuildNumber, [ref]$parsedBuild)) { $buildNumber = $parsedBuild }
    }
    $productName = ''
    $displayVersion = ''
    $ubr = $null
    if ($currentVersion) {
        $productName = [string]$currentVersion.ProductName
        $displayVersion = [string]$currentVersion.DisplayVersion
        if ($null -ne $currentVersion.UBR) { $ubr = [int]$currentVersion.UBR }
    }
    $windows11 = $buildNumber -ge 22000
    $windowsLabel = $productName
    if (-not $windowsLabel) { $windowsLabel = 'Windows' }
    if ($displayVersion) { $windowsLabel += ' ' + $displayVersion }
    $windowsLabel += ' (build ' + $buildNumber + ')'

    $msbuildFallbacks = @()
    foreach ($root in @("$env:ProgramFiles\Microsoft Visual Studio", "${env:ProgramFiles(x86)}\Microsoft Visual Studio")) {
        if ($root -and (Test-Path -LiteralPath $root)) {
            $hit = Get-ChildItem -LiteralPath $root -Filter 'MSBuild.exe' -Recurse -Depth 5 -ErrorAction SilentlyContinue |
                Where-Object { $_.FullName -match '\\MSBuild\\Current\\Bin\\MSBuild.exe$' } |
                Select-Object -First 1
            if ($hit) { $msbuildFallbacks += $hit.FullName }
        }
    }
    $signtoolFallbacks = @()
    foreach ($root in @("${env:ProgramFiles(x86)}\Windows Kits\10\bin", "$env:ProgramFiles\Windows Kits\10\bin")) {
        if ($root -and (Test-Path -LiteralPath $root)) {
            $hit = Get-ChildItem -LiteralPath $root -Filter 'signtool.exe' -Recurse -Depth 3 -ErrorAction SilentlyContinue |
                Where-Object { $_.FullName -match '\\x64\\signtool\.exe$' } |
                Select-Object -First 1
            if ($hit) { $signtoolFallbacks += $hit.FullName }
        }
    }

    $tools = [ordered]@{}
    $tools['go'] = [ordered]@{
        found = $false
        path  = (Find-ToolPath 'go' @("$env:ProgramFiles\Go\bin\go.exe", "$env:LOCALAPPDATA\Programs\Go\bin\go.exe"))
    }
    $tools['go'].found = [bool]$tools['go'].path
    $tools['cargo'] = [ordered]@{
        found = $false
        path  = (Find-ToolPath 'cargo' @("$env:USERPROFILE\.cargo\bin\cargo.exe"))
    }
    $tools['cargo'].found = [bool]$tools['cargo'].path
    $tools['rustc'] = [ordered]@{
        found = $false
        path  = (Find-ToolPath 'rustc' @("$env:USERPROFILE\.cargo\bin\rustc.exe"))
    }
    $tools['rustc'].found = [bool]$tools['rustc'].path
    $tools['msbuild'] = [ordered]@{
        found = $false
        path  = (Find-ToolPath 'msbuild' $msbuildFallbacks)
    }
    $tools['msbuild'].found = [bool]$tools['msbuild'].path
    $tools['signtool'] = [ordered]@{
        found = $false
        path  = (Find-ToolPath 'signtool' $signtoolFallbacks)
    }
    $tools['signtool'].found = [bool]$tools['signtool'].path

    $extras = [ordered]@{
        dotnet = (Find-ToolPath 'dotnet' @("$env:ProgramFiles\dotnet\dotnet.exe"))
        git    = (Find-ToolPath 'git' @("$env:ProgramFiles\Git\cmd\git.exe"))
    }

    $missing = @()
    if (-not $tools['go'].found) { $missing += 'Go' }
    if (-not ($tools['cargo'].found -or $tools['rustc'].found)) { $missing += 'Rust' }
    if (-not $tools['msbuild'].found) { $missing += 'MSBuild' }
    if (-not $tools['signtool'].found) { $missing += 'signtool' }

    $install = Get-InstallState
    $adminText = if ($admin) { '管理員' } else { '非管理員' }
    $installText = if ($install.installed) { '0C 已安裝' } else { '0C 未安裝' }
    $toolsText = if ($missing.Count -eq 0) { 'Go／Rust／MSBuild／signtool 都可找到' } else { '缺少 ' + ($missing -join '、') }
    $summary = '{0}；{1}；{2}；{3}' -f $adminText, $installText, $windowsLabel, $toolsText

    Write-Host $summary
    Write-Host ('0C 註冊 DLL：{0}' -f $(if ($install.registered_dll) { $install.registered_dll } else { '（無）' }))
    foreach ($name in @('go', 'cargo', 'rustc', 'msbuild', 'signtool')) {
        $value = $tools[$name]
        if ($value.found) { Write-Host ('{0}: {1}' -f $name, $value.path) }
        else { Write-Host ('{0}: 未偵測到' -f $name) }
    }

    $details = [ordered]@{
        admin           = $admin
        identity        = $identity.Name
        windows         = [ordered]@{
            product_name  = $productName
            display_version = $displayVersion
            build         = $buildNumber
            ubr           = $ubr
            os_version    = [Environment]::OSVersion.VersionString
            windows_11    = $windows11
        }
        install         = $install
        tools           = $tools
        extras          = $extras
        registry_writes = $false
    }
    return (Complete-InternalCheck -Name 'capabilities' -Status 'pass' -Summary $summary -Details $details)
}

# 只做 CI artifact 的非互動 smoke：有資料包才跑 -check-data，沒有就只確認執行檔可啟動。
# 不註冊 TSF、不寫 HKLM、不碰個人詞庫。
function Invoke-ArtifactCheck {
    if ([string]::IsNullOrWhiteSpace($ArtifactDir)) {
        return (Complete-InternalCheck -Name 'artifact' -Status 'skip' -Summary '未指定 artifact 目錄；略過 CI artifact smoke（可用 -ArtifactDir 指定）' -Details ([ordered]@{ artifact_dir = $null }))
    }
    $artifact = ConvertTo-WindowsPath $ArtifactDir
    if (-not (Test-Path -LiteralPath $artifact)) { throw "找不到 artifact 目錄：$artifact" }
    $server = Get-ChildItem -LiteralPath $artifact -Recurse -File -Filter '0c-server.exe' -ErrorAction SilentlyContinue | Select-Object -First 1
    if (-not $server) { throw "artifact 裡沒有 0c-server.exe：$artifact" }
    $serverFile = Get-Item -LiteralPath $server.FullName
    $hash = (Get-FileHash -LiteralPath $serverFile.FullName -Algorithm SHA256).Hash

    $data = $null
    if ($DataDir) {
        $data = ConvertTo-WindowsPath $DataDir
        if (-not (Test-Path -LiteralPath $data)) { throw "找不到資料包目錄：$data" }
    } else {
        $candidate = Join-Path $serverFile.Directory.FullName 'data'
        if ((Test-Path -LiteralPath (Join-Path $candidate 'manifest.json')) -or (Test-Path -LiteralPath (Join-Path $candidate 'lexicon.tsv'))) {
            $data = $candidate
        }
    }

    $details = [ordered]@{
        artifact_dir = $artifact
        server       = $serverFile.FullName
        bytes        = $serverFile.Length
        sha256       = $hash
        file_version = $serverFile.VersionInfo.FileVersion
        data_dir     = $data
        check_data   = $null
        exe_probe    = $null
    }

    if ($data) {
        $capture = Invoke-CapturedProcess -FilePath $serverFile.FullName -Arguments @('-check-data', '-data', $data) -WorkingDirectory $serverFile.Directory.FullName -TimeoutSeconds 180
        Write-Utf8File (Join-Path $CheckDir 'check-data.stdout.txt') ([string]$capture.Stdout)
        Write-Utf8File (Join-Path $CheckDir 'check-data.stderr.txt') ([string]$capture.Stderr)
        $details.check_data = [ordered]@{
            exit_code   = $capture.ExitCode
            timed_out   = $capture.TimedOut
            duration_ms = $capture.DurationMs
        }
        Write-Host ('0c-server.exe -check-data exit={0}' -f $capture.ExitCode)
        if ($capture.Stdout) { Write-Host $capture.Stdout.TrimEnd() }
        if ($capture.Stderr) { [Console]::Error.WriteLine($capture.Stderr.TrimEnd()) }
        if ($capture.StartError) { throw ('0c-server.exe 無法啟動：' + $capture.StartError) }
        if ($capture.TimedOut) {
            return (Complete-InternalCheck -Name 'artifact' -Status 'fail' -Summary '-check-data 逾時' -Details $details)
        }
        if ($capture.ExitCode -ne 0) {
            return (Complete-InternalCheck -Name 'artifact' -Status 'fail' -Summary ('-check-data 失敗（exit code {0}）' -f $capture.ExitCode) -Details $details)
        }
        return (Complete-InternalCheck -Name 'artifact' -Status 'pass' -Summary 'artifact 的資料包可載入（-check-data）' -Details $details)
    }

    $probe = Invoke-CapturedProcess -FilePath $serverFile.FullName -Arguments @('-h') -WorkingDirectory $serverFile.Directory.FullName -TimeoutSeconds 30
    Write-Utf8File (Join-Path $CheckDir 'exe-probe.stdout.txt') ([string]$probe.Stdout)
    Write-Utf8File (Join-Path $CheckDir 'exe-probe.stderr.txt') ([string]$probe.Stderr)
    $details.exe_probe = [ordered]@{
        exit_code   = $probe.ExitCode
        timed_out   = $probe.TimedOut
        duration_ms = $probe.DurationMs
        start_error = $probe.StartError
    }
    if ($probe.StartError) {
        return (Complete-InternalCheck -Name 'artifact' -Status 'fail' -Summary ('0c-server.exe 無法啟動：' + $probe.StartError) -Details $details)
    }
    $probeOk = (-not $probe.TimedOut) -and (@(0, 2) -contains $probe.ExitCode)
    if (-not $probeOk) {
        return (Complete-InternalCheck -Name 'artifact' -Status 'fail' -Summary ('0c-server.exe -h 未正常結束（exit code {0}）' -f $probe.ExitCode) -Details $details)
    }
    return (Complete-InternalCheck -Name 'artifact' -Status 'skip' -Summary 'artifact 沒有資料包；0c-server.exe 可啟動，未執行 -check-data' -Details $details)
}

if ($InternalCheck) {
    if (-not $ScriptDir -or -not $CheckDir) {
        [Console]::Error.WriteLine('內部檢查需要 -ScriptDir 與 -CheckDir')
        exit 2
    }
    $RepoRoot = ConvertTo-WindowsPath $RepoRoot
    if (-not (Test-Path -LiteralPath $CheckDir)) { New-Item -ItemType Directory -Force -Path $CheckDir | Out-Null }
    $status = 'fail'
    $result = $null
    try {
        switch ($InternalCheck) {
            'parser'       { $result = Invoke-ParserCheck }
            'interop'      { $result = Invoke-InteropCheck }
            'capabilities' { $result = Invoke-CapabilitiesCheck }
            'artifact'     { $result = Invoke-ArtifactCheck }
            default        { throw "未知的內部檢查：$InternalCheck" }
        }
        $status = [string]$result.status
        Write-Host ('[{0}] {1}：{2}' -f $result.status, $result.check, $result.summary)
    } catch {
        $result = [ordered]@{
            schema  = 1
            check   = $InternalCheck
            status  = 'fail'
            when    = (Get-Date).ToString('o')
            summary = '內部檢查失敗：' + $_.Exception.Message
            details = [ordered]@{ exception = $_.Exception.ToString() }
        }
        $status = 'fail'
        Write-Host ('[fail] {0}：{1}' -f $InternalCheck, $result.summary)
        [Console]::Error.WriteLine($_.Exception.ToString())
    }
    try { Write-Utf8File (Join-Path $CheckDir 'result.json') ($result | ConvertTo-Json -Depth 12) }
    catch {
        [Console]::Error.WriteLine('無法寫入 result.json：' + $_.Exception.Message)
        exit 1
    }
    if ($status -eq 'fail') { exit 1 }
    exit 0
}

# ---- 一般模式：建立結果目錄、複製腳本、逐項執行 ----

function New-CheckResult {
    param(
        [string]$Name,
        [string]$Description,
        [string]$Status,
        [string]$Summary,
        $Capture,
        [string]$Directory,
        $Details
    )
    $exitCode = $null
    $duration = $null
    if ($Capture) {
        $duration = $Capture.DurationMs
        if ($Capture.TimedOut) { $exitCode = 'timeout' }
        elseif ($null -eq $Capture.ExitCode) { $exitCode = 'unknown' }
        else { $exitCode = [string]$Capture.ExitCode }
    }
    $entry = [ordered]@{
        name        = $Name
        description = $Description
        status      = $Status
        summary     = $Summary
        exit_code   = $exitCode
        duration_ms = $duration
        directory   = $Directory
        details     = $Details
    }
    Add-RunLog ('{0}: {1} (exit {2}, {3} ms) {4}' -f $Status, $Name, $exitCode, $duration, $Summary)
    Write-Host ('[{0}] {1}：{2}' -f $Status, $Name, $Summary)
    return $entry
}

function Invoke-InternalCheckFromParent {
    param(
        [string]$Name,
        [string]$Description,
        [string[]]$ExtraArguments,
        [int]$CheckTimeoutSeconds = 300
    )
    $dir = Join-Path $script:ChecksDir $Name
    if (-not (Test-Path -LiteralPath $dir)) { New-Item -ItemType Directory -Force -Path $dir | Out-Null }
    $arguments = @(
        '-NoProfile', '-NonInteractive', '-ExecutionPolicy', 'Bypass',
        '-File', $SelfPath,
        '-InternalCheck', $Name,
        '-RepoRoot', $script:RepoRoot,
        '-ScriptDir', $script:ScriptDir,
        '-CheckDir', $dir
    )
    if ($ExtraArguments -and $ExtraArguments.Count -gt 0) { $arguments += $ExtraArguments }
    $capture = Invoke-CapturedProcess -FilePath $PsExe -Arguments $arguments -WorkingDirectory $dir -TimeoutSeconds $CheckTimeoutSeconds
    Save-Capture -Dir $dir -Capture $capture
    if ($capture.TimedOut) {
        return (New-CheckResult -Name $Name -Description $Description -Status 'fail' -Summary ('檢查逾時（{0} 秒）' -f $CheckTimeoutSeconds) -Capture $capture -Directory $dir -Details ([ordered]@{ timeout_seconds = $CheckTimeoutSeconds }))
    }
    if ($capture.StartError) {
        return (New-CheckResult -Name $Name -Description $Description -Status 'fail' -Summary ('無法啟動 Windows PowerShell：' + $capture.StartError) -Capture $capture -Directory $dir -Details $null)
    }
    $meta = Read-JsonFile (Join-Path $dir 'result.json')
    if (-not $meta) {
        $tail = ''
        if ($capture.Stderr) { $tail = (($capture.Stderr -split "`r?`n") | Select-Object -Last 8) -join "`n" }
        return (New-CheckResult -Name $Name -Description $Description -Status 'fail' -Summary ('子行程沒有產生 result.json（exit code {0}）' -f $capture.ExitCode) -Capture $capture -Directory $dir -Details ([ordered]@{ stderr_tail = $tail }))
    }
    $status = [string]$meta.status
    $summary = [string]$meta.summary
    if ($capture.ExitCode -ne 0 -and $status -ne 'fail') {
        $status = 'fail'
        $summary += ('；exit code {0}' -f $capture.ExitCode)
    }
    if ($capture.ExitCode -eq 0 -and $status -eq 'fail') { $summary += '；子行程回報失敗' }
    return (New-CheckResult -Name $Name -Description $Description -Status $status -Summary $summary -Capture $capture -Directory $dir -Details $meta.details)
}

function Invoke-ProfilesCheckFromParent {
    param([string]$Description)
    $dir = Join-Path $script:ChecksDir 'profiles'
    if (-not (Test-Path -LiteralPath $dir)) { New-Item -ItemType Directory -Force -Path $dir | Out-Null }
    $verify = Join-Path $script:ScriptDir 'verify-profiles.ps1'
    if (-not (Test-Path -LiteralPath $verify)) {
        return (New-CheckResult -Name 'profiles' -Description $Description -Status 'fail' -Summary "找不到 verify-profiles.ps1 複本：$verify" -Capture $null -Directory $dir -Details $null)
    }
    $jsonPath = Join-Path $dir 'profiles.json'
    $command = '[Console]::OutputEncoding=[Text.Encoding]::UTF8; & ''{0}'' -Out ''{1}''; exit $LASTEXITCODE' -f $verify.Replace("'", "''"), $jsonPath.Replace("'", "''")
    $capture = Invoke-CapturedProcess -FilePath $PsExe -Arguments @('-NoProfile', '-NonInteractive', '-ExecutionPolicy', 'Bypass', '-Command', $command) -WorkingDirectory $dir -TimeoutSeconds 180
    Save-Capture -Dir $dir -Capture $capture
    $profiles = Read-JsonFile $jsonPath
    $install = Get-InstallState
    $details = [ordered]@{
        installed         = $install.installed
        registered_dll    = $install.registered_dll
        profiles_json     = $jsonPath
        verify_exit_code  = $capture.ExitCode
        profiles          = @()
    }
    if (-not $profiles) {
        return (New-CheckResult -Name 'profiles' -Description $Description -Status 'fail' -Summary ('verify-profiles.ps1 沒有產生 profiles.json（exit code {0}）' -f $capture.ExitCode) -Capture $capture -Directory $dir -Details $details)
    }
    $rows = @($profiles.profiles)
    $details.profiles = $rows
    $zhRows = @($rows | Where-Object { $_.name -eq 'zh-TW' })
    $jaRows = @($rows | Where-Object { $_.name -eq 'ja-JP' })
    $zhRegistered = ($zhRows.Count -gt 0) -and [bool]$zhRows[0].registered
    $jaRegistered = ($jaRows.Count -gt 0) -and [bool]$jaRows[0].registered
    if ($capture.TimedOut) {
        return (New-CheckResult -Name 'profiles' -Description $Description -Status 'fail' -Summary 'verify-profiles.ps1 逾時' -Capture $capture -Directory $dir -Details $details)
    }
    if (@(0, 1) -notcontains $capture.ExitCode) {
        return (New-CheckResult -Name 'profiles' -Description $Description -Status 'fail' -Summary ('verify-profiles.ps1 結束碼 {0}' -f $capture.ExitCode) -Capture $capture -Directory $dir -Details $details)
    }
    if ($zhRegistered -and $jaRegistered) {
        return (New-CheckResult -Name 'profiles' -Description $Description -Status 'pass' -Summary 'zh-TW 與 ja-JP profile 都已註冊' -Capture $capture -Directory $dir -Details $details)
    }
    if (-not $zhRegistered -and -not $jaRegistered -and -not $install.installed) {
        return (New-CheckResult -Name 'profiles' -Description $Description -Status 'info' -Summary '0C 未安裝；zh-TW 與 ja-JP 都未註冊（不算 runner 失敗）' -Capture $capture -Directory $dir -Details $details)
    }
    return (New-CheckResult -Name 'profiles' -Description $Description -Status 'fail' -Summary ('0C 已安裝但 profile 未完整註冊：zh-TW={0}、ja-JP={1}' -f $zhRegistered, $jaRegistered) -Capture $capture -Directory $dir -Details $details)
}

function Copy-RunnerScripts {
    param([string]$Repo)
    $testSource = Join-Path $Repo 'platforms\windows\test'
    $installerSource = Join-Path $Repo 'platforms\windows\installer'
    if (-not (Test-Path -LiteralPath $testSource)) { Stop-WithUsageError "找不到 platforms\windows\test：$testSource" }
    if (-not (Test-Path -LiteralPath (Join-Path $testSource 'ja-common.ps1'))) { Stop-WithUsageError 'repo 裡沒有 ja-common.ps1' }

    if (Test-Path -LiteralPath $script:ScriptDir) {
        Get-ChildItem -LiteralPath $script:ScriptDir -File -ErrorAction SilentlyContinue | Remove-Item -Force -ErrorAction SilentlyContinue
    } else {
        New-Item -ItemType Directory -Force -Path $script:ScriptDir | Out-Null
    }

    $sources = New-Object System.Collections.ArrayList
    foreach ($file in (Get-ChildItem -LiteralPath $testSource -Filter 'ja-*.ps1' -File | Sort-Object Name)) {
        [void]$sources.Add($file)
    }
    foreach ($name in @('acceptance-test.ps1')) {
        $path = Join-Path $testSource $name
        if (-not (Test-Path -LiteralPath $path)) { Stop-WithUsageError "缺少 T29 腳本：$path" }
        [void]$sources.Add((Get-Item -LiteralPath $path))
    }

    $files = @()
    foreach ($source in $sources) {
        $destination = Join-Path $script:ScriptDir $source.Name
        Copy-Item -LiteralPath $source.FullName -Destination $destination -Force
        $files += [ordered]@{
            name            = $source.Name
            source          = $source.FullName
            destination     = $destination
            source_sha256   = (Get-FileHash -LiteralPath $source.FullName -Algorithm SHA256).Hash
            sha256          = (Get-FileHash -LiteralPath $destination -Algorithm SHA256).Hash
            bytes           = (Get-Item -LiteralPath $destination).Length
            encoding        = 'copied-as-is'
        }
    }

    # T28 的 verify-profiles.ps1 是 UTF-8 無 BOM；5.1 在 Big5 等非 UTF-8 ACP 的機器上
    # 會把中文當 ANSI 而剖析失敗。複本轉成 UTF-8 BOM，來源不動。
    $verifySource = Join-Path $installerSource 'verify-profiles.ps1'
    if (-not (Test-Path -LiteralPath $verifySource)) { Stop-WithUsageError "缺少 T28 verify-profiles.ps1：$verifySource" }
    $reader = New-Object System.IO.StreamReader($verifySource, (New-Object System.Text.UTF8Encoding($false)), $true)
    try { $verifyText = $reader.ReadToEnd() } finally { $reader.Dispose() }
    $verifyDestination = Join-Path $script:ScriptDir 'verify-profiles.ps1'
    [IO.File]::WriteAllText($verifyDestination, $verifyText, (New-Object System.Text.UTF8Encoding($true)))
    $files += [ordered]@{
        name            = 'verify-profiles.ps1'
        source          = $verifySource
        destination     = $verifyDestination
        source_sha256   = (Get-FileHash -LiteralPath $verifySource -Algorithm SHA256).Hash
        sha256          = (Get-FileHash -LiteralPath $verifyDestination -Algorithm SHA256).Hash
        bytes           = (Get-Item -LiteralPath $verifyDestination).Length
        encoding        = 'utf-8-bom (normalized copy; Windows PowerShell 5.1 cannot parse the BOM-less source under a non-UTF-8 ACP)'
    }

    $manifest = [ordered]@{
        schema      = 1
        when        = (Get-Date).ToString('o')
        source_root = $Repo
        files       = $files
    }
    Write-Utf8File (Join-Path $script:ScriptDir 'manifest.json') ($manifest | ConvertTo-Json -Depth 8)
    return $files
}

function Invoke-Runner {
    $version = $PSVersionTable.PSVersion
    $edition = [string]$PSVersionTable.PSEdition
    if ($edition -eq 'Core' -or $version -lt [version]'5.1') {
        Stop-WithUsageError ('此 runner 需要 Windows PowerShell 5.1；目前是 {0}（{1}）' -f $version, $edition)
    }

    $repo = ConvertTo-WindowsPath $RepoRoot
    if ([string]::IsNullOrWhiteSpace($repo)) { $repo = Join-Path $PSScriptRoot '..\..\..' }
    if (-not (Test-Path -LiteralPath $repo)) { Stop-WithUsageError "找不到 repo：$repo" }
    $script:RepoRoot = (Get-Item -LiteralPath $repo).FullName

    $resultRoot = ConvertTo-WindowsPath $ResultRoot
    try {
        if (-not (Test-Path -LiteralPath $resultRoot)) { New-Item -ItemType Directory -Force -Path $resultRoot | Out-Null }
    } catch {
        Stop-WithUsageError ('無法建立結果目錄 {0}：{1}' -f $resultRoot, $_.Exception.Message)
    }
    $resultRoot = (Get-Item -LiteralPath $resultRoot).FullName
    $script:ScriptDir = Join-Path $resultRoot 'scripts'
    $runId = (Get-Date -Format 'yyyyMMdd-HHmmss') + '-' + [guid]::NewGuid().ToString('N').Substring(0, 8)
    $runDir = Join-Path $resultRoot ('results\' + $runId)
    $script:ChecksDir = Join-Path $runDir 'checks'
    New-Item -ItemType Directory -Force -Path $script:ChecksDir | Out-Null
    $script:LogPath = Join-Path $runDir 'run.log'
    Write-Utf8File $script:LogPath ''

    Write-Host ('T29b Windows 非互動檢查：{0}' -f $runDir)
    Write-Host ('Repo：{0}' -f $script:RepoRoot)
    Add-RunLog ('runner={0} repo={1}' -f $SelfPath, $script:RepoRoot)

    Write-Host '複製 T29／T28 腳本…'
    $copied = Copy-RunnerScripts -Repo $script:RepoRoot
    foreach ($file in $copied) {
        Write-Host ('  {0}  {1}' -f $file.sha256.Substring(0, 12), $file.name)
        Add-RunLog ('script {0} {1}' -f $file.sha256, $file.destination)
    }

    $artifact = $null
    if ($ArtifactDir) {
        $artifact = ConvertTo-WindowsPath $ArtifactDir
    } else {
        $defaultArtifact = Join-Path $resultRoot 'artifact'
        if (Test-Path -LiteralPath $defaultArtifact) { $artifact = $defaultArtifact }
    }

    $results = @()
    $results += Invoke-InternalCheckFromParent -Name 'parser' -Description 'T29 .ps1 剖析' -CheckTimeoutSeconds 180
    $results += Invoke-InternalCheckFromParent -Name 'interop' -Description 'ja-common Add-Type interop 編譯' -CheckTimeoutSeconds 180
    $results += Invoke-ProfilesCheckFromParent -Description 'T28 profile 註冊查詢'
    $results += Invoke-InternalCheckFromParent -Name 'capabilities' -Description '主機能力盤點' -CheckTimeoutSeconds 120

    $artifactArguments = @()
    if ($artifact) { $artifactArguments += @('-ArtifactDir', $artifact) }
    if ($DataDir) { $artifactArguments += @('-DataDir', (ConvertTo-WindowsPath $DataDir)) }
    $results += Invoke-InternalCheckFromParent -Name 'artifact' -Description 'CI artifact 非互動 smoke' -ExtraArguments $artifactArguments -CheckTimeoutSeconds 300

    $failed = @($results | Where-Object { $_.status -eq 'fail' })
    $passed = @($results | Where-Object { $_.status -eq 'pass' })
    $info = @($results | Where-Object { $_.status -eq 'info' })
    $skipped = @($results | Where-Object { $_.status -eq 'skip' })
    $exitCode = 0
    if ($failed.Count -gt 0) { $exitCode = 1 }

    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = [Security.Principal.WindowsPrincipal]::new($identity)
    $admin = $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
    $hostInfo = [ordered]@{
        computer      = $env:COMPUTERNAME
        user          = $env:USERNAME
        identity      = $identity.Name
        admin         = $admin
        powershell    = $version.ToString()
        edition       = $edition
        os            = [Environment]::OSVersion.VersionString
        repo_root     = $script:RepoRoot
        repo_revision = $RepoRevision
        result_root   = $resultRoot
        artifact_dir  = $artifact
    }
    $selfHash = (Get-FileHash -LiteralPath $SelfPath -Algorithm SHA256).Hash
    $summary = [ordered]@{
        schema      = 1
        test        = 'T29b Windows 非互動檢查'
        when        = (Get-Date).ToString('o')
        run_id      = $runId
        passed      = ($failed.Count -eq 0)
        exit_code   = $exitCode
        host        = $hostInfo
        runner      = [ordered]@{ path = $SelfPath; sha256 = $selfHash }
        scripts     = $copied
        checks      = $results
    }
    Write-Utf8File (Join-Path $runDir 'summary.json') ($summary | ConvertTo-Json -Depth 14)

    $markdown = New-Object System.Collections.ArrayList
    [void]$markdown.Add('# T29b Windows 非互動檢查')
    [void]$markdown.Add('')
    [void]$markdown.Add(('- 時間：{0}' -f (Get-Date).ToString('o')))
    [void]$markdown.Add(('- 主機：{0}（{1}）' -f $env:COMPUTERNAME, $identity.Name))
    [void]$markdown.Add(('- Windows PowerShell：{0}' -f $version.ToString()))
    [void]$markdown.Add(('- 管理員：{0}' -f $admin))
    [void]$markdown.Add(('- Repo：{0}' -f $script:RepoRoot))
    if ($RepoRevision) { [void]$markdown.Add(('- Repo revision：{0}' -f $RepoRevision)) }
    [void]$markdown.Add(('- 結果：{0}' -f $runDir))
    [void]$markdown.Add(('- Artifact：{0}' -f $(if ($artifact) { $artifact } else { '（未指定）' })))
    [void]$markdown.Add('')
    [void]$markdown.Add('| 檢查 | 狀態 | exit | 秒 | 摘要 |')
    [void]$markdown.Add('|---|---|---|---|---|')
    foreach ($entry in $results) {
        $seconds = ''
        if ($null -ne $entry.duration_ms) { $seconds = [math]::Round($entry.duration_ms / 1000.0, 1) }
        [void]$markdown.Add(('| {0} | {1} | {2} | {3} | {4} |' -f $entry.name, $entry.status, $entry.exit_code, $seconds, ($entry.summary -replace '\|', '\|')))
    }
    [void]$markdown.Add('')
    [void]$markdown.Add('互動桌面項目（WPF／記事本／Terminal／Edge／WebView2／SearchHost、Win+Space、TSF 註冊）不在這支 runner 的範圍；請照 README 的 T29 驗收流程手動執行。')
    [void]$markdown.Add('')
    Write-Utf8File (Join-Path $runDir 'summary.md') (($markdown -join "`r`n") + "`r`n")

    Add-RunLog ('done pass={0} fail={1} info={2} skip={3} exit={4}' -f $passed.Count, $failed.Count, $info.Count, $skipped.Count, $exitCode)

    Write-Host ''
    Write-Host ('完成：{0} 通過、{1} 失敗、{2} 資訊、{3} 略過' -f $passed.Count, $failed.Count, $info.Count, $skipped.Count)
    Write-Host ('結果目錄：{0}' -f $runDir)
    if ($failed.Count -gt 0) {
        Write-Host ('失敗項目：{0}' -f (($failed | ForEach-Object { $_.name }) -join '、'))
    }
    return $exitCode
}

try {
    $code = Invoke-Runner
    exit $code
} catch {
    $exitCode = 3
    if ($script:UsageError) { $exitCode = 2 }
    [Console]::Error.WriteLine('T29b runner 失敗：' + $_.Exception.Message)
    [Console]::Error.WriteLine($_.Exception.ToString())
    exit $exitCode
}
