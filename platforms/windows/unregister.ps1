# 反向移除 register.ps1 的註冊：語言清單、TSF／COM 註冊、固定路徑 DLL 與 input-list 記號。
# 需以系統管理員的 PowerShell 執行（DllUnregisterServer 刪 HKLM 與 HKCR）。
[CmdletBinding()]
param()
$ErrorActionPreference = 'Stop'

$Clsid = '{138C35D0-7F08-4CEF-BA90-C34C16301528}'
$ProfileGuid = '{EF62F743-9855-4C94-A658-1F0F98B7B840}'
$MixedTip = "0404:$Clsid$ProfileGuid"
$LegacyJaTip = "0411:${Clsid}{3D6A8B21-5C47-4F9E-B0D2-71E4A9C8F653}"
$Base = Join-Path $env:LOCALAPPDATA 'Zuin'
$FixedDir = Join-Path $Base 'tsf'
$FixedDll = Join-Path $FixedDir '0c_tsf.dll'
$Marker = Join-Path $Base 'update\input-list'
$InprocKey = "Registry::HKEY_CLASSES_ROOT\CLSID\$Clsid\InprocServer32"

function Assert-Administrator {
    $principal = [Security.Principal.WindowsPrincipal]::new([Security.Principal.WindowsIdentity]::GetCurrent())
    if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
        throw '請以系統管理員的 PowerShell 執行；DllUnregisterServer 要刪 HKLM 與 HKCR。'
    }
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

# 旗標 1 是 ILOT_UNINSTALL；項目本來就不在清單裡時不呼叫。
function Remove-Tip([string]$Tip) {
    if ((Get-LanguageTips) -notcontains $Tip) { return }
    if ([ZuinDevLanguageList]::InstallLayoutOrTip($Tip, [uint32]1) -eq 0) {
        throw "InstallLayoutOrTip($Tip, 1) 失敗。"
    }
}

function Unregister-Dll([string]$Path) {
    $regsvr = Join-Path $env:SystemRoot 'System32\regsvr32.exe'
    $proc = Start-Process -FilePath $regsvr -ArgumentList @('/u', '/s', ('"{0}"' -f $Path)) -Wait -PassThru
    if ($proc.ExitCode -ne 0) { throw "regsvr32 /u 失敗（$($proc.ExitCode)）：$Path" }
    if (Test-Path -LiteralPath $InprocKey) { throw 'DllUnregisterServer 執行後 CLSID 的 InprocServer32 仍然存在。' }
}

# DLL 已經被刪掉時無法呼叫 DllUnregisterServer；直接清掉這個 CLSID 的兩個視圖與 TSF profile。
function Remove-RegistryKeys {
    foreach ($key in @(
        "Registry::HKEY_CLASSES_ROOT\CLSID\$Clsid",
        "Registry::HKEY_CLASSES_ROOT\WOW6432Node\CLSID\$Clsid",
        "HKLM:\SOFTWARE\Microsoft\CTF\TIP\$Clsid",
        "HKCU:\Software\Microsoft\CTF\TIP\$Clsid"
    )) {
        if (Test-Path -LiteralPath $key) {
            Remove-Item -LiteralPath $key -Recurse -Force
        }
    }
}

Assert-Administrator
Initialize-LanguageList
Remove-Tip $MixedTip
Remove-Tip $LegacyJaTip

if (Test-Path -LiteralPath $FixedDll -PathType Leaf) {
    Unregister-Dll $FixedDll
} else {
    Write-Warning "固定路徑沒有 $FixedDll；直接移除登錄機碼。"
    Remove-RegistryKeys
}

# 記號只剩其他項目時保留；沒有項目就刪掉檔案。
if (Test-Path -LiteralPath $Marker -PathType Leaf) {
    $remaining = @(Get-Content -LiteralPath $Marker | Where-Object { $_ -and $_ -ne $MixedTip -and $_ -ne $LegacyJaTip })
    if ($remaining.Count -gt 0) {
        Set-Content -LiteralPath $Marker -Value (($remaining -join "`n") + "`n") -NoNewline -Encoding ascii
    } else {
        Remove-Item -LiteralPath $Marker -Force
    }
}

# 宿主可能還載著 DLL；刪不掉時保留檔案並提示，註冊已經移除。
if (Test-Path -LiteralPath $FixedDll) {
    Remove-Item -LiteralPath $FixedDll -Force -ErrorAction SilentlyContinue
}
if (Test-Path -LiteralPath "$FixedDll.new") {
    Remove-Item -LiteralPath "$FixedDll.new" -Force -ErrorAction SilentlyContinue
}
if (Test-Path -LiteralPath $FixedDll) {
    Write-Warning "仍有程式載入 $FixedDll，檔案留著；登出或關閉那些程式後可自行刪除。"
} elseif ((Test-Path -LiteralPath $FixedDir) -and -not (Get-ChildItem -LiteralPath $FixedDir -Force)) {
    Remove-Item -LiteralPath $FixedDir -Force
}

Write-Host '已移除註冊：CLSID、Mixed profile、語言清單與固定路徑 DLL。'
