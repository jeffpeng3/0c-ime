# T29 32 位元日文驗收：必須由 SysWOW64 的 Windows PowerShell 啟動（acceptance-test.ps1 -Suite ja-32bit）。
# 確認 x86 行程載入 x86\0c_tsf.dll，而且日文組字、変換、送出都能用。
param([Parameter(Mandatory=$true)][string]$ResultDir,[Parameter(Mandatory=$true)][string]$ExpectedDLL)
$ErrorActionPreference='Stop'
. (Join-Path $PSScriptRoot 'ja-common.ps1')
$report=[ordered]@{passed=$false;checks=@();error='';dll='';machine='';pshome=$PSHOME;is64=$null}
$form=$null
$savedLocalAppData=$env:LOCALAPPDATA
try {
    if([Environment]::Is64BitProcess){throw 'This suite must run in 32-bit Windows PowerShell (SysWOW64)'}
    if($PSHOME -notmatch 'SysWOW64'){throw "not running under SysWOW64: $PSHOME"}
    if($ExpectedDLL -notmatch '\\x86\\0c_tsf\.dll$'){throw "expected the x86 DLL under x86\\, got [$ExpectedDLL]"}
    if(-not(Test-Path -LiteralPath $ExpectedDLL)){throw "x86 DLL missing: $ExpectedDLL"}
    $report.is64=[Environment]::Is64BitProcess
    $bytes=[IO.File]::ReadAllBytes($ExpectedDLL)
    $peOffset=[BitConverter]::ToInt32($bytes,0x3C)
    $report.machine=('0x{0:X4}' -f ([BitConverter]::ToUInt16($bytes,$peOffset+4)))
    if($report.machine -ne '0x014C'){throw "DLL is not i386 (machine $($report.machine))"}
    $report.checks+='ja_32bit_i386_dll'
    Add-Type -AssemblyName System.Windows.Forms,System.Drawing
    Initialize-JaProbe
    $env:LOCALAPPDATA=$ResultDir
    New-Item -ItemType Directory -Force (Join-Path $ResultDir 'Zuin')|Out-Null
    New-Item -ItemType File -Force (Join-Path $ResultDir 'Zuin\tsf-debug')|Out-Null
    $form=New-Object Windows.Forms.Form
    $form.Text='0c Japanese 32-bit acceptance';$form.Width=640;$form.Height=220;$form.TopMost=$true
    $box=New-Object Windows.Forms.TextBox
    $box.Multiline=$true;$box.Dock='Fill';$box.Font=New-Object Drawing.Font('Yu Gothic UI',24)
    $form.Controls.Add($box)
    $form.Show();$form.Activate();$box.Focus()|Out-Null
    function PumpForms([int]$Milliseconds){
        $watch=[Diagnostics.Stopwatch]::StartNew()
        while($watch.ElapsedMilliseconds -lt $Milliseconds){[Windows.Forms.Application]::DoEvents();Start-Sleep -Milliseconds 10}
    }
    PumpForms 600
    Invoke-JaProfile|Out-Null
    PumpForms 1200
    function FocusGuard {if([JaAcceptanceProbe]::GetForegroundWindow() -ne $form.Handle){throw '32-bit test window lost focus; refusing to type'}}
    FocusGuard
    [JaAcceptanceProbe]::TypeText('nihongo');PumpForms 1000
    if($box.Text -cne 'にほんご'){throw "32-bit host did not compose kana: [$($box.Text)]"}
    $report.checks+='ja_32bit_romaji'
    [JaAcceptanceProbe]::Tap(0x20);PumpForms 1000
    $candidate=Wait-JaCandidate
    if($candidate -eq [IntPtr]::Zero){throw '32-bit host did not show the candidate window'}
    $report.checks+='ja_32bit_candidate'
    [JaAcceptanceProbe]::Tap(0x31);PumpForms 900
    [JaAcceptanceProbe]::Tap(0x0D);PumpForms 900
    $commit=$box.Text
    if(-not$commit -or $commit -ceq 'にほんご' -or $commit -notmatch '[\u3040-\u30ff\u4e00-\u9fff]'){throw "32-bit host did not commit Japanese: [$commit]"}
    $report.checks+='ja_32bit_commit'
    $report.commit=$commit
    $module=(Get-Process -Id $PID).Modules|Where-Object ModuleName -eq '0c_tsf.dll'|Select-Object -First 1
    $report.dll=$module.FileName
    if($report.dll -ne $ExpectedDLL){throw "32-bit process loaded unexpected DLL: $($report.dll)"}
    $report.checks+='ja_32bit_x86_dll_loaded'
    $report.passed=$true
} catch {$report.error=$_.Exception.ToString()} finally {
    if($form){$form.Close();$form.Dispose()}
    $env:LOCALAPPDATA=$savedLocalAppData
    if(Test-Path -LiteralPath $ResultDir){$report|ConvertTo-Json -Depth 6|Set-Content (Join-Path $ResultDir 'ja-32bit-result.json') -Encoding UTF8}
}
if(-not$report.passed){exit 1}
