# 以本地建置的 x64 DLL 註冊 TSF（開發用）。
#
# 固定路徑與語言清單照 README「套件安裝與更新」「固定路徑」：
#   %LOCALAPPDATA%\Zuin\tsf\0c_tsf.dll
#   input.dll!InstallLayoutOrTip("0404:{CLSID}{profile}", 0)
# 完整套件請用 install.ps1（委派 0c-update.exe）；這支只處理已建好的 DLL。
# DllRegisterServer 會寫 HKLM\SOFTWARE\Microsoft\CTF\TIP 與 HKCR\CLSID，需要系統管理員。
# 語言清單寫在目前帳號的 HKCU；提升權限仍會留在同一個帳號。
[CmdletBinding()]
param(
    [string]$DllPath = (Join-Path $PSScriptRoot 'target\x86_64-pc-windows-gnu\release\0c_tsf.dll')
)
$ErrorActionPreference = 'Stop'

$Clsid = '{138C35D0-7F08-4CEF-BA90-C34C16301528}'
$ProfileGuid = '{EF62F743-9855-4C94-A658-1F0F98B7B840}'
$MixedTip = "0404:$Clsid$ProfileGuid"
# 舊版（T28）ja-JP 的 profile 不再註冊；清掉語言清單裡殘留的項目。
$LegacyJaTip = "0411:${Clsid}{3D6A8B21-5C47-4F9E-B0D2-71E4A9C8F653}"
$Base = Join-Path $env:LOCALAPPDATA 'Zuin'
$FixedDir = Join-Path $Base 'tsf'
$FixedDll = Join-Path $FixedDir '0c_tsf.dll'
$Marker = Join-Path $Base 'update\input-list'
$InprocKey = "Registry::HKEY_CLASSES_ROOT\CLSID\$Clsid\InprocServer32"
$ProfileKey = "HKLM:\SOFTWARE\Microsoft\CTF\TIP\$Clsid\LanguageProfile\0x00000404\$ProfileGuid"

function Assert-Administrator {
    $principal = [Security.Principal.WindowsPrincipal]::new([Security.Principal.WindowsIdentity]::GetCurrent())
    if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
        throw '請以系統管理員的 PowerShell 執行；DllRegisterServer 要寫 HKLM 與 HKCR。'
    }
}

# 只接受 x64 PE：這一輪不建 i686 DLL，避免把錯誤的產物複製到固定路徑。
function Assert-X64Pe([string]$Path) {
    $stream = [IO.File]::OpenRead($Path)
    try {
        $reader = [IO.BinaryReader]::new($stream)
        $stream.Position = 0x3c
        $pe = $reader.ReadInt32()
        $stream.Position = $pe
        if ($reader.ReadUInt32() -ne 0x00004550) { throw "$Path 不是 PE 檔。" }
        $machine = $reader.ReadUInt16()
        if ($machine -ne 0x8664) { throw ('{0} 不是 x64 PE（machine 0x{1:x4}）。' -f $Path, $machine) }
    } finally {
        $stream.Dispose()
    }
}

# 照固定路徑的換檔方式：先複製成 .new、核對雜湊，再把舊檔改名成 .old-*，最後換上 .new。
function Copy-FixedDll([string]$Source, [string]$Target) {
    $dir = Split-Path -Parent $Target
    New-Item -ItemType Directory -Force -Path $dir | Out-Null
    $staging = "$Target.new"
    Remove-Item -LiteralPath $staging -Force -ErrorAction SilentlyContinue
    Copy-Item -LiteralPath $Source -Destination $staging -Force
    if ((Get-FileHash -LiteralPath $Source).Hash -ne (Get-FileHash -LiteralPath $staging).Hash) {
        Remove-Item -LiteralPath $staging -Force -ErrorAction SilentlyContinue
        throw '複製到固定路徑後雜湊不符。'
    }
    if (Test-Path -LiteralPath $Target) {
        $old = "$Target.old-" + (Get-Date -Format 'yyyyMMddHHmmssfff')
        Move-Item -LiteralPath $Target -Destination $old -Force
        try {
            Move-Item -LiteralPath $staging -Destination $Target -Force
        } catch {
            Move-Item -LiteralPath $old -Destination $Target -Force
            throw
        }
        # 仍被宿主載入時刪不掉，留給之後的安裝或重開機後清理，不影響註冊。
        Remove-Item -LiteralPath $old -Force -ErrorAction SilentlyContinue
    } else {
        Move-Item -LiteralPath $staging -Destination $Target -Force
    }
}

