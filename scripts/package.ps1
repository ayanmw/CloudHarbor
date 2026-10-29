param(
    [Parameter(Mandatory = $true)][string]$Version,
    [string]$OutputRoot = '',
    [switch]$AllowDirty
)
$ErrorActionPreference = 'Stop'
if ($Version -notmatch '^v[0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9.-]+)?$') { throw 'Invalid version.' }
$repoRoot = Split-Path $PSScriptRoot -Parent
if (!$OutputRoot) { $OutputRoot = Join-Path (Split-Path $repoRoot -Parent) 'nas_baidupan_auto_downloader-releases' }
$OutputRoot = [IO.Path]::GetFullPath($OutputRoot)
$releasePath = Join-Path $OutputRoot $Version
$archivePath = "$releasePath.zip"
if ((Test-Path -LiteralPath $releasePath) -or (Test-Path -LiteralPath $archivePath)) { throw 'Version already exists. Use a new version; artifacts are immutable.' }
$originalLocation = Get-Location
$savedGoos = $env:GOOS
$savedGoarch = $env:GOARCH
$savedCgo = $env:CGO_ENABLED
try {
    Set-Location $repoRoot
    $baseRevision = (git rev-parse HEAD).Trim()
    if ($LASTEXITCODE -ne 0) { throw 'Cannot read source revision.' }
    $status = @(git status --porcelain)
    if ($status.Count -gt 0 -and !$AllowDirty) { throw 'Working tree is dirty. Pass -AllowDirty to package a content-addressed source snapshot.' }
    $sourceRevision = $baseRevision
    $snapshotText = ''
    $sourceManifest = @("Base revision: $baseRevision")
    if ($status.Count -gt 0) {
        $sourceManifest = @()
        $sourceFiles = @(git -c core.quotepath=false ls-files --cached --others --exclude-standard)
        if ($LASTEXITCODE -ne 0 -or $sourceFiles.Count -eq 0) { throw 'Cannot enumerate source snapshot files.' }
        foreach ($relative in ($sourceFiles | Sort-Object -Unique)) {
            if (!(Test-Path -LiteralPath $relative -PathType Leaf)) { continue }
            $hash = (Get-FileHash -LiteralPath $relative -Algorithm SHA256).Hash.ToLowerInvariant()
            $sourceManifest += "$hash  $($relative.Replace('\', '/'))"
        }
        $manifestText = [string]::Join("`n", $sourceManifest) + "`n"
        $manifestBytes = [Text.Encoding]::UTF8.GetBytes($manifestText)
        $sha256 = [Security.Cryptography.SHA256]::Create()
        try { $treeHash = ([BitConverter]::ToString($sha256.ComputeHash($manifestBytes))).Replace('-', '').ToLowerInvariant() }
        finally { $sha256.Dispose() }
        $sourceRevision = "$baseRevision-dirty-$($treeHash.Substring(0, 16))"
        $snapshotText = "Base revision: $baseRevision`nSnapshot SHA256: $treeHash`n`n$manifestText"
    }
    # Run each node:test file as its own process; the multi-file runner in recent Node versions spawns workers that are denied in the Windows package environment.
    node scripts/webui.test.cjs
    if ($LASTEXITCODE -ne 0) { throw 'Web UI tests failed.' }
    node scripts/userscript.test.cjs
    if ($LASTEXITCODE -ne 0) { throw 'Browser script tests failed.' }
    $env:GOOS = 'windows'; $env:GOARCH = 'amd64'; $env:CGO_ENABLED = '0'
    go test ./...
    if ($LASTEXITCODE -ne 0) { throw 'Tests failed.' }
    go vet ./...
    if ($LASTEXITCODE -ne 0) { throw 'Vet failed.' }
    New-Item -ItemType Directory -Path $releasePath -Force | Out-Null
    if ($snapshotText) { [IO.File]::WriteAllText((Join-Path $releasePath 'SOURCE-SNAPSHOT.txt'), $snapshotText, [Text.UTF8Encoding]::new($false)) }
    foreach ($target in @('windows-amd64', 'linux-amd64', 'linux-arm64')) {
        $parts = $target.Split('-')
        $env:GOOS = $parts[0]; $env:GOARCH = $parts[1]
        $targetDir = Join-Path $releasePath "bin/$target"
        New-Item -ItemType Directory -Path $targetDir -Force | Out-Null
        $binaryName = 'baidu-downloader'
        if ($parts[0] -eq 'windows') { $binaryName += '.exe' }
        go build -trimpath -ldflags "-s -w -X main.version=$Version -X main.revision=$sourceRevision" -o (Join-Path $targetDir $binaryName) .
        if ($LASTEXITCODE -ne 0) { throw "Build failed: $target" }
    }
    foreach ($name in @('Start.ps1', 'start.sh', 'Dockerfile', 'compose.yml', 'preview.env.example', 'TESTING.md')) {
        Copy-Item -LiteralPath (Join-Path $repoRoot "release/$name") -Destination $releasePath
    }
    Copy-Item -LiteralPath (Join-Path $repoRoot 'tampermonkey_baidupan_nas_queue.user.js') -Destination $releasePath
    $buildInfo = "Version: $Version`nRevision: $sourceRevision`nBase revision: $baseRevision`nDirty snapshot: $($status.Count -gt 0)`nToolchain: $(go version)`n"
    [IO.File]::WriteAllText((Join-Path $releasePath 'BUILD-INFO.txt'), $buildInfo, [Text.UTF8Encoding]::new($false))
    $checksums = Get-ChildItem -LiteralPath $releasePath -File -Recurse | Sort-Object FullName | ForEach-Object {
        $relative = $_.FullName.Substring($releasePath.Length + 1).Replace('\', '/')
        "$( (Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant() )  $relative"
    }
    [IO.File]::WriteAllLines((Join-Path $releasePath 'SHA256SUMS.txt'), $checksums, [Text.UTF8Encoding]::new($false))
    Compress-Archive -Path (Join-Path $releasePath '*') -DestinationPath $archivePath
    Get-FileHash -LiteralPath $archivePath -Algorithm SHA256 | Format-List
    Write-Output "Release directory: $releasePath"
} finally {
    $env:GOOS = $savedGoos; $env:GOARCH = $savedGoarch; $env:CGO_ENABLED = $savedCgo
    Set-Location $originalLocation
}
