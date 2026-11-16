# T29 日文 smoke 驗收：WPF、記事本、Windows Terminal。
# 羅馬字→かな、Space／変換、Enter、Escape 回かな、Tab／Shift+Tab、変換／無変換、F6–F10。
# 由 acceptance-test.ps1 -Suite ja-smoke 啟動；引擎已使用暫存 -user 詞庫，這裡不碰日常資料。
param([Parameter(Mandatory=$true)][string]$ResultDir,[Parameter(Mandatory=$true)][string]$ExpectedDLL)
$ErrorActionPreference='Stop'
. (Join-Path $PSScriptRoot 'ja-common.ps1')
$report=[ordered]@{passed=$false;session=[Diagnostics.Process]::GetCurrentProcess().SessionId;checks=@();hosts=[ordered]@{};dll='';error=''}
$win=$null;$notepad=$null;$terminal=$null
$savedLocalAppData=$env:LOCALAPPDATA
try {
    Add-Type -AssemblyName PresentationFramework,PresentationCore,WindowsBase,System.Windows.Forms
    Initialize-JaProbe
    # 除錯日誌只寫本測試目錄；引擎仍是外層啟動、使用暫存 user.json 的那一個。
    $env:LOCALAPPDATA=$ResultDir
    New-Item -ItemType Directory -Force (Join-Path $ResultDir 'Zuin')|Out-Null
    New-Item -ItemType File -Force (Join-Path $ResultDir 'Zuin\tsf-debug')|Out-Null
    $win=New-Object Windows.Window
    $win.Title='0c Japanese acceptance';$win.Width=760;$win.Height=260;$win.Topmost=$true
    $box=New-Object Windows.Controls.TextBox
    $box.FontSize=32;$box.AcceptsReturn=$false;$box.Padding=[Windows.Thickness]::new(8)
    $win.Content=$box
    $win.Show();$win.Activate()|Out-Null;$box.Focus()|Out-Null
    $handle=(New-Object Windows.Interop.WindowInteropHelper($win)).Handle
    function Pump([int]$Milliseconds){
        $frame=New-Object Windows.Threading.DispatcherFrame
        $timer=New-Object Windows.Threading.DispatcherTimer
        $timer.Interval=[TimeSpan]::FromMilliseconds($Milliseconds)
        $timer.add_Tick({$frame.Continue=$false}.GetNewClosure())
        $timer.Start()
        try{[Windows.Threading.Dispatcher]::PushFrame($frame)}finally{$timer.Stop()}
    }
    Pump 500
    $jaProfile=Invoke-JaProfile
    Pump 1200
    $report.hosts.wpf=[ordered]@{profile=$jaProfile.ToString();pid=$PID}
    function FocusGuard {if([JaAcceptanceProbe]::GetForegroundWindow() -ne $handle){throw 'WPF test window lost focus; refusing to type'}}
    function Send([string]$Text){FocusGuard;[JaAcceptanceProbe]::TypeText($Text);Pump 650}
    function Key([int]$VirtualKey){FocusGuard;[JaAcceptanceProbe]::Tap([uint16]$VirtualKey);Pump 550}
    function Expect([string]$Expected,[string]$Name){
        if($box.Text -cne $Expected){throw "$Name expected [$Expected], got [$($box.Text)]"}
        $report.checks+=$Name
    }
    function BoxHash {
        $bitmap=New-Object Windows.Media.Imaging.RenderTargetBitmap([int]$box.ActualWidth,[int]$box.ActualHeight,96,96,[Windows.Media.PixelFormats]::Pbgra32)
        $bitmap.Render($box)
        $encoder=New-Object Windows.Media.Imaging.PngBitmapEncoder
        $encoder.Frames.Add([Windows.Media.Imaging.BitmapFrame]::Create($bitmap))|Out-Null
        $stream=New-Object IO.MemoryStream
        try {
            $encoder.Save($stream)
            $sha=[Security.Cryptography.SHA256]::Create()
            try{return [BitConverter]::ToString($sha.ComputeHash($stream.ToArray())).Replace('-','')}
            finally{$sha.Dispose()}
        } finally {$stream.Dispose()}
    }
    function SaveWindow([string]$Name) {
        $bitmap=New-Object Windows.Media.Imaging.RenderTargetBitmap([int]$win.ActualWidth,[int]$win.ActualHeight,96,96,[Windows.Media.PixelFormats]::Pbgra32)
        $bitmap.Render($win)
        $encoder=New-Object Windows.Media.Imaging.PngBitmapEncoder
        $encoder.Frames.Add([Windows.Media.Imaging.BitmapFrame]::Create($bitmap))|Out-Null
        $stream=[IO.File]::Create((Join-Path $ResultDir $Name))
        try{$encoder.Save($stream)}finally{$stream.Dispose()}
    }
    # 先確認這是日文模式：一個 a 應該是「あ」，不是注音或英文字母。
    Send 'a'
    Expect 'あ' 'ja_wpf_mode'
    Key 0x1B
    Expect '' 'ja_wpf_initial_cancel'
    # 1. 羅馬字、Space 変換、Enter 確定。
    Send 'nihongo'
    Expect 'にほんご' 'ja_wpf_romaji'
    Key 0x20
    $candidate=Wait-JaCandidate
    if($candidate -eq [IntPtr]::Zero){throw 'conversion did not open the candidate window'}
    if($box.Text -ceq 'にほんご' -or $box.Text -notmatch '[\u4e00-\u9fff]'){throw "Space did not convert to kanji: [$($box.Text)]"}
    $report.checks+='ja_wpf_space_convert'
    Key 0x20
    if(-not[JaAcceptanceProbe]::IsWindowVisible($candidate)){throw 'second Space hid the candidate window'}
    $report.checks+='ja_wpf_space_next_candidate'
    Key 0x0D
    $wpfCommit=$box.Text
    if(-not$wpfCommit -or $wpfCommit -ceq 'にほんご'){throw "Enter did not commit a conversion: [$wpfCommit]"}
    $report.checks+='ja_wpf_enter_commit'
    $report.hosts.wpf.commit=$wpfCommit
    # 2. Escape 回かな，再一次 Escape 清掉整段。
    $box.Clear();Pump 200
    Send 'kitte';Key 0x20
    $candidate=Wait-JaCandidate
    if($candidate -eq [IntPtr]::Zero){throw 'Space did not start conversion for Escape test'}
    Key 0x1B
    Expect 'きって' 'ja_wpf_escape_back_to_kana'
    Key 0x1B
    Expect '' 'ja_wpf_escape_cancel'
    # 3. Tab／Shift+Tab 換文節：文字不變，但組字區的目前文節底線要移動。
    $box.Clear();Pump 200
    Send 'kuukigaii';Key 0x20
    $candidate=Wait-JaCandidate
    if($candidate -eq [IntPtr]::Zero){throw 'multi-clause conversion did not open the candidate window'}
    $before=$box.Text
    if($before -ceq 'くうきがいい'){throw 'multi-clause conversion stayed in kana'}
    $hash1=BoxHash
    Key 0x09
    if($box.Text -cne $before){throw 'Tab committed or changed the conversion text'}
    $hash2=BoxHash
    if($hash1 -eq $hash2){throw 'Tab did not move the active clause underline'}
    $report.checks+='ja_wpf_tab_next_clause'
    [JaAcceptanceProbe]::ShiftTab();Pump 550
    if($box.Text -cne $before){throw 'Shift+Tab committed or changed the conversion text'}
    $hash3=BoxHash
    if($hash3 -eq $hash2){throw 'Shift+Tab did not move the active clause underline back'}
    $report.checks+='ja_wpf_shift_tab_previous_clause'
    SaveWindow 'wpf-ja-clauses.png'
    Key 0x0D
    if(-not$box.Text){throw 'Enter after clause navigation committed nothing'}
    $report.checks+='ja_wpf_clause_commit'
    # 4. 實體変換／無変換鍵：開始変換、回到かな，再 Enter 送出かな。
    $box.Clear();Pump 200
    Send 'kyouha'
    Key 0x1C # VK_CONVERT
    $candidate=Wait-JaCandidate
    if($candidate -eq [IntPtr]::Zero){throw 'physical Convert key did not open the candidate window'}
    if($box.Text -ceq 'きょうは'){throw 'physical Convert key did not convert'}
    $report.checks+='ja_wpf_convert_key'
    Key 0x1D # VK_NONCONVERT
    Expect 'きょうは' 'ja_wpf_nonconvert_back_to_kana'
    Key 0x0D
    Expect 'きょうは' 'ja_wpf_nonconvert_commit'
    # 5. F6–F10：只轉換組字區，不先送出。
    $box.Clear();Pump 200
    Send 'kyouha'
    Key 0x76 # F7
    Expect 'キョウハ' 'ja_wpf_f7_katakana'
    Key 0x77 # F8
    Expect 'ｷｮｳﾊ' 'ja_wpf_f8_halfwidth_kana'
    Key 0x75 # F6
    Expect 'きょうは' 'ja_wpf_f6_hiragana'
    # F9／F10 由核心決定全角／半角英數的表記；T29 驗外殼有把鍵送進引擎、組字沒被送出。
    Key 0x78 # F9
    if(-not$box.Text){throw 'F9 committed or cleared the composition'}
    $report.hosts.wpf.f9=$box.Text
    $report.checks+='ja_wpf_f9_consumed'
    Key 0x79 # F10
    if(-not$box.Text){throw 'F10 committed or cleared the composition'}
    $report.hosts.wpf.f10=$box.Text
    $report.checks+='ja_wpf_f10_consumed'
    Key 0x1B
    Expect '' 'ja_wpf_f_keys_never_committed'
    SaveWindow 'wpf-ja.png'
    $module=(Get-Process -Id $PID).Modules|Where-Object ModuleName -eq '0c_tsf.dll'|Select-Object -First 1
    $report.dll=$module.FileName
    if($report.dll -ne $ExpectedDLL){throw "WPF loaded unexpected DLL: $($report.dll)"}
    $report.checks+='ja_wpf_dll_loaded'

    # 6. 記事本：同一個暫存引擎、新的宿主。用 Ctrl+S 讀回檔案，不靠 UI Automation 猜測。
    if(Get-Process notepad -ErrorAction SilentlyContinue){throw 'Existing Notepad is open; refusing to use user tabs'}
    $stem='0c-ja-notepad-'+[guid]::NewGuid().ToString('N')
    $file=Join-Path $ResultDir ($stem+'.txt')
    [IO.File]::WriteAllText($file,'',[Text.UTF8Encoding]::new($true))
    Start-Process notepad.exe -ArgumentList ('"'+$file+'"')|Out-Null
    $deadline=(Get-Date).AddSeconds(25)
    do {
        Start-Sleep -Milliseconds 200
        $notepad=Get-Process notepad -ErrorAction SilentlyContinue|Where-Object MainWindowTitle -like "*$stem*"|Select-Object -First 1
    } while(-not$notepad -and (Get-Date) -lt $deadline)
    if(-not$notepad){throw 'own Notepad window missing'}
    [JaAcceptanceProbe]::Focus($notepad.MainWindowHandle)|Out-Null
    if(-not(Wait-JaForeground $notepad.MainWindowHandle 5000)){throw 'Notepad did not become foreground'}
    Invoke-JaProfile -SessionWide $true|Out-Null
    Start-Sleep -Milliseconds 1200
    [JaAcceptanceProbe]::TypeText('nihongo');Start-Sleep -Milliseconds 900
    [JaAcceptanceProbe]::Tap(0x20);Start-Sleep -Milliseconds 900
    [JaAcceptanceProbe]::Tap(0x0D);Start-Sleep -Milliseconds 900
    [JaAcceptanceProbe]::CtrlS();Start-Sleep -Milliseconds 1200
    $deadline=(Get-Date).AddSeconds(4)
    while([IO.File]::ReadAllText($file) -ceq '' -and (Get-Date) -lt $deadline){Start-Sleep -Milliseconds 100}
    $notepadText=[IO.File]::ReadAllText($file)
    if(-not$notepadText -or $notepadText -notmatch '[\u3040-\u30ff\u4e00-\u9fff]'){throw "Notepad did not commit Japanese: [$notepadText]"}
    $report.hosts.notepad=[ordered]@{version=$notepad.MainModule.FileVersionInfo.ProductVersion;commit=$notepadText}
    $report.checks+='ja_notepad_commit'
    $notepad.Refresh()
    $module=$notepad.Modules|Where-Object ModuleName -eq '0c_tsf.dll'|Select-Object -First 1
    if($module.FileName -ne $ExpectedDLL){throw "Notepad loaded unexpected DLL: $($module.FileName)"}
    $report.checks+='ja_notepad_dll_loaded'
    $notepad.CloseMainWindow()|Out-Null
    $notepad=$null
    Start-Sleep -Milliseconds 800

    # 7. Windows Terminal：reader 只讀迴圈輸入，測試不執行任何輸入內容。
    $wt=Join-Path $savedLocalAppData 'Microsoft\WindowsApps\wt.exe'
    if(-not(Test-Path -LiteralPath $wt)){throw 'Windows Terminal is not installed'}
    @'
param([string]$ResultDir)
$ErrorActionPreference='Stop'
[IO.File]::WriteAllText("$ResultDir\ja-reader.pid",[string]$PID)
try {
    for($i=0;$i -lt 2;$i++) {
        [Console]::Write("0c ja test {0}> ",$i)
        [IO.File]::WriteAllText("$ResultDir\ja-ready-$i",'ready')
        $line=[Console]::ReadLine()
        [IO.File]::WriteAllText("$ResultDir\ja-line-$i.tmp",$line,[Text.UTF8Encoding]::new($true))
        [IO.File]::Move("$ResultDir\ja-line-$i.tmp","$ResultDir\ja-line-$i.txt")
    }
    while(-not(Test-Path "$ResultDir\ja-done")){Start-Sleep -Milliseconds 100}
} catch {
    [IO.File]::WriteAllText("$ResultDir\ja-reader-error.txt",$_.Exception.ToString())
    exit 1
}
'@|Set-Content (Join-Path $ResultDir 'ja-reader.ps1') -Encoding UTF8
    $title='0c-ja-terminal-'+[guid]::NewGuid().ToString('N')
    $arguments='-w new new-tab --title '+$title+' --suppressApplicationTitle -d "'+$ResultDir+'" powershell.exe -NoProfile -ExecutionPolicy Bypass -File "'+(Join-Path $ResultDir 'ja-reader.ps1')+'" -ResultDir "'+$ResultDir+'"'
    Start-Process $wt -ArgumentList $arguments|Out-Null
    $deadline=(Get-Date).AddSeconds(30)
    do {
        Start-Sleep -Milliseconds 150
        $terminal=Get-Process WindowsTerminal -ErrorAction SilentlyContinue|Where-Object MainWindowTitle -like "*$title*"|Select-Object -First 1
    } while(-not$terminal -and (Get-Date) -lt $deadline)
    if(-not$terminal){throw 'own Terminal window missing'}
    [JaAcceptanceProbe]::Focus($terminal.MainWindowHandle)|Out-Null
    if(-not(Wait-JaForeground $terminal.MainWindowHandle 5000)){throw 'Terminal did not become foreground'}
    Invoke-JaProfile -SessionWide $true|Out-Null
    Start-Sleep -Milliseconds 1200
    $deadline=(Get-Date).AddSeconds(15)
    while(-not(Test-Path (Join-Path $ResultDir 'ja-ready-0')) -and (Get-Date) -lt $deadline){Start-Sleep -Milliseconds 100}
    if(-not(Test-Path (Join-Path $ResultDir 'ja-ready-0'))){throw 'Terminal reader did not become ready'}
    [JaAcceptanceProbe]::TypeText('nihongo');Start-Sleep -Milliseconds 900
    [JaAcceptanceProbe]::Tap(0x20);Start-Sleep -Milliseconds 900
    [JaAcceptanceProbe]::Tap(0x0D);Start-Sleep -Milliseconds 800
    [JaAcceptanceProbe]::Tap(0x0D)
    $line=Join-Path $ResultDir 'ja-line-0.txt'
    $deadline=(Get-Date).AddSeconds(6)
    while(-not(Test-Path $line) -and (Get-Date) -lt $deadline){Start-Sleep -Milliseconds 100}
    if(-not(Test-Path $line)){throw 'Terminal reader did not receive the committed line'}
    $terminalText=[IO.File]::ReadAllText($line)
    if(-not$terminalText -or $terminalText -notmatch '[\u3040-\u30ff\u4e00-\u9fff]'){throw "Terminal did not commit Japanese: [$terminalText]"}
    $report.hosts.terminal=[ordered]@{version=$terminal.MainModule.FileVersionInfo.ProductVersion;commit=$terminalText}
    $report.checks+='ja_terminal_commit'
    $terminal.Refresh()
    $module=$terminal.Modules|Where-Object ModuleName -eq '0c_tsf.dll'|Select-Object -First 1
    if($module.FileName -ne $ExpectedDLL){throw "Terminal loaded unexpected DLL: $($module.FileName)"}
    $report.checks+='ja_terminal_dll_loaded'
    $report.passed=$true
} catch {$report.error=$_.Exception.ToString()} finally {
    if($terminal){
        [IO.File]::WriteAllText((Join-Path $ResultDir 'ja-done'),'done')
        $terminal.CloseMainWindow()|Out-Null
        if(-not$terminal.WaitForExit(3000)){Stop-Process -Id $terminal.Id -ErrorAction SilentlyContinue}
    }
    if($notepad){$notepad.CloseMainWindow()|Out-Null}
    if($win){$win.Close()}
    $env:LOCALAPPDATA=$savedLocalAppData
    if(Test-Path -LiteralPath $ResultDir){$report|ConvertTo-Json -Depth 6|Set-Content (Join-Path $ResultDir 'ja-smoke-result.json') -Encoding UTF8}
}
if(-not$report.passed){exit 1}
