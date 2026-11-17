# P2-40／P2-43：驗證 Windows 語言列只有一個 Mixed profile。
# zh-TW 的 Mixed profile 必須存在；舊版（T28）的 ja-JP「靈犀日本語」必須不存在。
# 需在 Windows 10/11 x64 執行；只讀取註冊與 0c-update status -json，不安裝、不修改系統。
# 用法：
#   pwsh -File verify-profiles.ps1
#   pwsh -File verify-profiles.ps1 -Updater "$env:LOCALAPPDATA\Zuin\bin\0c-update.exe" -Out build/windows/validation/p2-43-profiles.json
# 結束碼 0 代表只有 Mixed 已註冊；缺少 Mixed 或仍有 ja-JP 回傳 1。
[CmdletBinding()]
param(
    [string]$Updater,
    [string]$Out
)
$ErrorActionPreference = "Stop"
$clsid = "{138C35D0-7F08-4CEF-BA90-C34C16301528}"
$root = "HKLM:\SOFTWARE\Microsoft\CTF\TIP\$clsid\LanguageProfile"
$profiles = @(
    [pscustomobject]@{ Name = "Mixed"; Language = "zh"; LanguageId = "0x0404"; Profile = "{EF62F743-9855-4C94-A658-1F0F98B7B840}"; Required = $true },
    [pscustomobject]@{ Name = "legacy ja-JP"; Language = "ja"; LanguageId = "0x0411"; Profile = "{3D6A8B21-5C47-4F9E-B0D2-71E4A9C8F653}"; Required = $false }
)

$rows = foreach ($entry in $profiles) {
    # msctf 可能寫 0x0404 或 0x00000404；兩種都接受。
    $paths = @(
        (Join-Path $root "$($entry.LanguageId)\$($entry.Profile)")
        (Join-Path $root ("0x0000" + $entry.LanguageId.Substring(2) + "\" + $entry.Profile))
    )
    $key = $paths | Where-Object { Test-Path -LiteralPath $_ } | Select-Object -First 1
    [pscustomobject]@{
        name        = $entry.Name
        language    = $entry.Language
        language_id = $entry.LanguageId
        profile     = $entry.Profile
        required    = $entry.Required
        registered  = [bool]$key
        registry    = $key
    }
}

$report = [ordered]@{
    schema   = 1
    test     = "P2-43 Windows language profiles: exactly one Mixed"
    when     = (Get-Date).ToString("o")
    computer = $env:COMPUTERNAME
    profiles = $rows
}

if ($Updater) {
    if (-not (Test-Path -LiteralPath $Updater -PathType Leaf)) {
        throw "找不到 0c-update：$Updater"
    }
    $status = & $Updater status -json | ConvertFrom-Json
    $report.status_profiles = $status.profiles
}

$text = $report | ConvertTo-Json -Depth 8
$text
if ($Out) {
    $dir = Split-Path -Parent $Out
    if ($dir) { New-Item -ItemType Directory -Force -Path $dir | Out-Null }
    Set-Content -LiteralPath $Out -Value $text -Encoding utf8
}

$mixed = @($rows | Where-Object { $_.required -and $_.registered })
$legacy = @($rows | Where-Object { -not $_.required -and $_.registered })
if ($mixed.Count -ne 1) {
    Write-Error "缺少 Mixed profile" -ErrorAction Continue
    exit 1
}
if ($legacy.Count -ne 0) {
    Write-Error ("仍註冊舊版 profile：" + (($legacy | ForEach-Object { $_.name }) -join "、")) -ErrorAction Continue
    exit 1
}
exit 0
