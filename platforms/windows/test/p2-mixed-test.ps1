# P2-44: Mixed profile acceptance for Windows.
#
# Runs as one suite of acceptance-test.ps1.  It has two parts:
#   1. protocol checks against the isolated engine that acceptance-test started:
#      English direct, per-segment candidates, long-word match, zhuyin smoke,
#      and neutral candidate metadata;
#   2. a WPF host check for direct English typing and a visible candidate window.
#
# Raw requests/responses are written to p2-mixed-result.json as a trace; the
# Linux-side harness makes the final PASS/FAIL decisions so the same checks can
# be replayed and self-tested without a VM.
param(
    [Parameter(Mandatory = $true)][string]$ResultDir,
    [Parameter(Mandatory = $true)][string]$ExpectedDLL,
    [string]$Pipe
)
$ErrorActionPreference = 'Stop'
$report = [ordered]@{
    passed = $false
    checks = @()
    error = ''
    dll = ''
    protocol = [ordered]@{ ok = $false; error = ''; pipe = '' }
    host = [ordered]@{ ok = $false; error = ''; english_text = ''; zhuyin_text = ''; candidate_visible = $false; segment_switch_visible = $false; segment_candidate_start = $false; segment_candidate_end = $false; screenshots = @() }
    profile = $null
    trace = @()
    screenshots = @()
}
$trace = New-Object System.Collections.Generic.List[object]
$pipeStream = $null
$session = 0
$win = $null
$savedLocalAppData = ''
$saved = ''

function Read-Exact([System.IO.Stream]$stream, [int]$count) {
    $buffer = New-Object byte[] $count
    $offset = 0
    while ($offset -lt $count) {
        $read = $stream.Read($buffer, $offset, $count - $offset)
        if ($read -le 0) { throw 'engine disconnected while reading a frame' }
        $offset += $read
    }
    return ,$buffer
}
function Send-Json($value) {
    $bytes = [Text.Encoding]::UTF8.GetBytes(($value | ConvertTo-Json -Depth 12 -Compress))
    $header = [BitConverter]::GetBytes([uint32]$bytes.Length)
    $pipeStream.Write($header, 0, 4)
    $pipeStream.Write($bytes, 0, $bytes.Length)
    $pipeStream.Flush()
    $header = Read-Exact $pipeStream 4
    $length = [BitConverter]::ToUInt32($header, 0)
    if ($length -gt (1MB)) { throw "engine frame too large: $length" }
    $payload = Read-Exact $pipeStream ([int]$length)
    $reply = [Text.Encoding]::UTF8.GetString($payload) | ConvertFrom-Json
    if (-not $reply.ok) { throw ("engine replied: " + [string]$reply.error) }
    return $reply
}
function Exchange([string]$step, $request) {
    $reply = Send-Json ([ordered]@{ op = 'handle'; session = $session; req = $request })
    $trace.Add([ordered]@{ step = $step; request = $request; snapshot = $reply.snapshot }) | Out-Null
    return $reply
}
function Type-Keys([string]$prefix, [string]$keys) {
    foreach ($character in $keys.ToCharArray()) {
        Exchange ($prefix + '.' + $character) ([ordered]@{ type = 'key'; key = [string]$character }) | Out-Null
    }
}

function Get-Profiles {
    $clsid = '{138C35D0-7F08-4CEF-BA90-C34C16301528}'
    $root = "HKLM:\SOFTWARE\Microsoft\CTF\TIP\$clsid\LanguageProfile"
    $profiles = @()
    if (Test-Path -LiteralPath $root) {
        foreach ($language in (Get-ChildItem -LiteralPath $root -ErrorAction SilentlyContinue)) {
            foreach ($key in (Get-ChildItem -LiteralPath $language.PSPath -ErrorAction SilentlyContinue)) {
                $values = [ordered]@{}
                $item = Get-ItemProperty -LiteralPath $key.PSPath -ErrorAction SilentlyContinue
                $display = ''
                if ($item) {
                    foreach ($property in $item.PSObject.Properties) {
                        if ($property.Name -like 'PS*') { continue }
                        $values[$property.Name] = [string]$property.Value
                        if (-not $display -and ([string]$property.Value) -match 'Mixed') { $display = [string]$property.Value }
                    }
                }
                # msctf writes the display name as the key's default value.  Read
                # it explicitly because Get-ItemProperty does not expose it on
                # every Windows PowerShell 5.1 build.
                try {
                    $default = [string](Get-Item -LiteralPath $key.PSPath -ErrorAction Stop).GetValue('')
                    $values['(default)'] = $default
                    if (-not $display -and $default) { $display = $default }
                } catch {}
                $profiles += [ordered]@{
                    language = $language.PSChildName
                    guid = $key.PSChildName
                    display = $display
                    values = $values
                }
            }
        }
    }
    $tips = @()
    try { $tips = @((Get-WinUserLanguageList).InputMethodTips) } catch {}
    return [ordered]@{
        clsid = $clsid
        root = $root
        profiles = $profiles
        language_tips = $tips
    }
}

