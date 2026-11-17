# Run a script in the current user's interactive session as an elevated task.
# Used by p2vm.py from an SSH session: sshd itself is not an interactive desktop.
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$ScriptPath,
    [string]$ScriptArguments = '',
    [string]$TaskName = '0c-P2VM-Task',
    [int]$TimeoutMinutes = 60,
    [string]$LogPath
)
$ErrorActionPreference = 'Stop'
$identity = [Security.Principal.WindowsIdentity]::GetCurrent()
if (-not $LogPath) { $LogPath = Join-Path $env:TEMP ($TaskName + '.json') }
New-Item -ItemType Directory -Force -Path (Split-Path -Parent $LogPath) | Out-Null

function Write-Meta([hashtable]$values) {
    $values['when'] = (Get-Date).ToString('o')
    $values | ConvertTo-Json -Depth 6 | Set-Content -LiteralPath $LogPath -Encoding UTF8
}

$task = $null
$code = 1
$started = Get-Date
try {
    if (Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue) {
        Stop-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
        Unregister-ScheduledTask -TaskName $TaskName -Confirm:$false -ErrorAction SilentlyContinue
    }
    $powershell = Join-Path $env:WINDIR 'System32\WindowsPowerShell\v1.0\powershell.exe'
    if (-not (Test-Path -LiteralPath $powershell)) { throw "Windows PowerShell not found: $powershell" }
    $arguments = '-NoProfile -ExecutionPolicy Bypass -File "' + $ScriptPath + '"'
    if ($ScriptArguments) { $arguments += ' ' + $ScriptArguments }
    $action = New-ScheduledTaskAction -Execute $powershell -Argument $arguments
    $principal = New-ScheduledTaskPrincipal -UserId $identity.Name -LogonType Interactive -RunLevel Highest
    $settings = New-ScheduledTaskSettingsSet -ExecutionTimeLimit (New-TimeSpan -Minutes $TimeoutMinutes) `
        -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -StartWhenAvailable
    Register-ScheduledTask -TaskName $TaskName -Action $action -Principal $principal -Settings $settings -Force | Out-Null
    Start-ScheduledTask -TaskName $TaskName
    $deadline = (Get-Date).AddMinutes($TimeoutMinutes)
    do {
        Start-Sleep -Milliseconds 500
        $info = Get-ScheduledTaskInfo -TaskName $TaskName
        $state = (Get-ScheduledTask -TaskName $TaskName).State
        if ($state -ne 'Running' -and $info.LastRunTime -and $info.LastRunTime.Year -gt 2000) { break }
    } while ((Get-Date) -lt $deadline)
    $info = Get-ScheduledTaskInfo -TaskName $TaskName
    if ($state -eq 'Running') {
        Stop-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
        $code = 2
        Write-Meta @{ ok = $false; error = "interactive task timed out after $TimeoutMinutes minutes"; task_result = 2 }
    } else {
        $code = [int64]$info.LastTaskResult
        Write-Meta @{
            ok = ($code -eq 0)
            task = $TaskName
            script = $ScriptPath
            arguments = $ScriptArguments
            task_result = $code
            started = $started.ToString('o')
            seconds = [int]((Get-Date) - $started).TotalSeconds
        }
    }
} catch {
    Write-Meta @{ ok = $false; error = $_.Exception.ToString(); script = $ScriptPath; task = $TaskName }
    $code = 1
} finally {
    if (Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue) {
        Unregister-ScheduledTask -TaskName $TaskName -Confirm:$false -ErrorAction SilentlyContinue
    }
}
# LastTaskResult is an HRESULT-style unsigned value; treat only zero as success.
if ($code -eq 0) { exit 0 }
exit 1
