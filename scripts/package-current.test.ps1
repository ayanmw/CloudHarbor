$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path $PSScriptRoot -Parent
$version = (Get-Content -LiteralPath (Join-Path $repoRoot 'release/current-version.json') -Raw | ConvertFrom-Json).version
$fixtureRoot = [IO.Path]::GetFullPath((Join-Path ([IO.Path]::GetTempPath()) ('nas-downloader-package-test-' + [guid]::NewGuid().ToString('N'))))
$tempBoundary = [IO.Path]::GetFullPath([IO.Path]::GetTempPath())
if (!$fixtureRoot.StartsWith($tempBoundary, [StringComparison]::OrdinalIgnoreCase)) { throw 'Unsafe fixture path' }
$global:fixtureBuild = 1
$global:fixtureTarget = Join-Path $fixtureRoot "$version/bin/windows-amd64/baidu-downloader.exe"
$global:fixtureRunning = $false
$global:fixtureStops = @()
$global:fixtureFailCopy = $false
# Exercise the real packaging/install transaction with deterministic mock builds;
# provider, npm, git, compiler and OS process effects are not performed by this test.
function npm { $global:LASTEXITCODE = 0 }
function node { $global:LASTEXITCODE = 0 }
function git { if ($args -contains 'rev-parse') { '1111111111111111111111111111111111111111' } elseif ($args -contains 'ls-files') { 'README.md' }; $global:LASTEXITCODE = 0 }
function go {
 if ($args[0] -eq 'build') { $index = [Array]::IndexOf($args, '-o'); [IO.File]::WriteAllText($args[$index + 1], "fixture-program-$global:fixtureBuild") }
 elseif ($args[0] -eq 'version') { 'go version fixture' }
 $global:LASTEXITCODE = 0
}
function Get-CimInstance {
 param($ClassName,$Filter)
 if ($Filter) { if ($global:fixtureRunning) { [pscustomobject]@{ExecutablePath=$global:fixtureTarget;ProcessId=12345} }; return }
 [pscustomobject]@{ExecutablePath='D:\unrelated\baidu-downloader.exe';ProcessId=54321}
 if ($global:fixtureRunning) { [pscustomobject]@{ExecutablePath=$global:fixtureTarget;ProcessId=12345} }
}
function Stop-Process { param($Id,[switch]$Force); if ($Id -ne 12345) { throw 'Attempted to stop an unrelated process' }; $global:fixtureStops += $Id; $global:fixtureRunning = $false }
function Wait-Process { param($Id,$Timeout,$ErrorAction) }
function Copy-Item {
 param($LiteralPath,$Destination,[switch]$Recurse,[switch]$Force)
 if ($global:fixtureFailCopy -and $Destination -eq (Join-Path $fixtureRoot "$version/bin")) { throw 'injected install failure' }
 Microsoft.PowerShell.Management\Copy-Item @PSBoundParameters
}
function Assert-Equal($Actual,$Expected,$Message) { if ($Actual -ne $Expected) { throw $Message } }
try {
 & (Join-Path $PSScriptRoot 'package.ps1') -OutputRoot $fixtureRoot
 $release = Join-Path $fixtureRoot $version
 $owned = @('.nas-downloader/queue/queue.sqlite','.nas-downloader/queue/credentials.json','.env','downloads/user.bin','my-private-file.txt')
 $hashes = @{}
 foreach ($relative in $owned) {
  $file = Join-Path $release $relative
  New-Item -ItemType Directory -Path (Split-Path $file -Parent) -Force | Out-Null
  [IO.File]::WriteAllText($file, 'private-fixture-' + $relative)
  $hashes[$relative] = (Get-FileHash -LiteralPath $file -Algorithm SHA256).Hash
 }
 if (!(Test-Path -LiteralPath (Join-Path $release 'VERSION-NOTES.zh-CN.md'))) { throw 'Version notes missing' }
 if (!(Test-Path -LiteralPath (Join-Path $release 'SOURCE-SNAPSHOT.txt'))) { throw 'Clean-build source snapshot missing' }
 $global:fixtureBuild = 2; $global:fixtureRunning = $true
 & (Join-Path $PSScriptRoot 'package.ps1') -OutputRoot $fixtureRoot
 Assert-Equal ([IO.File]::ReadAllText($global:fixtureTarget)) 'fixture-program-2' 'Program was not replaced'
 Assert-Equal $global:fixtureStops.Count 1 'Target process was not stopped exactly once'
 foreach ($relative in $owned) { Assert-Equal (Get-FileHash -LiteralPath (Join-Path $release $relative) -Algorithm SHA256).Hash $hashes[$relative] ('User data changed: ' + $relative) }
 $zipHash = (Get-FileHash -LiteralPath "$release.zip" -Algorithm SHA256).Hash
 $global:fixtureBuild = 3; $global:fixtureFailCopy = $true; $failed = $false
 try { & (Join-Path $PSScriptRoot 'package.ps1') -OutputRoot $fixtureRoot } catch { $failed = $true }
 if (!$failed) { throw 'Injected failure was not observed' }
 Assert-Equal ([IO.File]::ReadAllText($global:fixtureTarget)) 'fixture-program-2' 'Rollback lost installed program'
 Assert-Equal (Get-FileHash -LiteralPath "$release.zip" -Algorithm SHA256).Hash $zipHash 'Rollback changed archive'
 foreach ($relative in $owned) { Assert-Equal (Get-FileHash -LiteralPath (Join-Path $release $relative) -Algorithm SHA256).Hash $hashes[$relative] ('Rollback changed user data: ' + $relative) }
 Write-Output 'PASS: default active version, clean snapshot, notes, exact process, data preservation and rollback.'
} finally {
 if ((Test-Path -LiteralPath $fixtureRoot) -and $fixtureRoot.StartsWith($tempBoundary,[StringComparison]::OrdinalIgnoreCase)) { Remove-Item -LiteralPath $fixtureRoot -Recurse -Force }
}
