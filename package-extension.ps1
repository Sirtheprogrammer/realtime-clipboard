# Package extensions for Chrome Web Store and Firefox AMO
$ErrorActionPreference = "Stop"

$root = $PSScriptRoot
if (-not $root) { $root = Get-Location }

$extDir = Join-Path $root "extension"
$distDir = Join-Path $root "dist"

if (-not (Test-Path $distDir)) {
    New-Item -ItemType Directory -Path $distDir | Out-Null
}

Write-Host "Reading manifest..." -ForegroundColor Cyan
$manifestContent = Get-Content (Join-Path $extDir "manifest.json") -Raw
$manifest = $manifestContent | ConvertFrom-Json
$version = $manifest.version
Write-Host "Extension Version: $version" -ForegroundColor Green

$utf8NoBom = New-Object System.Text.UTF8Encoding $false

# 1. Firefox AMO Package (removes service_worker, keeps scripts: ["background.js"])
Write-Host "Creating Firefox AMO package..." -ForegroundColor Cyan
$ffTemp = Join-Path ([System.IO.Path]::GetTempPath()) "clipboard-firefox-build-$([System.Guid]::NewGuid().ToString('N'))"
New-Item -ItemType Directory -Path $ffTemp | Out-Null

Copy-Item -Path "$extDir\*" -Destination $ffTemp -Recurse -Exclude "*.go","*_test.go"

$ffManifest = $manifestContent | ConvertFrom-Json
$ffManifest.background.PSObject.Properties.Remove("service_worker")
$ffManifestJson = $ffManifest | ConvertTo-Json -Depth 10
[System.IO.File]::WriteAllText((Join-Path $ffTemp "manifest.json"), $ffManifestJson, $utf8NoBom)

$ffZip = Join-Path $distDir "clipboard-vault-firefox.zip"
$ffXpi = Join-Path $distDir "clipboard-vault-firefox.xpi"
if (Test-Path $ffZip) { Remove-Item $ffZip }
if (Test-Path $ffXpi) { Remove-Item $ffXpi }

Compress-Archive -Path "$ffTemp\*" -DestinationPath $ffZip -Force
Copy-Item -Path $ffZip -Destination $ffXpi -Force
Remove-Item -Path $ffTemp -Recurse -Force
Write-Host "Created: $ffZip and $ffXpi" -ForegroundColor Green

# 2. Chrome Web Store Package (removes scripts: ["background.js"], keeps service_worker)
Write-Host "Creating Chrome Web Store package..." -ForegroundColor Cyan
$crTemp = Join-Path ([System.IO.Path]::GetTempPath()) "clipboard-chrome-build-$([System.Guid]::NewGuid().ToString('N'))"
New-Item -ItemType Directory -Path $crTemp | Out-Null

Copy-Item -Path "$extDir\*" -Destination $crTemp -Recurse -Exclude "*.go","*_test.go"

$crManifest = $manifestContent | ConvertFrom-Json
$crManifest.background.PSObject.Properties.Remove("scripts")
$crManifestJson = $crManifest | ConvertTo-Json -Depth 10
[System.IO.File]::WriteAllText((Join-Path $crTemp "manifest.json"), $crManifestJson, $utf8NoBom)

$crZip = Join-Path $distDir "clipboard-vault-chrome.zip"
if (Test-Path $crZip) { Remove-Item $crZip }

Compress-Archive -Path "$crTemp\*" -DestinationPath $crZip -Force
Remove-Item -Path $crTemp -Recurse -Force
Write-Host "Created: $crZip" -ForegroundColor Green

Write-Host "`nAll extension packages built successfully in ./dist/!" -ForegroundColor Cyan
