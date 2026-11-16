# T29 日文沙箱與 Chromium 宿主驗收：Edge、Windows 搜尋框、WebView2。
# 只用專用瀏覽器設定目錄與本機頁面；搜尋框只讀本次輸入的文字，結束後關閉搜尋並移除沙箱日誌。
# WebView2 執行階段或 SDK 不可用時標為 skipped，不算失敗（與 webview2-test.ps1 相同）。
param([Parameter(Mandatory=$true)][string]$ResultDir,[Parameter(Mandatory=$true)][string]$ExpectedDLL)
$ErrorActionPreference='Stop'
. (Join-Path $PSScriptRoot 'ja-common.ps1')
$report=[ordered]@{passed=$false;skipped='';checks=@();error='';hosts=[ordered]@{};dll=''}
$form=$null;$controller=$null;$edge=$null;$createdSearchLog=$false
$savedLocalAppData=$env:LOCALAPPDATA
$searchSandbox=Join-Path $savedLocalAppData 'Packages\MicrosoftWindows.Client.CBS_cw5n1h2txyewy\AC'
$searchLog=Join-Path $searchSandbox 'Zuin'
try {
    Add-Type -AssemblyName System.Windows.Forms,System.Drawing,UIAutomationClient,UIAutomationTypes
    Initialize-JaProbe
    $env:LOCALAPPDATA=$ResultDir
    New-Item -ItemType Directory -Force (Join-Path $ResultDir 'Zuin')|Out-Null
    New-Item -ItemType File -Force (Join-Path $ResultDir 'Zuin\tsf-debug')|Out-Null
    function Start-Await($Task,[string]$What){
        $watch=[Diagnostics.Stopwatch]::StartNew()
        while(-not$Task.IsCompleted -and $watch.ElapsedMilliseconds -lt 20000){[Windows.Forms.Application]::DoEvents();Start-Sleep -Milliseconds 10}
        if(-not$Task.IsCompleted){throw "$What timed out"}
        if($Task.IsFaulted){throw "$What failed: $($Task.Exception.InnerException.Message)"}
        return $Task
    }
    function Shot([string]$Name,[IntPtr]$Handle=[IntPtr]::Zero){
        if($Handle -ne [IntPtr]::Zero){
            $r=New-Object JaAcceptanceProbe+RECT;[JaAcceptanceProbe]::GetWindowRect($Handle,[ref]$r)|Out-Null
            $left=$r.Left;$top=$r.Top;$w=$r.Right-$r.Left;$h=$r.Bottom-$r.Top
        } else {
            $screen=[Windows.Forms.Screen]::PrimaryScreen.Bounds
            $left=$screen.Left;$top=$screen.Top;$w=$screen.Width;$h=$screen.Height
        }
        $bmp=New-Object Drawing.Bitmap $w,$h
        $g=[Drawing.Graphics]::FromImage($bmp);$g.CopyFromScreen($left,$top,0,0,$bmp.Size);$g.Dispose()
        $bmp.Save((Join-Path $ResultDir $Name),[Drawing.Imaging.ImageFormat]::Png);$bmp.Dispose()
    }

    # ---------- 1. Edge（Chromium） ----------
    $edgeExe=@(
        'C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe',
        'C:\Program Files\Microsoft\Edge\Application\msedge.exe'
    )|Where-Object {Test-Path -LiteralPath $_}|Select-Object -First 1
    if(-not$edgeExe){throw 'Microsoft Edge is not installed'}
    $marker='0cjaedge'+[guid]::NewGuid().ToString('N').Substring(0,6)
    $html=@"
<html><head><meta charset="utf-8"><title>$marker</title></head><body style="margin:24px">
<textarea id="text" autofocus style="font-size:28px;width:700px;height:140px"></textarea>
<script>
const t=document.getElementById('text');
setInterval(()=>{document.title='$marker|'+encodeURIComponent(t.value)+'|end'},80);
</script></body></html>
"@
    [IO.File]::WriteAllText((Join-Path $ResultDir 'ja-sandbox-edge.html'),$html,[Text.UTF8Encoding]::new($false))
    $url='file:///'+((Join-Path $ResultDir 'ja-sandbox-edge.html') -replace '\\','/')
    $edgeProfile=Join-Path $ResultDir 'edge-profile'
    New-Item -ItemType Directory -Force $edgeProfile|Out-Null
    Start-Process $edgeExe -ArgumentList @('--no-first-run','--no-default-browser-check','--disable-background-networking',('--user-data-dir="'+$edgeProfile+'"'),$url)|Out-Null
    $deadline=(Get-Date).AddSeconds(30)
    do {
        Start-Sleep -Milliseconds 200
        $edgeHandle=[JaAcceptanceProbe]::FindWindowContaining($marker)
    } while($edgeHandle -eq [IntPtr]::Zero -and (Get-Date) -lt $deadline)
    if($edgeHandle -eq [IntPtr]::Zero){throw 'Edge test window missing'}
    $edgePid=[uint32]0
    [JaAcceptanceProbe]::GetWindowThreadProcessId($edgeHandle,[ref]$edgePid)|Out-Null
    $edge=Get-Process -Id $edgePid -ErrorAction Stop
    [JaAcceptanceProbe]::Focus($edgeHandle)|Out-Null
    if(-not(Wait-JaForeground $edgeHandle 6000)){throw 'Edge did not become foreground'}
    Invoke-JaProfile -SessionWide $true|Out-Null
    Start-Sleep -Milliseconds 1500
    [JaAcceptanceProbe]::ClickClient($edgeHandle,300,120)
    Start-Sleep -Milliseconds 500
    function EdgeState {
        $title=[JaAcceptanceProbe]::Title($edgeHandle)
        $i=$title.IndexOf($marker+'|');$j=$title.IndexOf('|end')
        if($i -lt 0 -or $j -lt 0){return $null}
        return [Uri]::UnescapeDataString($title.Substring($i+$marker.Length+1,$j-$i-$marker.Length-1))
    }
    function ExpectEdge([string]$Expected,[string]$Name){
        $deadline=(Get-Date).AddSeconds(4)
        do {
            $actual=EdgeState
            if($null -ne$actual -and $actual -ceq $Expected){$report.checks+=$Name;return}
            Start-Sleep -Milliseconds 100
        } while((Get-Date) -lt $deadline)
        throw "$Name expected [$Expected], got [$actual]"
    }
    [JaAcceptanceProbe]::TypeText('nihongo');Start-Sleep -Milliseconds 1200
    ExpectEdge 'にほんご' 'ja_edge_romaji'
    [JaAcceptanceProbe]::Tap(0x20);Start-Sleep -Milliseconds 1000
    $candidate=Wait-JaCandidate
    if($candidate -eq [IntPtr]::Zero){throw 'Edge did not show the candidate window'}
    Shot 'ja-edge-candidate.png'
    $cand=New-Object JaAcceptanceProbe+RECT;[JaAcceptanceProbe]::GetWindowRect($candidate,[ref]$cand)|Out-Null
    $win=New-Object JaAcceptanceProbe+RECT;[JaAcceptanceProbe]::GetWindowRect($edgeHandle,[ref]$win)|Out-Null
    if($cand.Left -lt $win.Left -or $cand.Left -gt $win.Right -or $cand.Top -lt $win.Top -or $cand.Top -gt $win.Bottom){throw 'Edge candidate window is not near the page'}
    $report.checks+='ja_edge_candidate_near_field'
    [JaAcceptanceProbe]::Tap(0x31);Start-Sleep -Milliseconds 900
    [JaAcceptanceProbe]::Tap(0x0D);Start-Sleep -Milliseconds 1000
    $committed=EdgeState
    if(-not$committed -or $committed -notmatch '[\u3040-\u30ff\u4e00-\u9fff]' -or $committed -ceq 'にほんご'){throw "Edge did not commit a Japanese conversion: [$committed]"}
    $report.hosts.edge=[ordered]@{version=$edge.MainModule.FileVersionInfo.ProductVersion;commit=$committed}
    $report.checks+='ja_edge_commit'
    [JaAcceptanceProbe]::CtrlA();[JaAcceptanceProbe]::Tap(0x08);Start-Sleep -Milliseconds 500
    $edge.Refresh()
    $module=$edge.Modules|Where-Object ModuleName -eq '0c_tsf.dll'|Select-Object -First 1
    if(-not$module -or $module.FileName -ne $ExpectedDLL){throw "Edge loaded unexpected DLL: $($module.FileName)"}
    $report.checks+='ja_edge_dll_loaded'
    # 關掉這個專用設定目錄的 Edge 子行程。
    Get-CimInstance Win32_Process -Filter "Name='msedge.exe'"|Where-Object {$_.CommandLine -and $_.CommandLine.Contains($edgeProfile)}|ForEach-Object {Stop-Process -Id $_.ProcessId -ErrorAction SilentlyContinue}
    $edge=$null;Start-Sleep -Seconds 2

    # ---------- 2. Windows 搜尋框（SearchHost，AppContainer） ----------
    if(-not(Test-Path -LiteralPath $searchSandbox)){
        $report.hosts.search='skipped: SearchHost sandbox folder missing (Windows 11 search required)'
        $report.checks+='ja_search_skipped_windows_11_required'
    } else {
    if(-not(Test-Path -LiteralPath $searchLog)){New-Item -ItemType Directory -Force $searchLog|Out-Null;$createdSearchLog=$true}
    $searchLogFile=Join-Path $searchLog 'tsf.log'
    $searchStart=if(Test-Path -LiteralPath $searchLogFile){(Get-Item -LiteralPath $searchLogFile).Length}else{0}
    function SearchValue {
        $element=[Windows.Automation.AutomationElement]::FocusedElement
        $pattern=$null
        if($element -and $element.TryGetCurrentPattern([Windows.Automation.ValuePattern]::Pattern,[ref]$pattern)){return $pattern.Current.Value}
        return $null
    }
    Get-Process SearchHost -ErrorAction SilentlyContinue|Stop-Process -Force
    Start-Sleep -Seconds 3
    Invoke-JaProfile -SessionWide $true|Out-Null
    Start-Sleep -Milliseconds 800
    [JaAcceptanceProbe]::WinS();Start-Sleep -Seconds 3
    $deadline=(Get-Date).AddSeconds(6)
    do {
        $searchProcess=Get-Process SearchHost -ErrorAction SilentlyContinue|Select-Object -First 1
        $foregroundPid=[JaAcceptanceProbe]::ForegroundPid()
        if($searchProcess -and $foregroundPid -eq $searchProcess.Id){break}
        Start-Sleep -Milliseconds 150
    } while((Get-Date) -lt $deadline)
    if(-not$searchProcess){throw 'SearchHost did not start'}
    if([JaAcceptanceProbe]::ForegroundPid() -ne $searchProcess.Id){throw 'SearchHost search box is not focused'}
    [JaAcceptanceProbe]::TypeText('nihongo');Start-Sleep -Milliseconds 1500
    $composed=SearchValue
    $report.hosts.search=[ordered]@{composed=$composed}
    if($composed -cne 'にほんご'){throw "SearchHost did not compose Japanese: [$composed]"}
    $report.checks+='ja_search_composes'
    [JaAcceptanceProbe]::Tap(0x20);Start-Sleep -Milliseconds 1200
    $candidate=Wait-JaCandidate
    if($candidate -eq [IntPtr]::Zero){throw 'SearchHost did not show the in-process candidate window'}
    Shot 'ja-search-candidate.png'
    $report.checks+='ja_search_candidate'
    [JaAcceptanceProbe]::Tap(0x31);Start-Sleep -Milliseconds 1200
    $selected=SearchValue
    $report.hosts.search.selected=$selected
    if(-not$selected -or $selected -ceq 'にほんご' -or $selected -notmatch '[\u3040-\u30ff\u4e00-\u9fff]'){throw "SearchHost did not select a Japanese candidate: [$selected]"}
    $report.checks+='ja_search_selects'
    [JaAcceptanceProbe]::Tap(0x1B);Start-Sleep -Milliseconds 700
    $afterEscape=SearchValue
    if($null -ne$afterEscape -and $afterEscape -cne ''){throw 'Escape did not clear the search composition'}
    $report.checks+='ja_search_escape_clear'
    $searchProcess=Get-Process SearchHost -ErrorAction SilentlyContinue|Select-Object -First 1
    if(-not$searchProcess){throw 'SearchHost exited before the DLL check'}
    $module=$searchProcess.Modules|Where-Object ModuleName -eq '0c_tsf.dll'|Select-Object -First 1
    if(-not$module -or $module.FileName -ne $ExpectedDLL){throw "SearchHost loaded unexpected DLL: $($module.FileName)"}
    $report.checks+='ja_search_dll_loaded'
    if(Test-Path -LiteralPath $searchLogFile){
        $text=Get-Content -LiteralPath $searchLogFile -Raw -Encoding UTF8
        $text=if($text.Length -gt $searchStart){$text.Substring([int][Math]::Min($searchStart,$text.Length))}else{''}
        $band=[regex]::Match($text,'宿主行程內候選窗（層級 (\d+)）')
        if($band.Success -and [int]$band.Groups[1].Value -gt 1){$report.checks+='ja_search_host_layer_candidate'}
    }
    Get-Process SearchHost -ErrorAction SilentlyContinue|Stop-Process -Force
    Start-Sleep -Seconds 1
    }

    # ---------- 3. WebView2（新版 Teams／Outlook 的視窗結構） ----------
    $sdkVersion='1.0.4129.50'
    $sdkHash='D3934F482D484B89FB4825DF720C710664E1143A1E90F7B3A60794EF33F473D2'
    $root=(Resolve-Path (Join-Path $PSScriptRoot '..\..\..')).Path
    $sdk=Join-Path $root "build\windows\webview2-sdk\$sdkVersion"
    $needed=@('Microsoft.Web.WebView2.Core.dll','WebView2Loader.dll')
    $webviewAvailable=$true
    if(@($needed|Where-Object {-not(Test-Path (Join-Path $sdk $_))}).Count -gt 0){
        New-Item -ItemType Directory -Force $sdk|Out-Null
        $package=Join-Path $sdk 'package.nupkg'
        [Net.ServicePointManager]::SecurityProtocol=[Net.SecurityProtocolType]::Tls12
        try {
            Invoke-WebRequest -UseBasicParsing "https://api.nuget.org/v3-flatcontainer/microsoft.web.webview2/$sdkVersion/microsoft.web.webview2.$sdkVersion.nupkg" -OutFile $package
        } catch {
            Remove-Item $package -ErrorAction SilentlyContinue
            $report.hosts.webview2='skipped: WebView2 SDK could not be downloaded from NuGet'
            $report.checks+='ja_webview2_skipped_sdk'
            $webviewAvailable=$false
            $package=$null
        }
        if($package){
            if((Get-FileHash $package -Algorithm SHA256).Hash -ne $sdkHash){Remove-Item $package;throw 'WebView2 SDK hash mismatch'}
            Add-Type -AssemblyName System.IO.Compression.FileSystem
            $zip=[IO.Compression.ZipFile]::OpenRead($package)
            try {
                foreach($entry in $zip.Entries){
                    if($entry.FullName -in @('lib/net462/Microsoft.Web.WebView2.Core.dll','runtimes/win-x64/native/WebView2Loader.dll')){
                        [IO.Compression.ZipFileExtensions]::ExtractToFile($entry,(Join-Path $sdk $entry.Name),$true)
                    }
                }
            } finally {$zip.Dispose()}
        }
    }
    if($webviewAvailable -and (Test-Path (Join-Path $sdk 'Microsoft.Web.WebView2.Core.dll'))){
        $env:PATH="$sdk;$env:PATH"
        try {Add-Type -Path (Join-Path $sdk 'Microsoft.Web.WebView2.Core.dll') -ErrorAction Stop}
        catch {
            $report.hosts.webview2='skipped: WebView2 SDK assembly could not be loaded'
            $report.checks+='ja_webview2_skipped_sdk'
            $webviewAvailable=$false
        }
        $runtime=$null
        if($webviewAvailable){try {$runtime=[Microsoft.Web.WebView2.Core.CoreWebView2Environment]::GetAvailableBrowserVersionString()} catch {}}
        if($webviewAvailable -and -not$runtime){
            $report.hosts.webview2='skipped: WebView2 runtime is not installed'
            $report.checks+='ja_webview2_skipped_runtime'
        } elseif($webviewAvailable) {
            $form=New-Object Windows.Forms.Form
            $form.Text='0c Japanese WebView2 acceptance';$form.Width=900;$form.Height=520;$form.StartPosition='CenterScreen';$form.TopMost=$true
            $form.Show();[Windows.Forms.Application]::DoEvents()
            $environment=(Start-Await ([Microsoft.Web.WebView2.Core.CoreWebView2Environment]::CreateAsync($null,(Join-Path $ResultDir 'webview2-data'),$null)) 'WebView2 environment').Result
            $controller=(Start-Await $environment.CreateCoreWebView2ControllerAsync($form.Handle) 'WebView2 controller').Result
            $controller.Bounds=$form.ClientRectangle;$controller.IsVisible=$true
            $core=$controller.CoreWebView2
            $core.NavigateToString('<html><head><meta charset="utf-8"></head><body style="margin:24px"><h2>0c Japanese WebView2 acceptance</h2><textarea id="text" style="font-size:24px;width:650px;height:140px"></textarea></body></html>')
            $deadline=(Get-Date).AddSeconds(15)
            do {
                [Windows.Forms.Application]::DoEvents();Start-Sleep -Milliseconds 100
                $ready=(Start-Await $core.ExecuteScriptAsync('document.readyState==="complete" && !!document.getElementById("text")') 'page').Result|ConvertFrom-Json
            } while($ready -ne $true -and (Get-Date) -lt $deadline)
            [JaAcceptanceProbe]::Focus($form.Handle)|Out-Null
            $controller.MoveFocus([Microsoft.Web.WebView2.Core.CoreWebView2MoveFocusReason]::Programmatic)
            (Start-Await $core.ExecuteScriptAsync('document.getElementById("text").focus();true') 'focus').Result|Out-Null
            Start-Sleep -Milliseconds 800
            Invoke-JaProfile -SessionWide $true|Out-Null
            Start-Sleep -Milliseconds 1200
            function WebValue {return ((Start-Await $core.ExecuteScriptAsync('document.getElementById("text").value') 'value').Result|ConvertFrom-Json)}
            function ExpectWeb([string]$Expected,[string]$Name){
                $deadline=(Get-Date).AddSeconds(4)
                do {
                    $actual=WebValue
                    if($actual -ceq $Expected){$report.checks+=$Name;return}
                    Start-Sleep -Milliseconds 100
                } while((Get-Date) -lt $deadline)
                throw "$Name expected [$Expected], got [$actual]"
            }
            [JaAcceptanceProbe]::TypeText('nihongo');Start-Sleep -Milliseconds 1200
            ExpectWeb 'にほんご' 'ja_webview2_romaji'
            [JaAcceptanceProbe]::Tap(0x20);Start-Sleep -Milliseconds 1000
            $candidate=Wait-JaCandidate
            if($candidate -eq [IntPtr]::Zero){throw 'WebView2 did not show the candidate window'}
            Shot 'ja-webview2-candidate.png'
            $cand=New-Object JaAcceptanceProbe+RECT;[JaAcceptanceProbe]::GetWindowRect($candidate,[ref]$cand)|Out-Null
            $frame=New-Object JaAcceptanceProbe+RECT;[JaAcceptanceProbe]::GetWindowRect($form.Handle,[ref]$frame)|Out-Null
            if($cand.Left -lt $frame.Left -or $cand.Left -gt $frame.Right -or $cand.Top -lt $frame.Top -or $cand.Top -gt $frame.Bottom){throw 'WebView2 candidate window is not near the text field'}
            $report.checks+='ja_webview2_candidate'
            [JaAcceptanceProbe]::Tap(0x31);Start-Sleep -Milliseconds 900
            [JaAcceptanceProbe]::Tap(0x0D);Start-Sleep -Milliseconds 1000
            $committed=WebValue
            if(-not$committed -or $committed -ceq 'にほんご' -or $committed -notmatch '[\u3040-\u30ff\u4e00-\u9fff]'){throw "WebView2 did not commit Japanese: [$committed]"}
            $report.hosts.webview2=[ordered]@{runtime=$runtime;commit=$committed}
            $report.checks+='ja_webview2_commit'
            # 鍵盤焦點在另一個 msedgewebview2 行程，和 Teams 相同；核對有行程載入本測試的 DLL。
            $webProcesses=@(Get-Process msedgewebview2 -ErrorAction SilentlyContinue)
            if($webProcesses.Count -eq 0){throw 'WebView2 host process is not a separate msedgewebview2.exe'}
            $module=$null
            foreach($web in $webProcesses){
                $candidateModule=$web.Modules|Where-Object ModuleName -eq '0c_tsf.dll'|Select-Object -First 1
                if($candidateModule -and $candidateModule.FileName -eq $ExpectedDLL){$module=$candidateModule;break}
            }
            if(-not$module){throw 'no msedgewebview2 process loaded the expected 0c_tsf.dll'}
            $report.checks+='ja_webview2_dll_loaded'
        }
    }
    $report.passed=$true
} catch {$report.error=$_.Exception.ToString()} finally {
    try {[JaAcceptanceProbe]::Tap(0x1B);[JaAcceptanceProbe]::Tap(0x1B)} catch {}
    if($controller){try{$controller.Close()}catch{}}
    if($form){try{$form.Close();$form.Dispose()}catch{}}
    Get-CimInstance Win32_Process -Filter "Name='msedge.exe'"|Where-Object {$_.CommandLine -and $_.CommandLine.Contains($ResultDir)}|ForEach-Object {Stop-Process -Id $_.ProcessId -ErrorAction SilentlyContinue}
    if($createdSearchLog){
        Get-Process SearchHost -ErrorAction SilentlyContinue|Stop-Process -Force
        Start-Sleep -Seconds 2
        if(Test-Path (Join-Path $searchLog 'tsf.log')){Copy-Item (Join-Path $searchLog 'tsf.log') (Join-Path $ResultDir 'ja-search-tsf.log')}
        Remove-Item -LiteralPath $searchLog -Recurse -Force -ErrorAction SilentlyContinue
    }
    $env:LOCALAPPDATA=$savedLocalAppData
    if(Test-Path -LiteralPath $ResultDir){$report|ConvertTo-Json -Depth 6|Set-Content (Join-Path $ResultDir 'ja-sandbox-result.json') -Encoding UTF8}
}
if(-not$report.passed){exit 1}
