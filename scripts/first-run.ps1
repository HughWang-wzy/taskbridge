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

$releaseTag = 'v0.4.7'
if (-not (Test-Path $SetupDir)) {
    & git clone --branch $releaseTag --depth 1 https://github.com/HughWang-wzy/taskbridge.git $SetupDir
    if ($LASTEXITCODE -ne 0) { throw 'Could not clone TaskBridge' }
} elseif (-not (Test-Path (Join-Path $SetupDir 'scripts/setup.mjs'))) {
    throw "$SetupDir exists and is not a TaskBridge checkout; choose a new -SetupDir"
} else {
    if (-not (Test-Path (Join-Path $SetupDir '.git'))) { throw "$SetupDir is not a Git checkout; cannot update it safely" }
    $edits = & git -C $SetupDir status --porcelain --untracked-files=no
    if ($LASTEXITCODE -ne 0) { throw 'Could not inspect the existing checkout' }
    if ($edits) { throw "$SetupDir has local source edits; update it manually" }
    & git -C $SetupDir fetch --depth 1 origin "refs/tags/${releaseTag}:refs/tags/${releaseTag}"
    if ($LASTEXITCODE -ne 0) { throw 'Could not fetch the TaskBridge release tag' }
    & git -C $SetupDir checkout --detach -q $releaseTag
    if ($LASTEXITCODE -ne 0) { throw 'Could not update the TaskBridge checkout' }
}

Push-Location $SetupDir
try {
    & npm.cmd ci
    if ($LASTEXITCODE -ne 0) { throw 'npm ci failed' }
    $proxyBypass = if ($env:no_proxy) { $env:no_proxy } else { $env:NO_PROXY }
    $env:NO_PROXY = if ($proxyBypass) { "$proxyBypass,localhost,127.0.0.1,::1" } else { 'localhost,127.0.0.1,::1' }
    $env:no_proxy = $env:NO_PROXY
    $env:NODE_USE_ENV_PROXY = '1'
    $env:NODE_USE_SYSTEM_CA = '1'
    & node scripts/setup.mjs
    if ($LASTEXITCODE -ne 0) { throw 'TaskBridge setup failed' }
} finally {
    Pop-Location
}