try {
    $report.profile = Get-Profiles
    # acceptance-test started the isolated engine for this suite; connect to it.
    if (-not $Pipe) {
        $sid = [Security.Principal.WindowsIdentity]::GetCurrent().User.Value.ToLowerInvariant()
        $Pipe = '\\.\pipe\0c-' + $sid
    }
    $report.protocol.pipe = $Pipe
    $deadline = (Get-Date).AddSeconds(10)
    while ($true) {
        try {
            $pipeStream = [IO.Pipes.NamedPipeClientStream]::new('.', ($Pipe -replace '^\\\\\.\\pipe\\', ''), [IO.Pipes.PipeDirection]::InOut)
            $pipeStream.Connect(2000)
            break
        } catch {
            if ($pipeStream) { $pipeStream.Dispose(); $pipeStream = $null }
            if ((Get-Date) -ge $deadline) { throw "isolated engine pipe not ready: $Pipe ($($_.Exception.Message))" }
            Start-Sleep -Milliseconds 300
        }
    }
    $open = Send-Json ([ordered]@{ op = 'open'; version = 0 })
    $session = [int64]$open.session
    if ($session -eq 0) { throw 'engine open reply has no session' }
    $report.protocol.ok = $true

    # 1. English types directly in the Mixed profile, without a mode toggle.
    Type-Keys 'english.direct' 'hello'
    Exchange 'english.direct' ([ordered]@{ type = 'key'; key = 'Enter' }) | Out-Null
    Exchange 'english.cancel' ([ordered]@{ type = 'cancel' }) | Out-Null

    # 2. Zhuyin smoke: the Windows shell path still commits 你好.
    Type-Keys 'zhuyin' 'su3cl3'
    Exchange 'zhuyin.commit' ([ordered]@{ type = 'key'; key = 'Enter' }) | Out-Null

    # 3. Segment cursor fixture: 麻婆 + 豆腐.  麻婆豆腐 is one lexicon match that
    #    covers four input slots; it must stay one candidate.  Moving the cursor
    #    to 豆腐 must re-anchor the candidate range to that segment.
    Type-Keys 'segment' 'a86qi62.4zj3'
    Exchange 'segment.compose' ([ordered]@{ type = 'cursor'; to = 4 }) | Out-Null
    Exchange 'segment.cursor0' ([ordered]@{ type = 'cursor'; to = 0 }) | Out-Null
    Exchange 'segment.cursor0.candidates' ([ordered]@{ type = 'candidates' }) | Out-Null
    Exchange 'segment.cursor2' ([ordered]@{ type = 'cursor'; to = 2 }) | Out-Null
    Exchange 'segment.cursor2.candidates' ([ordered]@{ type = 'candidates' }) | Out-Null
    Exchange 'segment.cancel' ([ordered]@{ type = 'cancel' }) | Out-Null
    $report.checks += 'protocol_trace'
    $report.protocol.ok = $true
} catch {
    $report.protocol.error = $_.Exception.ToString()
} finally {
    if ($pipeStream) { $pipeStream.Dispose() }
}

