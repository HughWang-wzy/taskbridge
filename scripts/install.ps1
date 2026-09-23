param(
    [string]$ReleaseBase = 'https://github.com/HughWang-wzy/taskbridge/releases/latest/download',
    [string]$InstallDir = "$env:LOCALAPPDATA\Programs\TaskBridge",
    [string]$WorkerUrl = $env:TB_WORKER_URL,
    [string]$InstallMode = $env:TB_INSTALL_MODE,
    [string]$FirstRunScriptUri = 'https://github.com/HughWang-wzy/taskbridge/releases/latest/download/first-run.ps1',
    [switch]$EnableCodex,
    [switch]$DisableRelay,
    [switch]$Reconfigure
)

$ErrorActionPreference = 'Stop'
if (-not [Environment]::Is64BitOperatingSystem) {
    throw 'TaskBridge currently provides a Windows x86-64 binary only.'
}

$configDir = Join-Path $env:APPDATA 'taskbridge'
$configFile = Join-Path $configDir 'config.json'
if (-not $InstallMode) {
    if ($WorkerUrl -or $env:TB_CLIENT_TOKEN) {
        $InstallMode = 'client'
    } else {
        $defaultChoice = if (Test-Path $configFile) { '2' } else { '1' }
        Write-Host 'TaskBridge installation:'
        Write-Host '  1) Create a new Cloudflare Worker and D1 database'
        Write-Host '  2) Connect this computer to an existing Worker'
        $InstallMode = Read-Host "Choose 1 or 2 [$defaultChoice]"
        if (-not $InstallMode) { $InstallMode = $defaultChoice }
    }
}
if ($InstallMode -in @('1', 'deploy')) {
    $bootstrapPath = Join-Path ([IO.Path]::GetTempPath()) ('taskbridge-first-run-' + [Guid]::NewGuid().ToString('N') + '.ps1')
    try {
        if (Test-Path $FirstRunScriptUri) {
            Copy-Item $FirstRunScriptUri $bootstrapPath
        } else {
            Invoke-WebRequest -UseBasicParsing -Uri $FirstRunScriptUri -OutFile $bootstrapPath
        }
        & powershell.exe -NoProfile -ExecutionPolicy Bypass -File $bootstrapPath
        if ($LASTEXITCODE -ne 0) { throw 'First deployment failed; see the error above and retry.' }
    } finally {
        Remove-Item -Force $bootstrapPath -ErrorAction SilentlyContinue
    }
    return
}
if ($InstallMode -notin @('2', 'client')) { throw 'Choose 1 (new deployment) or 2 (existing Worker).' }