function Register-Dll([string]$Path) {
    $regsvr = Join-Path $env:SystemRoot 'System32\regsvr32.exe'
    $proc = Start-Process -FilePath $regsvr -ArgumentList @('/s', ('"{0}"' -f $Path)) -Wait -PassThru
    if ($proc.ExitCode -ne 0) { throw "regsvr32 失敗（$($proc.ExitCode)）：$Path" }
    if (-not (Test-Path -LiteralPath $InprocKey)) { throw 'regsvr32 結束了，但 CLSID 的 InprocServer32 沒有建立。' }
    $registered = (Get-Item -LiteralPath $InprocKey).GetValue('')
    if ($registered -ne $Path) { throw "InprocServer32 指向 $registered，預期 $Path。" }
    if (-not (Test-Path -LiteralPath $ProfileKey)) { throw "Mixed profile 沒有登記到 $ProfileKey。" }
}

function Initialize-LanguageList {
    if (-not ('ZuinDevLanguageList' -as [type])) {
        Add-Type @'
using System.Runtime.InteropServices;
public static class ZuinDevLanguageList {
    [DllImport("input.dll", CharSet = CharSet.Unicode)]
    public static extern int InstallLayoutOrTip(string psz, uint flags);
}
'@
    }
}

function Get-LanguageTips {
    return @(Get-WinUserLanguageList | ForEach-Object { $_.InputMethodTips })
}

# 旗標 0 加入、1（ILOT_UNINSTALL）移除；兩者都不改預設輸入法。
function Set-TipListed([string]$Tip, [bool]$Present) {
    $flags = if ($Present) { [uint32]0 } else { [uint32]1 }
    if ([ZuinDevLanguageList]::InstallLayoutOrTip($Tip, $flags) -eq 0) {
        throw ('InstallLayoutOrTip({0}, {1}) 失敗。' -f $Tip, $flags)
    }
}

Assert-Administrator
if (-not (Test-Path -LiteralPath $DllPath -PathType Leaf)) {
    throw "找不到 $DllPath；先執行 cargo build --release --target x86_64-pc-windows-gnu -p zuin-tsf。"
}
$DllPath = (Resolve-Path -LiteralPath $DllPath).Path
Assert-X64Pe $DllPath

Copy-FixedDll $DllPath $FixedDll
Register-Dll $FixedDll

Initialize-LanguageList
$listed = Get-LanguageTips
if ($listed -contains $LegacyJaTip) { Set-TipListed $LegacyJaTip $false }
if ($listed -notcontains $MixedTip) { Set-TipListed $MixedTip $true }
if ((Get-LanguageTips) -notcontains $MixedTip) { throw "Mixed profile 沒有出現在目前帳號的語言清單：$MixedTip" }

# 與 0c-update 相同的記號：之後的安裝或更新不會再重複加入使用者自己移除的項目。
New-Item -ItemType Directory -Force -Path (Split-Path -Parent $Marker) | Out-Null
Set-Content -LiteralPath $Marker -Value ($MixedTip + "`n") -NoNewline -Encoding ascii

Write-Host "已註冊：$FixedDll"
Write-Host "Mixed profile：$MixedTip"
Write-Host '註冊與 Win+Space 切換只能在 Windows 桌面驗證；這支腳本只在 Windows 上執行。'
