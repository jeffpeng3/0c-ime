# T29 日文隱私驗收：密碼、PIN 不送鍵也不學習；IS_PRIVATE 欄位照常組字但不讀前文、不學習。
# 使用暫存引擎與測試目錄的 user.json；不碰日常詞庫。
param([Parameter(Mandatory=$true)][string]$ResultDir,[Parameter(Mandatory=$true)][string]$ExpectedDLL)
$ErrorActionPreference='Stop'
. (Join-Path $PSScriptRoot 'ja-common.ps1')
$report=[ordered]@{passed=$false;checks=@();error='';values=[ordered]@{};learning=[ordered]@{};dll=''}
$win=$null;$form=$null
$savedLocalAppData=$env:LOCALAPPDATA
try {
    Add-Type -AssemblyName PresentationFramework,PresentationCore,WindowsBase,System.Windows.Forms,System.Drawing
    Add-Type @'
using System;
using System.Runtime.InteropServices;
public static class JaPrivacyProbe {
    [DllImport("msctf.dll")] public static extern int SetInputScope(IntPtr hwnd, int scope);
    [DllImport("user32.dll")] public static extern IntPtr GetForegroundWindow();
}
'@
    Initialize-JaProbe
    $env:LOCALAPPDATA=$ResultDir
    New-Item -ItemType Directory -Force (Join-Path $ResultDir 'Zuin')|Out-Null
    New-Item -ItemType File -Force (Join-Path $ResultDir 'Zuin\tsf-debug')|Out-Null
    $logPath=Join-Path $ResultDir 'Zuin\tsf.log'
    $userPath=Join-Path $ResultDir 'test-user.json'
    function Pump([int]$Milliseconds){
        $frame=New-Object Windows.Threading.DispatcherFrame
        $timer=New-Object Windows.Threading.DispatcherTimer
        $timer.Interval=[TimeSpan]::FromMilliseconds($Milliseconds)
        $timer.add_Tick({$frame.Continue=$false}.GetNewClosure())
        $timer.Start()
        try{[Windows.Threading.Dispatcher]::PushFrame($frame)}finally{$timer.Stop()}
    }
    function LogText {if(Test-Path -LiteralPath $logPath){return (Get-Content -LiteralPath $logPath -Raw -Encoding UTF8)};return ''}
    function UserText {if(Test-Path -LiteralPath $userPath){return [IO.File]::ReadAllText($userPath)};return ''}
    function CountOf([string]$Text,[string]$Word){if(-not$Word){return 0};return ([regex]::Matches($Text,[regex]::Escape($Word))).Count}
    function PumpForms([int]$Milliseconds){
        $watch=[Diagnostics.Stopwatch]::StartNew()
        while($watch.ElapsedMilliseconds -lt $Milliseconds){[Windows.Forms.Application]::DoEvents();Start-Sleep -Milliseconds 10}
    }
    function Send([string]$Text){[JaAcceptanceProbe]::TypeText($Text);Pump 700}
    function Key([int]$VirtualKey){[JaAcceptanceProbe]::Tap([uint16]$VirtualKey);Pump 600}
    function SendNative([string]$Text){[JaAcceptanceProbe]::TypeText($Text);PumpForms 800}
    function KeyNative([int]$VirtualKey){[JaAcceptanceProbe]::Tap([uint16]$VirtualKey);PumpForms 700}
    $win=New-Object Windows.Window
    $win.Title='0c Japanese privacy acceptance';$win.Width=700;$win.Height=280;$win.Topmost=$true
    $panel=New-Object Windows.Controls.StackPanel
    $box=New-Object Windows.Controls.TextBox;$box.FontSize=28
    $password=New-Object Windows.Controls.PasswordBox;$password.FontSize=28
    $panel.Children.Add($box)|Out-Null;$panel.Children.Add($password)|Out-Null
    $win.Content=$panel
    $win.Show();$win.Activate()|Out-Null;$box.Focus()|Out-Null
    $handle=(New-Object Windows.Interop.WindowInteropHelper($win)).Handle
    Pump 500
    Invoke-JaProfile|Out-Null
    Pump 1200
    function FocusGuard {if([JaAcceptanceProbe]::GetForegroundWindow() -ne $handle){throw 'WPF test window lost focus; refusing to type'}}
    # 1. 一般欄位先送出一個字，建立要學習的對照；等引擎寫檔後再測私人欄位。
    FocusGuard
    Send 'nihongo';Key 0x20;Key 0x0D
    $normalCommit=$box.Text
    if(-not$normalCommit -or $normalCommit -ceq 'にほんご'){throw "normal field did not commit a conversion: [$normalCommit]"}
    $report.values.normal=$normalCommit
    $report.checks+='ja_private_normal_commit'
    Start-Sleep -Seconds 12
    $baseline=UserText
    $report.learning.normal_observed=(CountOf $baseline $normalCommit) -gt 0
    if($report.learning.normal_observed){$report.checks+='ja_private_normal_learned'}
    # 2. 密碼欄位：TSF 停用，按日文字母與変換鍵都原樣進入欄位，不開候選、不送引擎。
    $password.Focus()|Out-Null;Pump 400
    $logBefore=LogText
    Send 'nihongo';Key 0x1C
    if($password.Password -cne 'nihongo'){throw "password field did not receive raw keys: [$($password.Password)]"}
    $candidate=[JaAcceptanceProbe]::Candidate()
    if($candidate -ne [IntPtr]::Zero -and [JaAcceptanceProbe]::IsWindowVisible($candidate)){throw 'candidate window appeared in the password field'}
    $newLog=LogText
    if($newLog.Length -gt $logBefore.Length){$newLog=$newLog.Substring([Math]::Min($logBefore.Length,$newLog.Length))}
    else {$newLog=''}
    if($newLog -match 'vk=|snapshot|open: session='){throw 'password keystrokes reached the engine or the keyboard log'}
    $report.checks+='ja_private_password_passthrough'
    $password.Clear()
    # 3. 原生 EDIT＋IS_PRIVATE：照常組字、変換與選字，但不提供前文，也不學習。
    $form=New-Object Windows.Forms.Form
    $form.Text='0c Japanese private field';$form.Width=620;$form.Height=180;$form.TopMost=$true
    $native=New-Object Windows.Forms.TextBox
    $native.Multiline=$true;$native.Dock='Fill';$native.Font=New-Object Drawing.Font('Yu Gothic UI',24)
    $form.Controls.Add($native)
    $form.Show();$form.Activate()|Out-Null;$native.Focus()|Out-Null
    PumpForms 600
    [JaPrivacyProbe]::SetInputScope($native.Handle,61)|Out-Null # IS_PRIVATE
    PumpForms 300
    [JaAcceptanceProbe]::Focus($form.Handle)|Out-Null
    if([JaPrivacyProbe]::GetForegroundWindow() -ne $form.Handle){throw 'native private test form lost focus'}
    SendNative 'nihongo'
    if($native.Text -cne 'にほんご'){throw "private field did not compose Japanese: [$($native.Text)]"}
    $report.checks+='ja_private_scope_61_composes'
    KeyNative 0x20
    $privateCandidate=Wait-JaCandidate
    if($privateCandidate -eq [IntPtr]::Zero){throw 'private field did not show conversion candidates'}
    KeyNative 0x0D
    $privateCommit=$native.Text
    if(-not$privateCommit -or $privateCommit -ceq 'にほんご'){throw "private field did not commit a conversion: [$privateCommit]"}
    $report.values.private=$privateCommit
    $report.checks+='ja_private_scope_61_commit'
    # 送出後立刻再打一段：私人欄位不應讀前文，tsf.log 不能出現 document context confirmed。
    SendNative 'nihongo'
    KeyNative 0x20
    KeyNative 0x0D
    $privateLog=LogText
    $privateNew=if($privateLog.Length -gt $logBefore.Length){$privateLog.Substring([Math]::Min($logBefore.Length,$privateLog.Length))}else{''}
    if($privateNew -match 'document context confirmed'){throw 'private field read the preceding document text'}
    $report.checks+='ja_private_scope_61_no_document_context'
    Start-Sleep -Seconds 12
    $afterPrivate=UserText
    $beforeCount=CountOf $baseline $privateCommit
    $afterCount=CountOf $afterPrivate $privateCommit
    if($afterCount -gt $beforeCount){throw "private field taught [$privateCommit] to the personal dictionary"}
    $report.learning.private_word=$privateCommit
    $report.learning.private_before=$beforeCount
    $report.learning.private_after=$afterCount
    $report.checks+='ja_private_not_learned'
    # 4. 密碼（IS_PASSWORD=31）與數值 PIN（IS_NUMERIC_PIN=64）：按鍵直接進 EDIT，沒有候選、沒有引擎 session。
    [JaPrivacyProbe]::SetInputScope($native.Handle,0)|Out-Null
    $native.Clear();PumpForms 300
    foreach($scope in @(31,64)){
        [JaPrivacyProbe]::SetInputScope($native.Handle,$scope)|Out-Null
        PumpForms 300
        $logBefore=LogText
        SendNative 'nihongo'
        KeyNative 0x1C
        if($native.Text -cne 'nihongo'){throw "scope $scope did not receive raw keys: [$($native.Text)]"}
        $candidate=[JaAcceptanceProbe]::Candidate()
        if($candidate -ne [IntPtr]::Zero -and [JaAcceptanceProbe]::IsWindowVisible($candidate)){throw "scope $scope opened a candidate window"}
        $scopeLog=LogText
        $scopeNew=if($scopeLog.Length -gt $logBefore.Length){$scopeLog.Substring([Math]::Min($logBefore.Length,$scopeLog.Length))}else{''}
        if($scopeNew -match 'vk=|snapshot|open: session='){throw "scope $scope reached the engine or the keyboard log"}
        $report.checks+="ja_private_scope_${scope}_passthrough"
        $native.Clear();PumpForms 300
    }
    [JaPrivacyProbe]::SetInputScope($native.Handle,0)|Out-Null
    $afterSensitive=UserText
    if((CountOf $afterSensitive 'nihongo') -gt (CountOf $baseline 'nihongo')){throw 'password or PIN text reached the personal dictionary'}
    $report.checks+='ja_private_password_pin_not_learned'
    $module=(Get-Process -Id $PID).Modules|Where-Object ModuleName -eq '0c_tsf.dll'|Select-Object -First 1
    $report.dll=$module.FileName
    if($report.dll -ne $ExpectedDLL){throw "unexpected DLL: $($report.dll)"}
    $report.checks+='ja_private_dll_loaded'
    $report.passed=$true
} catch {$report.error=$_.Exception.ToString()} finally {
    if($form){$form.Close();$form.Dispose()}
    if($win){$win.Close()}
    $env:LOCALAPPDATA=$savedLocalAppData
    if(Test-Path -LiteralPath $ResultDir){$report|ConvertTo-Json -Depth 6|Set-Content (Join-Path $ResultDir 'ja-private-result.json') -Encoding UTF8}
}
if(-not$report.passed){exit 1}
