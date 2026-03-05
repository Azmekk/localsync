$ErrorActionPreference = "Stop"

$repo = "Azmekk/localsync"
$installDir = "$env:LOCALAPPDATA\localsync"

Write-Host "Fetching latest release..."
$release = Invoke-RestMethod -Uri "https://api.github.com/repos/$repo/releases/latest"

$localsyncAsset = $release.assets | Where-Object { $_.name -eq "localsync-windows-amd64.exe" }
$syncclientAsset = $release.assets | Where-Object { $_.name -eq "syncclient-windows-amd64.exe" }

if (-not $localsyncAsset -or -not $syncclientAsset) {
    Write-Error "Could not find Windows release assets"
    exit 1
}

# Create install directory
if (-not (Test-Path $installDir)) {
    New-Item -ItemType Directory -Path $installDir -Force | Out-Null
}

# Download binaries
Write-Host "Downloading localsync..."
Invoke-WebRequest -Uri $localsyncAsset.browser_download_url -OutFile "$installDir\localsync.exe"

Write-Host "Downloading syncclient..."
Invoke-WebRequest -Uri $syncclientAsset.browser_download_url -OutFile "$installDir\syncclient.exe"

# Add to user PATH if not already present
$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($userPath -notlike "*$installDir*") {
    [Environment]::SetEnvironmentVariable("Path", "$userPath;$installDir", "User")
    $env:Path = "$env:Path;$installDir"
    Write-Host "Added $installDir to user PATH"
}

Write-Host ""
Write-Host "Installed localsync and syncclient to $installDir"

# Check for optional dependencies
if (-not (Get-Command mpv -ErrorAction SilentlyContinue)) {
    Write-Host "Warning: mpv not found — install it before running localsync"
}

if (-not (Get-Command ffmpeg -ErrorAction SilentlyContinue)) {
    Write-Host "Warning: ffmpeg not found — install it before running localsync"
}

Write-Host "Done!"