try {
    Add-Type -AssemblyName PresentationFramework, PresentationCore, WindowsBase, System.Windows.Forms, System.Drawing
    Add-Type @'
using System;
using System.Runtime.InteropServices;
public static class P2WpfProbe {
  [ComImport, Guid("71C6E74C-0F28-11D8-A82A-00065B84435C"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
  interface IProfiles { [PreserveSig] int ActivateProfile(uint type, ushort language, ref Guid clsid, ref Guid profile, IntPtr hkl, uint flags); }
  [StructLayout(LayoutKind.Sequential)] public struct RECT { public int Left, Top, Right, Bottom; }
  [DllImport("user32.dll")] public static extern IntPtr GetForegroundWindow();
  [DllImport("user32.dll", CharSet=CharSet.Unicode)] public static extern IntPtr FindWindow(string cls, string title);
  [DllImport("user32.dll")] public static extern bool IsWindowVisible(IntPtr hwnd);
  [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr hwnd, out RECT rect);
  [DllImport("user32.dll")] static extern void keybd_event(byte key, byte scan, uint flags, UIntPtr extra);
  public static void Latin(string text) { foreach(char c in text) { byte k=(byte)char.ToUpperInvariant(c); keybd_event(k,0,0,UIntPtr.Zero); keybd_event(k,0,2,UIntPtr.Zero); } }
  public static void Activate() {
    object obj = Activator.CreateInstance(Type.GetTypeFromCLSID(new Guid("33C53A50-F456-4884-B049-85FD643ECFED")));
    try {
      Guid clsid = new Guid("138C35D0-7F08-4CEF-BA90-C34C16301528"), profile = new Guid("EF62F743-9855-4C94-A658-1F0F98B7B840");
      Marshal.ThrowExceptionForHR(((IProfiles)obj).ActivateProfile(1, 0x0404, ref clsid, ref profile, IntPtr.Zero, 0x10000000));
    } finally { Marshal.FinalReleaseComObject(obj); }
  }
}
'@
    $savedLocalAppData = $env:LOCALAPPDATA
    $saved = $savedLocalAppData
    $env:LOCALAPPDATA = $ResultDir
    New-Item -ItemType Directory -Force (Join-Path $ResultDir 'Zuin') | Out-Null
    New-Item -ItemType File -Force (Join-Path $ResultDir 'Zuin\tsf-debug') | Out-Null
    $win = New-Object Windows.Window
    $win.Title = '0c P2-44 Mixed acceptance'
    $win.Width = 700
    $win.Height = 260
    $win.Topmost = $true
    $box = New-Object Windows.Controls.TextBox
    $box.FontSize = 28
    $box.AcceptsReturn = $true
    $win.Content = $box
    $win.Show()
    $win.Activate() | Out-Null
    $box.Focus() | Out-Null
    $handle = (New-Object Windows.Interop.WindowInteropHelper($win)).Handle
    function Pump([int]$ms) {
        $frame = New-Object Windows.Threading.DispatcherFrame
        $timer = New-Object Windows.Threading.DispatcherTimer
        $timer.Interval = [TimeSpan]::FromMilliseconds($ms)
        $timer.add_Tick({ $frame.Continue = $false }.GetNewClosure())
        $timer.Start()
        try { [Windows.Threading.Dispatcher]::PushFrame($frame) } finally { $timer.Stop() }
    }
    function Shot([string]$name) {
        $bitmap = New-Object Windows.Media.Imaging.RenderTargetBitmap([int]$win.ActualWidth, [int]$win.ActualHeight, 96, 96, [Windows.Media.PixelFormats]::Pbgra32)
        $bitmap.Render($win)
        $encoder = New-Object Windows.Media.Imaging.PngBitmapEncoder
        $encoder.Frames.Add([Windows.Media.Imaging.BitmapFrame]::Create($bitmap))
        $path = Join-Path $ResultDir $name
        $stream = [IO.File]::Create($path)
        try { $encoder.Save($stream) } finally { $stream.Dispose() }
        $report.screenshots += $name
    }
    function Capture-Candidate([string]$name) {
        $candidate = [P2WpfProbe]::FindWindow('ZuinCandidateWindow', 'Zuin')
        if ($candidate -eq [IntPtr]::Zero -or -not [P2WpfProbe]::IsWindowVisible($candidate)) { return $null }
        $rect = [P2WpfProbe+RECT]::new()
        if (-not [P2WpfProbe]::GetWindowRect($candidate, [ref]$rect)) { return $null }
        $width = $rect.Right - $rect.Left
        $height = $rect.Bottom - $rect.Top
        if ($width -le 0 -or $height -le 0) { return $null }
        $bitmap = New-Object Drawing.Bitmap $width, $height
        $graphics = [Drawing.Graphics]::FromImage($bitmap)
        try {
            $graphics.CopyFromScreen($rect.Left, $rect.Top, 0, 0, $bitmap.Size)
            $path = Join-Path $ResultDir $name
            $bitmap.Save($path, [Drawing.Imaging.ImageFormat]::Png)
        } finally {
            $graphics.Dispose()
            $bitmap.Dispose()
        }
        $report.screenshots += $name
        return (Get-FileHash -LiteralPath (Join-Path $ResultDir $name) -Algorithm SHA256).Hash
    }
    Pump 500
    [P2WpfProbe]::Activate()
    Pump 800
    if ([P2WpfProbe]::GetForegroundWindow() -ne $handle) { throw 'WPF test window lost focus' }
    [P2WpfProbe]::Latin('hello')
    Pump 900
    $report.host.english_text = $box.Text
    Shot 'p2-wpf-english.png'
    if ($box.Text -ne 'hello') { throw "direct English expected [hello], got [$($box.Text)]" }
    $report.checks += 'wpf_english_direct'
    $box.Clear()
    Pump 200
    if ([P2WpfProbe]::GetForegroundWindow() -ne $handle) { throw 'WPF test window lost focus' }
    [Windows.Forms.SendKeys]::SendWait('su3cl3')
    Pump 700
    [Windows.Forms.SendKeys]::SendWait('{ENTER}')
    Pump 700
    $report.host.zhuyin_text = $box.Text
    if ($box.Text -ne '你好') { throw "zhuyin smoke expected [你好], got [$($box.Text)]" }
    $report.checks += 'wpf_zhuyin_commit'
    $box.Clear()
    Pump 200
    [Windows.Forms.SendKeys]::SendWait('su3{DOWN}')
    Pump 700
    $candidate = [P2WpfProbe]::FindWindow('ZuinCandidateWindow', 'Zuin')
    $report.host.candidate_visible = ($candidate -ne [IntPtr]::Zero -and [P2WpfProbe]::IsWindowVisible($candidate))
    Shot 'p2-wpf-candidate.png'
    if (-not $report.host.candidate_visible) { throw 'WPF candidate window is not visible' }
    $report.checks += 'wpf_candidate_visible'

    # P2-41 shell check: with no candidate window open, moving the composition
    # cursor to another segment reopens candidates for that segment.  Capture
    # the candidate window in both positions and require different pixels.
    [Windows.Forms.SendKeys]::SendWait('{ESC}')
    Pump 300
    $box.Clear()
    Pump 200
    [Windows.Forms.SendKeys]::SendWait('a86qi62.4zj3')
    Pump 900
    [Windows.Forms.SendKeys]::SendWait('{HOME}')
    Pump 900
    $startHash = Capture-Candidate 'p2-wpf-segment-start.png'
    $report.host.segment_candidate_start = [bool]$startHash
    if ($startHash) {
        [Windows.Forms.SendKeys]::SendWait('{ESC}')
        Pump 400
        [Windows.Forms.SendKeys]::SendWait('{END}')
        Pump 900
        $endHash = Capture-Candidate 'p2-wpf-segment-end.png'
        $report.host.segment_candidate_end = [bool]$endHash
        $report.host.segment_switch_visible = ($report.host.segment_candidate_start -and
            $report.host.segment_candidate_end -and $startHash -ne $endHash)
    }
    if ($report.host.segment_switch_visible) {
        $report.checks += 'wpf_segment_candidates_follow_cursor'
    }
    [Windows.Forms.SendKeys]::SendWait('{ESC}')
    Pump 300
    $box.Clear()
    Pump 200

    $module = (Get-Process -Id $PID).Modules | Where-Object ModuleName -eq '0c_tsf.dll' | Select-Object -First 1
    $report.dll = $module.FileName
    if ($report.dll -ne $ExpectedDLL) { throw "host loaded unexpected DLL: $($report.dll)" }
    $report.checks += 'new_version_dll_loaded'
    $report.host.ok = $true
} catch {
    $report.host.error = $_.Exception.ToString()
} finally {
    if ($win) { $win.Close() }
    if ($saved) { $env:LOCALAPPDATA = $saved }
}
$report.trace = @($trace)
$report.passed = ($report.protocol.ok -and $report.host.ok -and -not $report.protocol.error -and -not $report.host.error)
if (-not $report.passed) { $report.error = (($report.protocol.error, $report.host.error) | Where-Object { $_ }) -join "`n" }
$report | ConvertTo-Json -Depth 40 | Set-Content -LiteralPath (Join-Path $ResultDir 'p2-mixed-result.json') -Encoding UTF8
if (-not $report.passed) { exit 1 }
