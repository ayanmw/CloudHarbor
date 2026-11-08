$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path $PSScriptRoot -Parent
$version = (Get-Content -LiteralPath (Join-Path $repoRoot 'release/current-version.json') -Raw | ConvertFrom-Json).version
$fixtureRoot = [IO.Path]::GetFullPath((Join-Path ([IO.Path]::GetTempPath()) ('nas-downloader-package-test-' + [guid]::NewGuid().ToString('N'))))
$tempBoundary = [IO.Path]::GetFullPath([IO.Path]::GetTempPath())
if (!$fixtureRoot.StartsWith($tempBoundary, [StringComparison]::OrdinalIgnoreCase)) { throw 'Unsafe fixture path' }
$global:fixtureCompiler = (Get-Command go.exe -ErrorAction Stop).Source
$global:fixtureSource = Join-Path $fixtureRoot 'fixture-main.go'
New-Item -ItemType Directory -Path $fixtureRoot -Force | Out-Null
[IO.File]::WriteAllText($global:fixtureSource, @"
package main
import("encoding/json";"os";"runtime";"time")
var version,revision,builtAtUTC,sourceSnapshot,fixtureBuild string
func main(){if len(os.Args)!=2||os.Args[1]!="-build-info"{os.Exit(2)};json.NewEncoder(os.Stdout).Encode(map[string]string{"version":version,"revision":revision,"built_at_utc":builtAtUTC,"source_snapshot_sha256":sourceSnapshot,"runtime":runtime.GOOS+"/"+runtime.GOARCH,"go_version":runtime.Version(),"observed_at_utc":time.Now().UTC().Format(time.RFC3339Nano),"fixture_build":fixtureBuild})}
"@, [Text.UTF8Encoding]::new($false))
$global:fixtureBuild = 1
$global:fixtureTarget = Join-Path $fixtureRoot "$version/bin/windows-amd64/baidu-downloader.exe"
$global:fixtureRunning = $false
$global:fixtureStops = @()
$global:fixtureFailCopy = $false
$global:fixtureWrongIdentity = $false
# Exercise the real packaging/install transaction with deterministic mock builds;
# Provider, npm, git and service process effects are mocked; Go builds only a tiny temporary identity fixture.
function npm { $global:LASTEXITCODE = 0 }
function node { $global:LASTEXITCODE = 0 }
function git { if ($args -contains 'rev-parse') { '1111111111111111111111111111111111111111' } elseif ($args -contains 'ls-files') { 'README.md' }; $global:LASTEXITCODE = 0 }
function go {
 if ($args[0] -eq 'build') {
  $index = [Array]::IndexOf($args, '-o'); $target = $args[$index + 1]
  if ($target.EndsWith('.exe')) {
   $linkerIndex = [Array]::IndexOf($args, '-ldflags')
   $fixtureFlags = $args[$linkerIndex + 1] + " -X main.fixtureBuild=$global:fixtureBuild"
   if ($global:fixtureWrongIdentity) { $fixtureFlags += " -X main.version=v0.0.0" }
   & $global:fixtureCompiler build -trimpath -buildvcs=false -ldflags $fixtureFlags -o $target $global:fixtureSource
   if ($LASTEXITCODE -ne 0) { throw 'Identity fixture compilation failed' }
  } else { [IO.File]::WriteAllText($target, "fixture-program-$global:fixtureBuild") }
 }
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
 $info = & $global:fixtureTarget -build-info | ConvertFrom-Json
 $notes = [IO.File]::ReadAllText((Join-Path $release 'VERSION-NOTES.zh-CN.md'))
 $builtUTC = if ($info.built_at_utc -is [DateTime]) { $info.built_at_utc.ToUniversalTime().ToString('o') } else { $info.built_at_utc }
 if (!$notes.StartsWith("# $version 程序包说明") -or !$notes.Contains($builtUTC) -or !$notes.Contains($info.source_snapshot_sha256) -or !$notes.Contains($info.revision)) { throw 'Package notes do not identify the compiled fixture' }
 if (!$notes.Contains('<details>') -or $notes.IndexOf('## 本版已交付能力') -gt $notes.IndexOf('<details>')) { throw 'Published summary must precede frozen developer appendix' }
 $notesManifest = Get-Content -LiteralPath (Join-Path $repoRoot 'release/current-version.json') -Raw | ConvertFrom-Json
 $sourceNotes = [IO.File]::ReadAllText((Join-Path $repoRoot $notesManifest.notes))
 if (!$notes.Contains($sourceNotes)) { throw 'Frozen developer notes were changed or lost' }
 . (Join-Path $PSScriptRoot 'package-notes.ps1')
 foreach ($invalidNotes in @('# no published section', "## 本版已交付能力`n`n## 下一项`n待验收")) {
  $missingSummaryRejected = $false
  try { New-PackageVersionNotes -Version $version -Revision 'fixture' -SourceSnapshot ('a' * 64) -BuiltAtUTC '2026-11-06T00:00:00Z' -SourceNotes $invalidNotes | Out-Null } catch { $missingSummaryRejected = $_.Exception.Message -like '*nonempty published-capabilities*' }
  if (!$missingSummaryRejected) { throw 'Missing or empty published summary was silently accepted' }
 }


 $global:fixtureBuild = 2; $global:fixtureRunning = $true
 & (Join-Path $PSScriptRoot 'package.ps1') -OutputRoot $fixtureRoot
 Assert-Equal ((& $global:fixtureTarget -build-info | ConvertFrom-Json).fixture_build) '2' 'Program was not replaced'
 Assert-Equal $global:fixtureStops.Count 1 'Target process was not stopped exactly once'
 foreach ($relative in $owned) { Assert-Equal (Get-FileHash -LiteralPath (Join-Path $release $relative) -Algorithm SHA256).Hash $hashes[$relative] ('User data changed: ' + $relative) }
 $zipHash = (Get-FileHash -LiteralPath "$release.zip" -Algorithm SHA256).Hash
 $global:fixtureBuild = 3; $global:fixtureFailCopy = $true; $failed = $false
 try { & (Join-Path $PSScriptRoot 'package.ps1') -OutputRoot $fixtureRoot } catch { $failed = $true }
 if (!$failed) { throw 'Injected failure was not observed' }
 Assert-Equal ((& $global:fixtureTarget -build-info | ConvertFrom-Json).fixture_build) '2' 'Rollback lost installed program'
 Assert-Equal (Get-FileHash -LiteralPath "$release.zip" -Algorithm SHA256).Hash $zipHash 'Rollback changed archive'
 foreach ($relative in $owned) { Assert-Equal (Get-FileHash -LiteralPath (Join-Path $release $relative) -Algorithm SHA256).Hash $hashes[$relative] ('Rollback changed user data: ' + $relative) }
 $global:fixtureFailCopy = $false; $global:fixtureWrongIdentity = $true; $global:fixtureRunning = $true; $identityRejected = $false
 try { & (Join-Path $PSScriptRoot 'package.ps1') -OutputRoot $fixtureRoot } catch { $identityRejected = $_.Exception.Message -like '*Compiled build identity does not match*' }
 if (!$identityRejected) { throw 'Mismatched compiled identity was not rejected' }
 Assert-Equal $global:fixtureStops.Count 1 'Identity rejection stopped an installed process'
 Assert-Equal ((& $global:fixtureTarget -build-info | ConvertFrom-Json).fixture_build) '2' 'Identity rejection replaced installed program'
 Assert-Equal (Get-FileHash -LiteralPath "$release.zip" -Algorithm SHA256).Hash $zipHash 'Identity rejection changed archive'
 foreach ($relative in $owned) { Assert-Equal (Get-FileHash -LiteralPath (Join-Path $release $relative) -Algorithm SHA256).Hash $hashes[$relative] ('Identity rejection changed user data: ' + $relative) }
 Write-Output 'PASS: default active version, clean snapshot, compiled notes, exact process, data preservation, rollback and identity rejection.'

} finally {
 if ((Test-Path -LiteralPath $fixtureRoot) -and $fixtureRoot.StartsWith($tempBoundary,[StringComparison]::OrdinalIgnoreCase)) { Remove-Item -LiteralPath $fixtureRoot -Recurse -Force }
}
