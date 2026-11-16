# T28：驗證 Windows 語言列同時有 zh-TW 與 ja-JP 兩個靈犀 profile。
# 需在 Windows 10/11 x64 執行；只讀取註冊與 0c-update status -json，不安裝、不修改系統。
# 用法：
#   pwsh -File verify-profiles.ps1
#   pwsh -File verify-profiles.ps1 -Updater "$env:LOCALAPPDATA\Zuin\bin\0c-update.exe" -Out build/windows/validation/t28-profiles.json
# 結束碼 0 代表兩個 profile 都已註冊；缺少任何一個回傳 1。
[CmdletBinding()]
param(
    [string]$Updater,
    [string]$Out
)
$ErrorActionPreference = "Stop"
$clsid = "{138C35D0-7F08-4CEF-BA90-C34C16301528}"
$root = "HKLM:\SOFTWARE\Microsoft\CTF\TIP\$clsid\LanguageProfile"
$profiles = @(
    [pscustomobject]@{ Name = "zh-TW"; Language = "zh"; LanguageId = "0x0404"; Profile = "{EF62F743-9855-4C94-A658-1F0F98B7B840}" },
    [pscustomobject]@{ Name = "ja-JP"; Language = "ja"; LanguageId = "0x0411"; Profile = "{3D6A8B21-5C47-4F9E-B0D2-71E4A9C8F653}" }
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
        registered  = [bool]$key
        registry    = $key
    }
}

$report = [ordered]@{
    schema   = 1
    test     = "T28 Windows language profiles"
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

$missing = @($rows | Where-Object { -not $_.registered })
if ($missing.Count -gt 0) {
    Write-Error ("缺少語言 profile：" + (($missing | ForEach-Object { $_.name }) -join "、")) -ErrorAction Continue
    exit 1
}
exit 0