$archive = 'taskbridge-windows-amd64.zip'
$workDir = Join-Path ([IO.Path]::GetTempPath()) ('taskbridge-' + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $workDir | Out-Null
try {
    $base = $ReleaseBase.TrimEnd('/')
    $archivePath = Join-Path $workDir $archive
    $checksumsPath = Join-Path $workDir 'SHA256SUMS'
    Invoke-WebRequest -UseBasicParsing -Uri "$base/$archive" -OutFile $archivePath
    Invoke-WebRequest -UseBasicParsing -Uri "$base/SHA256SUMS" -OutFile $checksumsPath

    $expected = $null
    foreach ($line in Get-Content $checksumsPath) {
        $parts = $line.Trim() -split '\s+'
        if ($parts.Length -ge 2 -and $parts[1] -eq "./$archive") {
            $expected = $parts[0].ToLowerInvariant()
            break
        }
    }
    if (-not $expected) { throw "Release checksum is missing for $archive" }
    $actual = (Get-FileHash -Path $archivePath -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($actual -ne $expected) { throw 'Release checksum mismatch' }

    Expand-Archive -Path $archivePath -DestinationPath $workDir
    New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
    $tbExe = Join-Path $InstallDir 'tb.exe'
    Copy-Item -Force (Join-Path $workDir 'taskbridge-windows-amd64\tb.exe') $tbExe
    Write-Host "Installed $tbExe"

    if (-not (Test-Path $configFile) -or $Reconfigure) {
        if (-not $WorkerUrl) { $WorkerUrl = Read-Host 'Worker URL' }
        $topic = if ($env:TB_NTFY_TOPIC) { $env:TB_NTFY_TOPIC } else { Read-Host 'ntfy topic' }
        if ($env:TB_CLIENT_TOKEN) {
            $token = $env:TB_CLIENT_TOKEN
        } else {
            $secure = Read-Host 'Device client token' -AsSecureString
            $pointer = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($secure)
            try { $token = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($pointer) }
            finally { [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($pointer) }
        }
        $env:TB_TOKEN = $token
        $env:TB_NTFY_TOPIC = $topic
        try {
            & $tbExe init --url $WorkerUrl
            if ($LASTEXITCODE -ne 0) { throw 'tb init failed' }
        } finally {
            Remove-Item Env:TB_TOKEN -ErrorAction SilentlyContinue
            Remove-Item Env:TB_NTFY_TOPIC -ErrorAction SilentlyContinue
            $token = $null
        }
    } else {
        Write-Host "Keeping existing $configFile (use -Reconfigure to replace it)."
    }

    & $tbExe doctor
    if ($LASTEXITCODE -ne 0) { throw 'tb doctor failed; check Worker URL and device token' }

    $codexChoice = if ($EnableCodex) { 'y' } else { Read-Host 'Install Codex Hooks and MCP? [y/N]' }
    if ($codexChoice -match '^(y|yes)$') {
        if (-not (Get-Command codex -ErrorAction SilentlyContinue)) {
            Write-Warning 'Codex CLI not found; skipping Codex integration.'
        } else {
            $codexConfig = Join-Path $configDir 'codex.json'
            if (-not (Test-Path $codexConfig) -or $Reconfigure) { Copy-Item -Force $configFile $codexConfig }
            $customize = if ($env:TB_CUSTOMIZE_CODEX) { $env:TB_CUSTOMIZE_CODEX } else { Read-Host 'Customize Codex completion notifications? [y/N]' }
            if ($customize -match '^(y|yes|1)$') {
                $hookTopic = if ($env:TB_CODEX_TOPIC) { $env:TB_CODEX_TOPIC } else { Read-Host 'Topic name (e.g. Training)' }
                $hookTitle = if ($env:TB_CODEX_TITLE) { $env:TB_CODEX_TITLE } else { Read-Host 'Title template [{topic} finished]' }
                $hookBody = if ($env:TB_CODEX_BODY) { $env:TB_CODEX_BODY } else { Read-Host 'Body template [{topic} task finished; Duration: {duration}]' }
                $hookOutput = if ($env:TB_CODEX_FINAL_OUTPUT) { $env:TB_CODEX_FINAL_OUTPUT } else { Read-Host 'Include final Codex answer on phone? [y/N]' }
                $outputMode = if ($hookOutput -match '^(y|yes|1|on)$') { 'on' } else { 'off' }
                & $tbExe hook codex --topic $hookTopic --title $hookTitle --body $hookBody --final-output $outputMode
            } else {
                & $tbExe hook codex
            }
            if ($LASTEXITCODE -ne 0) { throw 'Codex Hook installation failed' }
            & codex mcp get taskbridge *> $null
            if ($LASTEXITCODE -eq 0) {
                Write-Host 'TaskBridge MCP already exists; inspect its binary path with: codex mcp get taskbridge'
            } else {
                & codex mcp add taskbridge -- $tbExe mcp
                if ($LASTEXITCODE -ne 0) { throw 'Codex MCP registration failed' }
            }
            Write-Host 'Restart Codex and review the new Hooks in /hooks.'
        }
    }

    $relayChoice = if ($DisableRelay) { 'n' } else { Read-Host 'Start a background relay on this computer? [Y/n]' }
    if ($relayChoice -notmatch '^(n|no)$') {
        $startup = [Environment]::GetFolderPath('Startup')
        $shortcut = (New-Object -ComObject WScript.Shell).CreateShortcut((Join-Path $startup 'TaskBridge Relay.lnk'))
        $shortcut.TargetPath = $tbExe
        $shortcut.Arguments = 'relay --interval=20s'
        $shortcut.WindowStyle = 7
        $shortcut.Save()
        $running = Get-CimInstance Win32_Process -Filter "Name='tb.exe'" |
            Where-Object { $_.ExecutablePath -eq $tbExe -and $_.CommandLine -match '\brelay\b' }
        if (-not $running) {
            Start-Process -FilePath $tbExe -ArgumentList 'relay', '--interval=20s' -WindowStyle Hidden
        }
        Write-Host 'Relay started and added to the user Startup folder.'
    }

    Write-Host "TaskBridge setup complete. Test with: & '$tbExe' notify --title Test 'New computer connected'"
} finally {
    Remove-Item -Recurse -Force $workDir -ErrorAction SilentlyContinue
}
