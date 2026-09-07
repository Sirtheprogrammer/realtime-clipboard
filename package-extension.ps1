# Package extensions for Chrome Web Store and Firefox AMO
$ErrorActionPreference = "Stop"

$root = $PSScriptRoot
if (-not $root) { $root = Get-Location }

$python = (Get-Command python -ErrorAction SilentlyContinue).Source
if ($python) {
    & $python (Join-Path $root "package_extension.py")
    exit $LASTEXITCODE
}

# Fallback to tar.exe if python is not found
$tar = (Get-Command tar.exe -ErrorAction SilentlyContinue).Source
if ($tar) {
    $distDir = Join-Path $root "dist"
    if (-not (Test-Path $distDir)) { New-Item -ItemType Directory -Path $distDir | Out-Null }
    $extDir = Join-Path $root "extension"
    $ffZip = Join-Path $distDir "clipboard-vault-firefox.zip"
    $ffXpi = Join-Path $distDir "clipboard-vault-firefox.xpi"
    
    # Run tar with forward slashes
    Set-Location $extDir
    & tar.exe -a -c -f $ffZip --exclude="*.go" --exclude="*_test.go" *
    Copy-Item -Path $ffZip -Destination $ffXpi -Force
    Set-Location $root
    Write-Host "Created $ffZip and $ffXpi using tar.exe" -ForegroundColor Green
    exit 0
}

Write-Error "Neither python nor tar.exe found. Please install Python or use tar.exe."
