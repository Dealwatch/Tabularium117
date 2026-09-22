# Build tabularium117.exe on Windows. Equivalent to `make build`.
$ErrorActionPreference = "Stop"

$version = "dev"
try { $version = (git describe --tags --always --dirty 2>$null); if (-not $version) { $version = "dev" } } catch {}

$env:CGO_ENABLED = "0"
$env:GOOS = "windows"
$env:GOARCH = "amd64"

New-Item -ItemType Directory -Force -Path dist | Out-Null
go build -trimpath -ldflags "-s -w -X main.version=$version" -o dist/tabularium117.exe ./cmd/tabularium117
Write-Host "built dist/tabularium117.exe ($version)"
