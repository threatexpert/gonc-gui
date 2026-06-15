param(
    [string]$GonetcatDir = "..\gonetcat",
    [string]$Platform = "windows-amd64"
)

$ErrorActionPreference = "Stop"

$repo = Resolve-Path $GonetcatDir
$sourceCandidates = @(
    (Join-Path $repo "bin\gonc.exe"),
    (Join-Path $repo "gonc.exe")
)

$source = $sourceCandidates | Where-Object { Test-Path $_ } | Select-Object -First 1
if (-not $source) {
    throw "Cannot find gonc.exe. Build gonetcat first, then rerun this script."
}

$targetDir = Join-Path $PSScriptRoot "..\bundled\gonc\$Platform"
New-Item -ItemType Directory -Force -Path $targetDir | Out-Null
Copy-Item -Force $source (Join-Path $targetDir "gonc.exe")
Write-Host "Copied $source to $targetDir"

$buildBin = Join-Path $PSScriptRoot "..\build\bin"
if (Test-Path $buildBin) {
    $buildTargetDir = Join-Path $buildBin "bundled\gonc\$Platform"
    New-Item -ItemType Directory -Force -Path $buildTargetDir | Out-Null
    Copy-Item -Force $source (Join-Path $buildTargetDir "gonc.exe")
    Write-Host "Copied $source to $buildTargetDir"
}
