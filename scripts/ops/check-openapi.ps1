$ErrorActionPreference = "Stop"

$spec = Get-Content -Raw -Path "docs/openapi.json" | ConvertFrom-Json
if (-not $spec.openapi -or -not $spec.paths) {
  throw "openapi.json missing required top-level fields"
}

$requiredPaths = @(
  "/health",
  "/ready",
  "/metrics",
  "/v1/responses",
  "/v1/chat/completions",
  "/v1/messages",
  "/v1/models"
)

$specPaths = @($spec.paths.PSObject.Properties.Name)
$missing = @()
foreach ($path in $requiredPaths) {
  if ($specPaths -notcontains $path) {
    $missing += $path
  }
}

if ($missing.Count -gt 0) {
  throw "openapi missing required paths: $($missing -join ', ')"
}

Write-Output ("OPENAPI_OK {0}" -f $specPaths.Count)
