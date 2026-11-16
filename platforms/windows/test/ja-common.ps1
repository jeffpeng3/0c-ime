# T29 日文驗收共用工具；由 ja-*-test.ps1 以「. (Join-Path $PSScriptRoot 'ja-common.ps1')」載入。
# 只操作目前測試視窗與暫存引擎；不讀寫日常詞庫、不修改語言清單。
# 日文 profile 的 GUID 由 T28 註冊，這裡從 HKCU 的 TSF 登錄查詢，不在腳本裡寫死。
$script:JaClsid='138C35D0-7F08-4CEF-BA90-C34C16301528'
$script:JaProfileRoot="HKCU:\Software\Microsoft\CTF\TIP\$($script:JaClsid)\LanguageProfile\0x00000411"
$script:JaProfileCategory='34745C63-B2F0-4784-8B67-5E12C8701A31'

# 回傳目前註冊的日文 TSF profile GUID。沒有或超過一個時直接失敗，避免驗收到別人的 profile。
function Get-JaProfileGuid {
    if(-not(Test-Path -LiteralPath $script:JaProfileRoot)){throw 'Japanese TSF profile is not registered; install the ja-JP profile (T28) before this suite'}
    $profiles=@(Get-ChildItem -LiteralPath $script:JaProfileRoot -ErrorAction Stop|ForEach-Object {$_.PSChildName})
    if($profiles.Count -eq 0){throw 'Japanese TSF profile registry key is empty; reinstall the ja-JP profile'}
    if($profiles.Count -gt 1){throw ('More than one Japanese TSF profile is registered: '+($profiles -join ', '))}
    try {return [guid]$profiles[0]} catch {throw "Japanese TSF profile GUID is invalid: $($profiles[0])"}
}

# 回傳兩個語言 profile 是否都在登錄裡；ja-profile-test 及總表用。
function Get-JaProfileState {
    $zh="HKCU:\Software\Microsoft\CTF\TIP\$($script:JaClsid)\LanguageProfile\0x00000404"
    $jaGuid=''
    if(Test-Path -LiteralPath $script:JaProfileRoot){$jaGuid=@(Get-ChildItem -LiteralPath $script:JaProfileRoot|ForEach-Object {$_.PSChildName}) -join ','}
    return [ordered]@{
        zh_registered=Test-Path -LiteralPath $zh
        ja_registered=Test-Path -LiteralPath $script:JaProfileRoot
        ja_guid=$jaGuid
    }
}

