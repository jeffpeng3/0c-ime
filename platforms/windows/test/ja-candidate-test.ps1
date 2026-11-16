# T29 日文候選窗驗收：文節底線與目前文節、候選窗內容、Space 換候選、PageUp／PageDown、滑鼠選字。
# 由 acceptance-test.ps1 -Suite ja-candidate 啟動；只操作自己的 WPF 視窗與暫存引擎。
param([Parameter(Mandatory=$true)][string]$ResultDir,[Parameter(Mandatory=$true)][string]$ExpectedDLL)
$ErrorActionPreference='Stop'
. (Join-Path $PSScriptRoot 'ja-common.ps1')
$report=[ordered]@{passed=$false;checks=@();error='';candidate_rect=@();hashes=[ordered]@{};dll=''}
$win=$null
$savedLocalAppData=$env:LOCALAPPDATA
try {
    Add-Type -AssemblyName PresentationFramework,PresentationCore,WindowsBase,System.Windows.Forms,System.Drawing
    Initialize-JaProbe
    $env:LOCALAPPDATA=$ResultDir
    New-Item -ItemType Directory -Force (Join-Path $ResultDir 'Zuin')|Out-Null
    New-Item -ItemType File -Force (Join-Path $ResultDir 'Zuin\tsf-debug')|Out-Null
    $win=New-Object Windows.Window
    $win.Title='0c Japanese candidate acceptance';$win.Width=820;$win.Height=280;$win.Topmost=$true
    $box=New-Object Windows.Controls.TextBox
    $box.FontSize=32;$box.Padding=[Windows.Thickness]::new(8);$box.CaretBrush=[Windows.Media.Brushes]::Transparent
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
    Invoke-JaProfile|Out-Null
    Pump 1200
    function FocusGuard {if([JaAcceptanceProbe]::GetForegroundWindow() -ne $handle){throw 'WPF test window lost focus; refusing to type'}}
    function Send([string]$Text){FocusGuard;[JaAcceptanceProbe]::TypeText($Text);Pump 650}
    function Key([int]$VirtualKey){FocusGuard;[JaAcceptanceProbe]::Tap([uint16]$VirtualKey);Pump 550}
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
    function SaveBox([string]$Name) {
        $bitmap=New-Object Windows.Media.Imaging.RenderTargetBitmap([int]$box.ActualWidth,[int]$box.ActualHeight,96,96,[Windows.Media.PixelFormats]::Pbgra32)
        $bitmap.Render($box)
        $encoder=New-Object Windows.Media.Imaging.PngBitmapEncoder
        $encoder.Frames.Add([Windows.Media.Imaging.BitmapFrame]::Create($bitmap))|Out-Null
        $stream=[IO.File]::Create((Join-Path $ResultDir $Name))
        try{$encoder.Save($stream)}finally{$stream.Dispose()}
    }
    # 重新啟動一次測試段落；同一個暫存引擎，選字學習會影響排序，所以每個段落用不同讀音。
    function Compose([string]$Text) {
        $box.Clear();Pump 200
        Send $Text
        Key 0x20
        $candidate=Wait-JaCandidate
        if($candidate -eq [IntPtr]::Zero){throw "candidate window missing after Space for [$Text]"}
        return $candidate
    }
    # 1. 文節底線與目前文節：Tab 前後只移動底線／候選，文字不變。
    $candidate=Compose 'kuukigaii'
    $converted=$box.Text
    if($converted -ceq 'くうきがいい'){throw 'conversion stayed in kana; cannot verify clause display'}
    $report.checks+='ja_candidate_conversion_visible'
    $report.hashes.clause1=BoxHash
    SaveBox 'candidate-clause-1.png'
    $candidateHash1=Get-JaWindowHash $candidate
    Key 0x09
    $candidate=Wait-JaCandidate
    if($candidate -eq [IntPtr]::Zero){throw 'candidate window disappeared after Tab'}
    if($box.Text -cne $converted){throw 'Tab committed or changed the preedit text'}
    $report.hashes.clause2=BoxHash
    SaveBox 'candidate-clause-2.png'
    if($report.hashes.clause1 -eq $report.hashes.clause2){throw 'Tab did not visibly move the active clause underline'}
    $report.checks+='ja_candidate_tab_moves_active_clause'
    $candidateHash2=Get-JaWindowHash $candidate
    if($candidateHash1 -eq $candidateHash2){throw 'Tab did not change the candidate window for the next clause'}
    $report.checks+='ja_candidate_tab_changes_candidates'
    [JaAcceptanceProbe]::ShiftTab();Pump 550
    $candidate=Wait-JaCandidate
    if($candidate -eq [IntPtr]::Zero){throw 'candidate window disappeared after Shift+Tab'}
    if($box.Text -cne $converted){throw 'Shift+Tab committed or changed the preedit text'}
    $report.hashes.clauseBack=BoxHash
    if($report.hashes.clauseBack -eq $report.hashes.clause2){throw 'Shift+Tab did not visibly move the active clause underline back'}
    $report.checks+='ja_candidate_shift_tab_previous_clause'
    # 2. 候選窗內容：視窗在畫面內，而且不是空白視窗。
    $bounds=New-Object JaAcceptanceProbe+RECT
    [JaAcceptanceProbe]::GetWindowRect($candidate,[ref]$bounds)|Out-Null
    $report.candidate_rect=@($bounds.Left,$bounds.Top,$bounds.Right,$bounds.Bottom)
    $area=[Windows.Forms.Screen]::FromHandle($handle).WorkingArea
    if($bounds.Left -lt $area.Left -or $bounds.Top -lt $area.Top -or $bounds.Right -gt $area.Right -or $bounds.Bottom -gt $area.Bottom){throw 'candidate window extends past the work area'}
    $bitmap=[JaAcceptanceProbe]::Capture($candidate)
    try {
        if($bitmap.Width -lt 40 -or $bitmap.Height -lt 40){throw 'candidate window is too small to contain a title and a candidate'}
        $ink=0
        for($x=0;$x -lt $bitmap.Width -and $ink -lt 200;$x+=2){
            for($y=0;$y -lt $bitmap.Height -and $ink -lt 200;$y+=2){
                $pixel=$bitmap.GetPixel($x,$y)
                if($pixel.R -lt 200 -or $pixel.G -lt 200 -or $pixel.B -lt 200){$ink++}
            }
        }
        if($ink -lt 40){throw 'candidate window appears blank'}
    } finally {$bitmap.Dispose()}
    $report.checks+='ja_candidate_window_content'
    # 3. Space 換候選：組字區換成下一個候選，但還沒送出；候選窗內容要變。
    Key 0x20
    $candidate=Wait-JaCandidate
    if($candidate -eq [IntPtr]::Zero){throw 'candidate window disappeared after Space'}
    $spaceHash=Get-JaWindowHash $candidate
    if(-not$box.Text -or $box.Text -notmatch '[\u3040-\u30ff\u4e00-\u9fff]'){throw 'Space committed or cleared the conversion instead of changing candidates'}
    if($spaceHash -eq $candidateHash2){throw 'Space did not change the candidate selection'}
    $report.checks+='ja_candidate_space_next_candidate'
    # 4. 翻頁鍵：沒有第二頁時按鍵仍要由核心吃掉，不能送出組字。
    Key 0x22 # VK_NEXT (PageDown)
    $candidate=Wait-JaCandidate
    if($candidate -eq [IntPtr]::Zero){throw 'candidate window disappeared after PageDown'}
    $pageDownHash=Get-JaWindowHash $candidate
    if(-not$box.Text -or $box.Text -notmatch '[\u3040-\u30ff\u4e00-\u9fff]'){throw 'PageDown committed or cleared the conversion'}
    Key 0x21 # VK_PRIOR (PageUp)
    $candidate=Wait-JaCandidate
    if($candidate -eq [IntPtr]::Zero){throw 'candidate window disappeared after PageUp'}
    if(-not$box.Text -or $box.Text -notmatch '[\u3040-\u30ff\u4e00-\u9fff]'){throw 'PageUp committed or cleared the conversion'}
    $report.hashes.pageDown=$pageDownHash
    $report.hashes.pageUp=Get-JaWindowHash $candidate
    $report.checks+='ja_candidate_page_keys_consumed'
    if($pageDownHash -ne $report.hashes.pageUp){$report.checks+='ja_candidate_paging_changed'}
    # 5. 滑鼠選第二列；選字後組字區應換成該候選，Enter 送出的就是它。
    $client=New-Object JaAcceptanceProbe+RECT
    [JaAcceptanceProbe]::GetClientRect($candidate,[ref]$client)|Out-Null
    $dpi=[JaAcceptanceProbe]::GetDpiForWindow($candidate)
    if($client.Bottom -lt [int](8*$dpi/96)+[int](20*$dpi/96)+[int](28*$dpi/96)){throw 'fewer than two candidates; cannot test mouse selection'}
    $beforeClick=$box.Text
    $beforeClickHash=Get-JaWindowHash $candidate
    [JaAcceptanceProbe]::ClickCandidate($candidate,1);Pump 800
    $afterClick=$box.Text
    $afterClickHash=Get-JaWindowHash $candidate
    if($afterClick -ceq $beforeClick -and $afterClickHash -eq $beforeClickHash){throw 'mouse click on the second candidate had no visible effect'}
    $report.checks+='ja_candidate_mouse_selection'
    Key 0x0D
    if($box.Text -cne $afterClick){throw "Enter after mouse selection committed [$($box.Text)], preedit was [$afterClick]"}
    $report.checks+='ja_candidate_mouse_commit'
    # 6. 送出後沒有殘留的空候選窗。
    $deadline=(Get-Date).AddSeconds(2)
    while([JaAcceptanceProbe]::IsWindowVisible($candidate) -and (Get-Date) -lt $deadline){Pump 100}
    if([JaAcceptanceProbe]::IsWindowVisible($candidate)){throw 'candidate window remained visible after commit'}
    $report.checks+='ja_candidate_closes_after_commit'
    $module=(Get-Process -Id $PID).Modules|Where-Object ModuleName -eq '0c_tsf.dll'|Select-Object -First 1
    $report.dll=$module.FileName
    if($report.dll -ne $ExpectedDLL){throw "unexpected DLL: $($report.dll)"}
    $report.checks+='ja_candidate_dll_loaded'
    $report.passed=$true
} catch {$report.error=$_.Exception.ToString()} finally {
    if($win){$win.Close()}
    $env:LOCALAPPDATA=$savedLocalAppData
    if(Test-Path -LiteralPath $ResultDir){$report|ConvertTo-Json -Depth 6|Set-Content (Join-Path $ResultDir 'ja-candidate-result.json') -Encoding UTF8}
}
if(-not$report.passed){exit 1}
