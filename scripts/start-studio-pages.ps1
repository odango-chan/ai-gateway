# Start the local Gateway for GitHub Pages Studio without storing provider keys.
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$config = Join-Path $root "configs/config.studio-pages.example.yaml"
if (-not (Test-Path -LiteralPath $config)) {
    throw "Missing bundled Studio example config: $config"
}
$originalKey = $env:OPENCODE_GO_API_KEY
try {
    if ([string]::IsNullOrWhiteSpace($env:OPENCODE_GO_API_KEY)) {
        $secureKey = Read-Host "OpenCode Go API key (not saved)" -AsSecureString
        $pointer = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($secureKey)
        try {
            $env:OPENCODE_GO_API_KEY = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($pointer)
        } finally {
            [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($pointer)
        }
        if ([string]::IsNullOrWhiteSpace($env:OPENCODE_GO_API_KEY)) {
            throw "A non-empty OpenCode Go API key is required."
        }
    }
    Write-Host "Starting local Gateway at http://127.0.0.1:8787"
    Write-Host "Allowed Studio origin: https://odango-chan.github.io"
    Write-Host "Press Ctrl+C to stop."
    $binary = Join-Path $root "ai-gateway.exe"
    if (Test-Path -LiteralPath $binary) {
        & $binary -config $config
        if ($LASTEXITCODE -ne 0) { throw "Gateway exited with code $LASTEXITCODE" }
    } elseif ((Get-Command go -ErrorAction SilentlyContinue) -and (Test-Path (Join-Path $root "go.mod"))) {
        Push-Location $root
        try {
            & go run ./cmd/ai-gateway -config $config
            if ($LASTEXITCODE -ne 0) { throw "Gateway exited with code $LASTEXITCODE" }
        } finally {
            Pop-Location
        }
    } else {
        throw "No Windows Gateway binary found. Use the matching Windows release asset or install Go."
    }
} finally {
    # Restore the previous session environment after the child process exits.
    if ($null -eq $originalKey) {
        Remove-Item Env:OPENCODE_GO_API_KEY -ErrorAction SilentlyContinue
    } else {
        $env:OPENCODE_GO_API_KEY = $originalKey
    }
}
