param([string]$SetupDir = (Join-Path $HOME 'taskbridge'))

$ErrorActionPreference = 'Stop'
function Test-NodeReady {
    if (-not (Get-Command node -ErrorAction SilentlyContinue) -or -not (Get-Command npm -ErrorAction SilentlyContinue)) { return $false }
    $major = & node -p 'Number(process.versions.node.split(".")[0])'
    if ($LASTEXITCODE -ne 0 -or [int]$major -lt 22) { return $false }
    & npm.cmd --version *> $null
    return ($LASTEXITCODE -eq 0)
}
function Test-GitReady {
    if (-not (Get-Command git -ErrorAction SilentlyContinue)) { return $false }
    & git --version *> $null
    return ($LASTEXITCODE -eq 0)
}

if (-not (Test-GitReady) -or -not (Test-NodeReady)) {
    Write-Host 'TaskBridge needs Git and Node.js 22+ with npm.'
    Write-Host 'Node.js guide: https://nodejs.org/en/download'
    if (Get-Command winget -ErrorAction SilentlyContinue) {
        $repair = Read-Host 'Install or update missing tools with winget? [y/N]'
        if ($repair -match '^(y|yes)$') {
            if (-not (Test-GitReady)) {
                & winget install --id Git.Git -e --accept-package-agreements --accept-source-agreements
            }
            if (-not (Test-NodeReady)) {
                & winget upgrade --id OpenJS.NodeJS.LTS -e --accept-package-agreements --accept-source-agreements
                if ($LASTEXITCODE -ne 0) {
                    & winget install --id OpenJS.NodeJS.LTS -e --accept-package-agreements --accept-source-agreements
                }
            }
        }
    } else {
        Write-Host 'Install Git: https://git-scm.com/download/win'
    }
    if (-not (Test-GitReady) -or -not (Test-NodeReady)) {
        throw 'Install Git and Node.js 22+, open a new PowerShell window, then rerun this script.'
    }
}

if (-not (Test-Path $SetupDir)) {
    & git clone --branch v0.4.0 --depth 1 https://github.com/HughWang-wzy/taskbridge.git $SetupDir
    if ($LASTEXITCODE -ne 0) { throw 'Could not clone TaskBridge' }
} elseif (-not (Test-Path (Join-Path $SetupDir 'scripts/setup.mjs'))) {
    throw "$SetupDir exists and is not a TaskBridge checkout; choose a new -SetupDir"
}

Push-Location $SetupDir
try {
    & npm.cmd ci
    if ($LASTEXITCODE -ne 0) { throw 'npm ci failed' }
    & node scripts/setup.mjs
    if ($LASTEXITCODE -ne 0) { throw 'TaskBridge setup failed' }
} finally {
    Pop-Location
}
