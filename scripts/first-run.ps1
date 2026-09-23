param([string]$SetupDir = (Join-Path $HOME 'taskbridge'))

$ErrorActionPreference = 'Stop'
foreach ($program in @('git', 'node', 'npm')) {
    if (-not (Get-Command $program -ErrorAction SilentlyContinue)) {
        throw "Missing $program (Node.js 22+ and Git are required)"
    }
}

if (-not (Test-Path $SetupDir)) {
    & git clone --branch v0.3.0 --depth 1 https://github.com/HughWang-wzy/taskbridge.git $SetupDir
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