# 載入一次 P/Invoke 與 COM 宣告。JaAcceptanceProbe 是本檔專用的型別名稱，重複載入不衝突。
function Initialize-JaProbe {
    if('JaAcceptanceProbe' -as [type]){return}
    Add-Type -ReferencedAssemblies System.Drawing,System.Windows.Forms -TypeDefinition @'
using System;
using System.Collections.Generic;
using System.Drawing;
using System.Runtime.InteropServices;
using System.Text;

public static class JaAcceptanceProbe {
    [ComImport, Guid("71C6E74C-0F28-11D8-A82A-00065B84435C"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
    interface IProfiles {
        [PreserveSig] int ActivateProfile(uint type, ushort language, ref Guid clsid, ref Guid profile, IntPtr hkl, uint flags);
        [PreserveSig] int DeactivateProfile(uint type, ushort language, ref Guid clsid, ref Guid profile, IntPtr hkl, uint flags);
        [PreserveSig] int GetProfile(uint type, ushort language, ref Guid clsid, ref Guid profile, IntPtr hkl, out Profile result);
        [PreserveSig] int EnumProfiles(ushort language, out IntPtr profiles);
        [PreserveSig] int ReleaseInputProcessor(ref Guid clsid, uint flags);
        [PreserveSig] int RegisterProfile(IntPtr unused);
        [PreserveSig] int UnregisterProfile(ref Guid clsid, ushort language, ref Guid profile, uint flags);
        [PreserveSig] int GetActiveProfile(ref Guid category, out Profile result);
    }
    [StructLayout(LayoutKind.Sequential)] public struct Profile {
        public uint type; public ushort language; public Guid clsid, profile, category;
        public IntPtr substitute; public uint caps; public IntPtr layout; public uint flags;
    }
    [StructLayout(LayoutKind.Sequential)] public struct RECT { public int Left, Top, Right, Bottom; }
    [StructLayout(LayoutKind.Sequential)] struct KEYBDINPUT { public ushort vk, scan; public uint flags, time; public IntPtr extra; }
    [StructLayout(LayoutKind.Sequential)] struct MOUSEINPUT { public int dx, dy; public uint mouseData, flags, time; public IntPtr extra; }
    [StructLayout(LayoutKind.Explicit)] struct INPUTUNION { [FieldOffset(0)] public MOUSEINPUT mi; [FieldOffset(0)] public KEYBDINPUT ki; }
    [StructLayout(LayoutKind.Sequential)] struct INPUT { public uint type; public INPUTUNION u; }
    public delegate bool EnumProc(IntPtr h, IntPtr l);

    [DllImport("user32.dll")] public static extern IntPtr GetForegroundWindow();
    [DllImport("user32.dll")] public static extern uint GetWindowThreadProcessId(IntPtr h, out uint pid);
    [DllImport("user32.dll")] public static extern IntPtr GetKeyboardLayout(uint thread);
    [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr h);
    [DllImport("user32.dll")] public static extern bool BringWindowToTop(IntPtr h);
    [DllImport("user32.dll")] public static extern bool ShowWindow(IntPtr h, int cmd);
    [DllImport("user32.dll")] public static extern bool AttachThreadInput(uint a, uint b, bool attach);
    [DllImport("kernel32.dll")] public static extern uint GetCurrentThreadId();
    [DllImport("user32.dll")] static extern uint SendInput(uint count, INPUT[] inputs, int size);
    [DllImport("user32.dll", CharSet=CharSet.Unicode)] public static extern IntPtr FindWindow(string cls, string title);
    [DllImport("user32.dll")] public static extern bool PostMessage(IntPtr h, uint msg, IntPtr w, IntPtr l);
    [DllImport("user32.dll")] public static extern bool IsWindowVisible(IntPtr h);
    [DllImport("user32.dll")] public static extern uint GetDpiForWindow(IntPtr h);
    [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr h, out RECT r);
    [DllImport("user32.dll")] public static extern bool GetClientRect(IntPtr h, out RECT r);
    [DllImport("user32.dll")] static extern bool SetCursorPos(int x, int y);
    [DllImport("user32.dll")] static extern void mouse_event(uint flags, uint dx, uint dy, uint data, UIntPtr extra);
    [DllImport("user32.dll")] public static extern bool PrintWindow(IntPtr h, IntPtr dc, uint flags);
    [DllImport("user32.dll", CharSet=CharSet.Unicode)] public static extern int GetClassName(IntPtr h, StringBuilder s, int n);
    [DllImport("user32.dll", CharSet=CharSet.Unicode)] public static extern int GetWindowText(IntPtr h, StringBuilder s, int n);
    [DllImport("user32.dll")] public static extern bool EnumWindows(EnumProc p, IntPtr l);
    public static string Title(IntPtr h) { var s = new StringBuilder(1024); GetWindowText(h, s, 1024); return s.ToString(); }
    public static IntPtr FindWindowContaining(string marker) {
        IntPtr found = IntPtr.Zero;
        EnumWindows((h, l) => {
            if (IsWindowVisible(h) && Title(h).Contains(marker)) { found = h; return false; }
            return true;
        }, IntPtr.Zero);
        return found;
    }

    static INPUT Key(ushort vk, bool up) {
        INPUT i = new INPUT(); i.type = 1; i.u.ki.vk = vk; i.u.ki.scan = 0;
        i.u.ki.flags = up ? 2u : 0u;
        return i;
    }
    static void Send(INPUT[] inputs) {
        if (SendInput((uint)inputs.Length, inputs, Marshal.SizeOf(typeof(INPUT))) != (uint)inputs.Length)
            throw new System.ComponentModel.Win32Exception();
    }
    // 一次送完整個按鍵序列：Shift+Tab 的 Shift 狀態在 Tab 被處理時一定還是按著，不必在 UI 執行緒 sleep。
    public static void TypeText(string text) {
        var inputs = new List<INPUT>();
        foreach (char c in text) {
            ushort vk = (ushort)char.ToUpperInvariant(c);
            inputs.Add(Key(vk, false)); inputs.Add(Key(vk, true));
        }
        Send(inputs.ToArray());
    }
    public static void Tap(ushort vk) { Send(new INPUT[] { Key(vk, false), Key(vk, true) }); }
    public static void ShiftTab() {
        Send(new INPUT[] { Key(0xA0, false), Key(0x09, false), Key(0x09, true), Key(0xA0, true) });
    }
    public static void WinSpace() {
        Send(new INPUT[] { Key(0x5B, false), Key(0x20, false), Key(0x20, true), Key(0x5B, true) });
    }
    public static void WinS() {
        Send(new INPUT[] { Key(0x5B, false), Key(0x53, false), Key(0x53, true), Key(0x5B, true) });
    }
    public static void ShiftDown(bool down) { Send(new INPUT[] { Key(0xA0, !down) }); }
    public static void Ctrl(ushort vk) {
        Send(new INPUT[] { Key(0xA2, false), Key(vk, false), Key(vk, true), Key(0xA2, true) });
    }
    public static void CtrlS() { Ctrl(0x53); }
    public static void CtrlA() { Ctrl(0x41); }

    public static int Activate(string clsid, string profile, ushort language, uint flags) {
        object o = Activator.CreateInstance(System.Type.GetTypeFromCLSID(new Guid("33C53A50-F456-4884-B049-85FD643ECFED")));
        try {
            Guid c = new Guid(clsid), p = new Guid(profile);
            return ((IProfiles)o).ActivateProfile(1, language, ref c, ref p, IntPtr.Zero, flags);
        } finally { Marshal.FinalReleaseComObject(o); }
    }
    public static Profile Active(string category) {
        object o = Activator.CreateInstance(System.Type.GetTypeFromCLSID(new Guid("33C53A50-F456-4884-B049-85FD643ECFED")));
        try {
            Guid cat = new Guid(category);
            Profile p;
            int hr = ((IProfiles)o).GetActiveProfile(ref cat, out p);
            if (hr < 0) Marshal.ThrowExceptionForHR(hr);
            return p;
        } finally { Marshal.FinalReleaseComObject(o); }
    }
    // 目前前景視窗的執行緒用的鍵盤排列；低 16 位是語言 ID（0x0404 注音、0x0411 日文）。
    public static int LanguageId(IntPtr hwnd) {
        uint pid;
        uint tid = GetWindowThreadProcessId(hwnd, out pid);
        return (int)(GetKeyboardLayout(tid).ToInt64() & 0xFFFF);
    }
    public static uint ForegroundPid() {
        uint pid;
        GetWindowThreadProcessId(GetForegroundWindow(), out pid);
        return pid;
    }
    public static void Focus(IntPtr h) {
        uint pid;
        uint fg = GetWindowThreadProcessId(GetForegroundWindow(), out pid);
        uint tid = GetWindowThreadProcessId(h, out pid);
        uint me = GetCurrentThreadId();
        if (me != tid) AttachThreadInput(me, tid, true);
        if (me != fg) AttachThreadInput(me, fg, true);
        ShowWindow(h, 9); BringWindowToTop(h); SetForegroundWindow(h);
        if (me != fg) AttachThreadInput(me, fg, false);
        if (me != tid) AttachThreadInput(me, tid, false);
    }
    public static string Class(IntPtr h) {
        var s = new StringBuilder(256); GetClassName(h, s, 256); return s.ToString();
    }
    // 可見的候選窗：0c-ui 的 ZuinCandidateWindow，或 Chromium 宿主行程內的 0cCandidateInProcess。
    public static IntPtr Candidate() {
        IntPtr found = IntPtr.Zero;
        EnumWindows((h, l) => {
            if (!IsWindowVisible(h)) return true;
            string c = Class(h);
            if (c != "ZuinCandidateWindow" && c != "0cCandidateInProcess") return true;
            RECT r; if (!GetWindowRect(h, out r) || r.Right <= r.Left + 20) return true;
            found = h; return false;
        }, IntPtr.Zero);
        return found;
    }
    // 只擷取指定視窗的 client 區域；不碰其他視窗，也不含桌面其他內容。
    public static Bitmap Capture(IntPtr hwnd) {
        RECT r; if (!GetClientRect(hwnd, out r)) throw new Exception("client bounds unavailable");
        var b = new Bitmap(r.Right, r.Bottom);
        using (var g = Graphics.FromImage(b)) {
            IntPtr dc = g.GetHdc();
            try { if (!PrintWindow(hwnd, dc, 3)) throw new Exception("window capture failed"); }
            finally { g.ReleaseHdc(dc); }
        }
        return b;
    }
    // 把點擊送到候選窗的第 row 列（0 起算，標題不算一列）。實際列高由 0c-ui 的 dpi 換算。
    public static void ClickCandidate(IntPtr hwnd, int row) {
        uint dpi = GetDpiForWindow(hwnd);
        int x = (int)(20 * dpi / 96);
        int y = (int)((8 + 20 + row * 28 + 14) * dpi / 96);
        PostMessage(hwnd, 0x202, IntPtr.Zero, (IntPtr)((y << 16) | x));
    }
    // 在視窗 client 座標點一下，讓瀏覽器頁面取得焦點（Edge 啟動時網址列可能還拿著焦點）。
    public static void ClickClient(IntPtr hwnd, int x, int y) {
        RECT r; if (!GetWindowRect(hwnd, out r)) throw new Exception("window bounds unavailable");
        SetCursorPos(r.Left + x, r.Top + y);
        mouse_event(0x0002, 0, 0, 0, UIntPtr.Zero);
        mouse_event(0x0004, 0, 0, 0, UIntPtr.Zero);
    }
}
'@
}

# 切到日文 profile。SessionWide=true 用 TF_TMAE_SESSION 旗標（影響整個工作階段的宿主，例如搜尋框）。
function Invoke-JaProfile {
    param([bool]$SessionWide=$false)
    Initialize-JaProbe
    $guid=Get-JaProfileGuid
    $flags=if($SessionWide){0x20000000}else{0x10000000}
    $hr=[JaAcceptanceProbe]::Activate($script:JaClsid,$guid.ToString(),0x0411,$flags)
    if($hr -ne 0){throw ('ActivateProfile(ja) failed 0x{0:X8}' -f $hr)}
    return $guid
}

function Get-JaActiveProfile {
    Initialize-JaProbe
    return [JaAcceptanceProbe]::Active($script:JaProfileCategory)
}

# 等前景視窗變成指定視窗；外部程式（記事本、Terminal、Edge）啟動時用。
function Wait-JaForeground {
    param([IntPtr]$Handle,[int]$TimeoutMs=5000)
    $deadline=(Get-Date).AddMilliseconds($TimeoutMs)
    do {
        if([JaAcceptanceProbe]::GetForegroundWindow() -eq $Handle){return $true}
        Start-Sleep -Milliseconds 80
    } while((Get-Date) -lt $deadline)
    return $false
}

function Wait-JaCandidate {
    param([int]$TimeoutMs=3000)
    $deadline=(Get-Date).AddMilliseconds($TimeoutMs)
    do {
        $candidate=[JaAcceptanceProbe]::Candidate()
        if($candidate -ne [IntPtr]::Zero){return $candidate}
        Start-Sleep -Milliseconds 80
    } while((Get-Date) -lt $deadline)
    return [IntPtr]::Zero
}

# 擷取候選窗畫面並回傳 SHA-256；用來確認 Space／Tab 真的換了候選或文節，不依賴日文候選內容。
function Get-JaWindowHash {
    param([IntPtr]$Handle)
    $bitmap=[JaAcceptanceProbe]::Capture($Handle)
    try {
        $stream=New-Object IO.MemoryStream
        try {
            $bitmap.Save($stream,[Drawing.Imaging.ImageFormat]::Png)
            $sha=[Security.Cryptography.SHA256]::Create()
            try {return [BitConverter]::ToString($sha.ComputeHash($stream.ToArray())).Replace('-','')}
            finally {$sha.Dispose()}
        } finally {$stream.Dispose()}
    } finally {$bitmap.Dispose()}
}

function Format-JaProfile($Value) {
    return ('{0:x4}:{1}' -f $Value.language,$Value.profile)
}
