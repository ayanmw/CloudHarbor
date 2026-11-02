param(
    [Parameter(Mandatory = $true)][string]$Version,
    [string]$OutputRoot = '',
    [switch]$AllowDirty,
    [switch]$UpdatePreview
)
$ErrorActionPreference = 'Stop'
if ($Version -notmatch '^v[0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9.-]+)?$') { throw 'Invalid version.' }
if ($UpdatePreview -and $Version -notmatch '-preview\.[0-9]+$') { throw '-UpdatePreview is only allowed for preview versions.' }
$repoRoot = Split-Path $PSScriptRoot -Parent
if (!$OutputRoot) { $OutputRoot = Join-Path (Split-Path $repoRoot -Parent) 'nas_baidupan_auto_downloader-releases' }
$OutputRoot = [IO.Path]::GetFullPath($OutputRoot)
$finalReleasePath = Join-Path $OutputRoot $Version
$finalArchivePath = "$finalReleasePath.zip"
$artifactExists = (Test-Path -LiteralPath $finalReleasePath) -or (Test-Path -LiteralPath $finalArchivePath)
if ($artifactExists -and !$UpdatePreview) { throw 'Version already exists. Use -UpdatePreview to rebuild this preview, or choose a new version.' }
$stagingRoot = Join-Path $OutputRoot ('.staging-' + $Version + '-' + [guid]::NewGuid().ToString('N'))
$releasePath = Join-Path $stagingRoot $Version
$archivePath = Join-Path $stagingRoot "$Version.zip"
$preserveStaging = $false
$originalLocation = Get-Location
$savedGoos = $env:GOOS
$savedGoarch = $env:GOARCH
$savedCgo = $env:CGO_ENABLED
try {
    New-Item -ItemType Directory -Path $OutputRoot -Force | Out-Null
    New-Item -ItemType Directory -Path $stagingRoot | Out-Null
    Set-Location $repoRoot
    npm --prefix frontend ci --no-fund --no-audit
    if ($LASTEXITCODE -ne 0) { throw 'Frontend dependency install failed.' }
    npm --prefix frontend run build
    if ($LASTEXITCODE -ne 0) { throw 'Frontend build failed.' }
    npm --prefix frontend test
    if ($LASTEXITCODE -ne 0) { throw 'Frontend interaction tests failed.' }
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
    if ($UpdatePreview -and $artifactExists) {
        # Replace only package-managed paths. Runtime data such as .nas-downloader and downloads stays in place.
        $managedPaths = @('bin', 'Start.ps1', 'start.sh', 'Dockerfile', 'compose.yml', 'preview.env.example', 'TESTING.md', 'tampermonkey_baidupan_nas_queue.user.js', 'BUILD-INFO.txt', 'SHA256SUMS.txt', 'SOURCE-SNAPSHOT.txt')
        $backupRoot = Join-Path $stagingRoot 'backup'
        $updatedPaths = New-Object 'System.Collections.Generic.List[string]'
        $archiveBackedUp = $false
        $archiveInstalled = $false
        try {
            New-Item -ItemType Directory -Path $finalReleasePath -Force | Out-Null
            foreach ($relative in $managedPaths) {
                $target = Join-Path $finalReleasePath $relative
                $source = Join-Path $releasePath $relative
                $backup = Join-Path $backupRoot $relative
                if (Test-Path -LiteralPath $target) {
                    New-Item -ItemType Directory -Path (Split-Path $backup -Parent) -Force | Out-Null
                    Move-Item -LiteralPath $target -Destination $backup
                }
                $updatedPaths.Add($relative)
                if (Test-Path -LiteralPath $source) {
                    Copy-Item -LiteralPath $source -Destination $target -Recurse -Force
                }
            }
            if (Test-Path -LiteralPath $finalArchivePath) {
                New-Item -ItemType Directory -Path $backupRoot -Force | Out-Null
                Move-Item -LiteralPath $finalArchivePath -Destination (Join-Path $backupRoot "$Version.zip")
                $archiveBackedUp = $true
            }
            Move-Item -LiteralPath $archivePath -Destination $finalArchivePath
            $archiveInstalled = $true
        } catch {
            $rollbackErrors = New-Object 'System.Collections.Generic.List[string]'
            try {
                if ($archiveInstalled -and (Test-Path -LiteralPath $finalArchivePath)) { Remove-Item -LiteralPath $finalArchivePath -Force }
                if ($archiveBackedUp -and (Test-Path -LiteralPath (Join-Path $backupRoot "$Version.zip"))) { Move-Item -LiteralPath (Join-Path $backupRoot "$Version.zip") -Destination $finalArchivePath }
            } catch { $rollbackErrors.Add($_.Exception.Message) }
            for ($i = $updatedPaths.Count - 1; $i -ge 0; $i--) {
                $relative = $updatedPaths[$i]
                $target = Join-Path $finalReleasePath $relative
                $backup = Join-Path $backupRoot $relative
                try {
                    if (Test-Path -LiteralPath $target) { Remove-Item -LiteralPath $target -Recurse -Force }
                    if (Test-Path -LiteralPath $backup) {
                        New-Item -ItemType Directory -Path (Split-Path $target -Parent) -Force | Out-Null
                        Move-Item -LiteralPath $backup -Destination $target
                    }
                } catch { $rollbackErrors.Add($_.Exception.Message) }
            }
            if ($rollbackErrors.Count -gt 0) {
                $preserveStaging = $true
                throw "Preview update failed and rollback was incomplete. Backup retained at '$stagingRoot'. $($rollbackErrors -join '; ')"
            }
            throw
        }
    } else {
        Move-Item -LiteralPath $releasePath -Destination $finalReleasePath
        try {
            Move-Item -LiteralPath $archivePath -Destination $finalArchivePath
        } catch {
            if (Test-Path -LiteralPath $finalReleasePath) { Move-Item -LiteralPath $finalReleasePath -Destination $releasePath }
            throw
        }
    }
    Get-FileHash -LiteralPath $finalArchivePath -Algorithm SHA256 | Format-List
    Write-Output "Release directory: $finalReleasePath"
    if ($UpdatePreview -and $artifactExists) { Write-Output 'Updated existing preview; unrecognized runtime files were preserved.' }
} finally {
    $env:GOOS = $savedGoos; $env:GOARCH = $savedGoarch; $env:CGO_ENABLED = $savedCgo
    Set-Location $originalLocation
    if (!$preserveStaging -and (Test-Path -LiteralPath $stagingRoot)) { Remove-Item -LiteralPath $stagingRoot -Recurse -Force }
}
