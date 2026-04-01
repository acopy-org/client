$ErrorActionPreference = "Stop"

$base = "https://github.com/acopy-org/client/releases/latest/download"
$arch = if ($env:PROCESSOR_ARCHITECTURE -eq "ARM64") { "arm64" } else { "amd64" }
$url = "$base/dl/windows-$arch/acopy.exe"
$dir = "$env:LOCALAPPDATA\acopy"
$bin = "$dir\acopy.exe"

New-Item -ItemType Directory -Force -Path $dir | Out-Null

Write-Host "downloading acopy for windows/$arch..."
Invoke-WebRequest -Uri $url -OutFile $bin -UseBasicParsing

# Add to PATH if not already there
$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($userPath -notlike "*$dir*") {
    [Environment]::SetEnvironmentVariable("Path", "$userPath;$dir", "User")
    $env:Path = "$env:Path;$dir"
}

Write-Host "installed. run: acopy setup"
