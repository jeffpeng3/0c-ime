# T29 日文語言 profile 驗收：Win+Space 在 zh-TW／ja-JP 之間切換、每個程式記住 profile、新程式從設定的語言開始。
# 只切換作用中的 profile；不新增、不移除語言清單項目，finally 回到原本的作用中 profile。
param([Parameter(Mandatory=$true)][string]$ResultDir,[Parameter(Mandatory=$true)][string]$ExpectedDLL)
$ErrorActionPreference='Stop'
. (Join-Path $PSScriptRoot 'ja-common.ps1')
$report=[ordered]@{passed=$false;checks=@();error='';profiles=[ordered]@{};languages=[ordered]@{};apps=[ordered]@{};dll='';language_list_preserved=$false}
$win=$null;$notepad=$null
$savedLocalAppData=$env:LOCALAPPDATA
$originalProfile=$null
try {
    Add-Type -AssemblyName PresentationFramework,PresentationCore,WindowsBase,System.Windows.Forms
    Initialize-JaProbe
    $env:LOCALAPPDATA=$ResultDir
    New-Item -ItemType Directory -Force (Join-Path $ResultDir 'Zuin')|Out-Null
    New-Item -ItemType File -Force (Join-Path $ResultDir 'Zuin\tsf-debug')|Out-Null
    $report.profiles=Get-JaProfileState
    $jaGuid=Get-JaProfileGuid
    if(-not$report.profiles.zh_registered){throw 'zh-TW TSF profile is not registered'}
    if(-not$report.profiles.ja_registered){throw 'ja-JP TSF profile is not registered'}
    $report.checks+='ja_profile_windows_registered'
    $zhTip="0404:{$($script:JaClsid)}{EF62F743-9855-4C94-A658-1F0F98B7B840}"
    $jaTip="0411:{$($script:JaClsid)}{$jaGuid}"
    function Get-LanguageTips {
        try {return @(Get-WinUserLanguageList|ForEach-Object {$_.InputMethodTips})} catch {return @()}
    }
    $tipsBefore=Get-LanguageTips
    $report.languages.tips_before=$tipsBefore
    if($tipsBefore.Count -gt 0){
        if($tipsBefore -notcontains $zhTip){throw "zh-TW TIP is missing from the Windows language list: $zhTip"}
        if($tipsBefore -notcontains $jaTip){throw "ja-JP TIP is missing from the Windows language list: $jaTip"}
        $report.checks+='ja_profile_language_list'
    } else {
        $report.languages.note='Get-WinUserLanguageList unavailable; registry check only'
    }
    # 系統設定的開始輸入法：有 InputMethodOverride 就用它，否則用語言清單第一個 TIP。
    $override=(Get-ItemProperty 'HKCU:\Control Panel\International\User Profile' -ErrorAction SilentlyContinue).InputMethodOverride
    $startTip=if($override){[string]$override}else{if($tipsBefore.Count -gt 0){$tipsBefore[0]}else{''}}
    $startLanguage=if($startTip -match '^([0-9A-Fa-f]{4}):'){[Convert]::ToInt32($matches[1],16)}else{0}
    $report.languages.start_tip=$startTip
    $report.languages.start_language=('0x{0:X4}' -f $startLanguage)
    $win=New-Object Windows.Window
    $win.Title='0c Japanese profile acceptance';$win.Width=700;$win.Height=220;$win.Topmost=$true
    $box=New-Object Windows.Controls.TextBox;$box.FontSize=28
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
    function FocusWindow([IntPtr]$Target){
        [JaAcceptanceProbe]::Focus($Target)|Out-Null
        Pump 400
        if([JaAcceptanceProbe]::GetForegroundWindow() -ne $Target){throw "window $Target lost foreground"}
    }
    function ExpectLanguage([IntPtr]$Target,[int]$Language,[string]$Name){
        if([JaAcceptanceProbe]::LanguageId($Target) -ne $Language){throw "$Name expected language 0x$('{0:X4}' -f $Language), got 0x$('{0:X4}' -f [JaAcceptanceProbe]::LanguageId($Target))"}
        $report.checks+=$Name
    }
    # Win+Space 可能還要經過其他已安裝的語言；最多按 Max 次，回到起點就失敗。
    function Switch-Language([IntPtr]$Target,[int]$Language,[string]$Name,[int]$Max=8){
        $start=[JaAcceptanceProbe]::LanguageId($Target)
        if($start -eq $Language){$report.checks+=$Name;return}
        for($i=1;$i -le $Max;$i++){
            [JaAcceptanceProbe]::WinSpace();Pump 1200
            $now=[JaAcceptanceProbe]::LanguageId($Target)
            if($now -eq $Language){$report.apps[$Name]=$i;$report.checks+=$Name;return}
            if($now -eq $start -and $i -gt 1){break}
        }
        throw ("Win+Space did not reach 0x{0:X4} from 0x{1:X4}; switch hotkey may be disabled" -f $Language,$start)
    }
    Pump 500
    FocusWindow $handle
    $originalProfile=Get-JaActiveProfile
    $report.languages.original=Format-JaProfile $originalProfile
    # 1. Win+Space 在兩個 profile 之間切換。先確定注音作用中。
    $zhGuid=[guid]'EF62F743-9855-4C94-A658-1F0F98B7B840'
    $hr=[JaAcceptanceProbe]::Activate($script:JaClsid,$zhGuid.ToString(),0x0404,0x10000000)
    if($hr -ne 0){throw ('ActivateProfile(zh) failed 0x{0:X8}' -f $hr)}
    Pump 600
    if((Get-JaActiveProfile).language -ne 0x0404){throw 'could not activate the zh-TW profile as the starting point'}
    Switch-Language $handle 0x0411 'ja_profile_win_space_to_japanese'
    $after=Get-JaActiveProfile
    if($after.language -ne 0x0411 -or $after.profile -ne $jaGuid){throw ('Win+Space reached ja-JP language, but not the 0c profile: '+ (Format-JaProfile $after))}
    Switch-Language $handle 0x0404 'ja_profile_win_space_to_chinese'
    $back=Get-JaActiveProfile
    if($back.language -ne 0x0404 -or $back.profile -ne $zhGuid){throw ('Win+Space reached zh-TW language, but not the 0c profile: '+ (Format-JaProfile $back))}
    # 2. 每個程式記住自己的 profile：WPF 留在日文，記事本切到注音，來回焦點不互相覆蓋。
    Invoke-JaProfile|Out-Null
    Pump 600
    if((Get-JaActiveProfile).language -ne 0x0411){throw 'WPF window did not stay on ja-JP'}
    if(Get-Process notepad -ErrorAction SilentlyContinue){throw 'Existing Notepad is open; refusing to use user tabs'}
    $stem='0c-ja-profile-'+[guid]::NewGuid().ToString('N')
    $file=Join-Path $ResultDir ($stem+'.txt')
    [IO.File]::WriteAllText($file,'',[Text.UTF8Encoding]::new($true))
    Start-Process notepad.exe -ArgumentList ('"'+$file+'"')|Out-Null
    $deadline=(Get-Date).AddSeconds(25)
    do {
        Start-Sleep -Milliseconds 200
        $notepad=Get-Process notepad -ErrorAction SilentlyContinue|Where-Object MainWindowTitle -like "*$stem*"|Select-Object -First 1
    } while(-not$notepad -and (Get-Date) -lt $deadline)
    if(-not$notepad){throw 'own Notepad window missing'}
    $noteHandle=$notepad.MainWindowHandle
    FocusWindow $noteHandle
    $noteStart=[JaAcceptanceProbe]::LanguageId($noteHandle)
    $report.apps.notepad_start=('0x{0:X4}' -f $noteStart)
    if($startLanguage -ne 0 -and $noteStart -ne $startLanguage){throw ("new Notepad started with 0x{0:X4}, configured start is 0x{1:X4}" -f $noteStart,$startLanguage)}
    $report.checks+='ja_profile_new_app_starts_configured_language'
    # 記事本切到注音。
    Switch-Language $noteHandle 0x0404 'ja_profile_notepad_switch_to_chinese'
    $report.apps.notepad_after_switch='0x0404'
    # 回到 WPF：仍是日文（沒有被記事本的切換改掉）。
    FocusWindow $handle
    if((Get-JaActiveProfile).language -ne 0x0411){throw 'focusing the WPF window changed its remembered ja-JP profile'}
    $report.checks+='ja_profile_per_app_memory_wpf'
    # 再回記事本：仍是注音。
    FocusWindow $noteHandle
    ExpectLanguage $noteHandle 0x0404 'ja_profile_per_app_memory_notepad'
    $notepad.CloseMainWindow()|Out-Null
    $notepad=$null
    Pump 500
    # 3. 新程式從設定的語言開始：焦點在 WPF 的日文時，新記事本不應繼承 WPF 的日文。
    if($startLanguage -ne 0){
        $stem2='0c-ja-profile-new-'+[guid]::NewGuid().ToString('N')
        $file2=Join-Path $ResultDir ($stem2+'.txt')
        [IO.File]::WriteAllText($file2,'',[Text.UTF8Encoding]::new($true))
        Start-Process notepad.exe -ArgumentList ('"'+$file2+'"')|Out-Null
        $deadline=(Get-Date).AddSeconds(25)
        do {
            Start-Sleep -Milliseconds 200
            $notepad=Get-Process notepad -ErrorAction SilentlyContinue|Where-Object MainWindowTitle -like "*$stem2*"|Select-Object -First 1
        } while(-not$notepad -and (Get-Date) -lt $deadline)
        if(-not$notepad){throw 'second Notepad window missing'}
        FocusWindow $notepad.MainWindowHandle
        $newStart=[JaAcceptanceProbe]::LanguageId($notepad.MainWindowHandle)
        $report.apps.notepad2_start=('0x{0:X4}' -f $newStart)
        if($newStart -ne $startLanguage){throw ("new app started with 0x{0:X4}, configured start is 0x{1:X4}" -f $newStart,$startLanguage)}
        $report.checks+='ja_profile_new_process_not_inherited'
        $notepad.CloseMainWindow()|Out-Null
        $notepad=$null
    }
    # 4. 語言清單只在開始時讀取，finally 再讀一次確認沒有被本測試改動。
    $module=(Get-Process -Id $PID).Modules|Where-Object ModuleName -eq '0c_tsf.dll'|Select-Object -First 1
    $report.dll=$module.FileName
    if($report.dll -ne $ExpectedDLL){throw "unexpected DLL: $($report.dll)"}
    $report.checks+='ja_profile_dll_loaded'
    $report.passed=$true
} catch {$report.error=$_.Exception.ToString()} finally {
    # 回到測試前的 profile；不碰語言清單。
    try {
        if($originalProfile -and $originalProfile.profile -ne [guid]::Empty){
            [JaAcceptanceProbe]::Activate($script:JaClsid,$originalProfile.profile.ToString(),[uint16]$originalProfile.language,0x20000000)|Out-Null
        }
    } catch {}
    try {
        $tipsAfter=@(Get-WinUserLanguageList|ForEach-Object {$_.InputMethodTips})
        $report.languages.tips_after=$tipsAfter
        $report.language_list_preserved=(($tipsAfter -join "`n") -ceq (@($report.languages.tips_before) -join "`n"))
        if(-not$report.language_list_preserved){$report.passed=$false;$report.error+='; language list changed during the test'}
    } catch {}
    if($notepad){$notepad.CloseMainWindow()|Out-Null}
    if($win){$win.Close()}
    $env:LOCALAPPDATA=$savedLocalAppData
    if(Test-Path -LiteralPath $ResultDir){$report|ConvertTo-Json -Depth 6|Set-Content (Join-Path $ResultDir 'ja-profile-result.json') -Encoding UTF8}
}
if(-not$report.passed){exit 1}
