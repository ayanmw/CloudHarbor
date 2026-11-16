param(
    [string]$AccessToken = $env:BAIDU_ACCESS_TOKEN,
    [string]$RefreshToken = $env:BAIDU_REFRESH_TOKEN,
    [string]$AppID = $env:BAIDU_APP_ID,
    [string]$AppKey = $env:BAIDU_APP_KEY,
    [string]$SecretKey = $env:BAIDU_SECRET_KEY,
    [string]$SignKey = $env:BAIDU_SIGN_KEY,
    [string]$AdminPassword = $env:ADMIN_PASSWORD,
    [string]$PublicOrigin = $env:PUBLIC_ORIGIN,
    [string]$QueueDir = $(if (-not [string]::IsNullOrWhiteSpace($env:QUEUE_DIR)) { $env:QUEUE_DIR } elseif (-not [string]::IsNullOrWhiteSpace($env:QUEUE_DIR_DEFAULT)) { $env:QUEUE_DIR_DEFAULT } else { '/NAS_Download_Queue_preview' }),
    [string]$DownloadDir = $(if ([string]::IsNullOrWhiteSpace($env:LOCAL_DOWNLOAD_DIR)) { Join-Path $PSScriptRoot 'downloads' } else { $env:LOCAL_DOWNLOAD_DIR }),
    [int]$Port = 18089,
    [int]$MaxParallel = 2,
    [int]$MaxRetries = 3,
    [int]$RetryBackoffSeconds = 5
)
$ErrorActionPreference = 'Stop'
$queuePathExplicit = $PSBoundParameters.ContainsKey('QueueDir') -or -not [string]::IsNullOrWhiteSpace($env:QUEUE_DIR)
$localPathExplicit = $PSBoundParameters.ContainsKey('DownloadDir') -or -not [string]::IsNullOrWhiteSpace($env:LOCAL_DOWNLOAD_DIR)
$env:BAIDU_ACCESS_TOKEN = $AccessToken
$env:BAIDU_REFRESH_TOKEN = $RefreshToken
$env:BAIDU_APP_ID = $AppID
$env:BAIDU_APP_KEY = $AppKey
$env:BAIDU_SECRET_KEY = $SecretKey
$env:BAIDU_SIGN_KEY = $SignKey
$env:ADMIN_PASSWORD = $AdminPassword
$env:PUBLIC_ORIGIN = $PublicOrigin
if ($queuePathExplicit) { $env:QUEUE_DIR = $QueueDir; Remove-Item Env:QUEUE_DIR_DEFAULT -ErrorAction SilentlyContinue } else { Remove-Item Env:QUEUE_DIR -ErrorAction SilentlyContinue; $env:QUEUE_DIR_DEFAULT = $QueueDir }
if ($localPathExplicit) { $env:LOCAL_DOWNLOAD_DIR = [IO.Path]::GetFullPath($DownloadDir) } else { Remove-Item Env:LOCAL_DOWNLOAD_DIR -ErrorAction SilentlyContinue }
$env:SCAN_INTERVAL = '30'
$env:MAX_PARALLEL = [string]$MaxParallel
$env:MAX_RETRIES = [string]$MaxRetries
$env:RETRY_BACKOFF_SECONDS = [string]$RetryBackoffSeconds
$env:SERVER_PORT = [string]$Port
$env:SERVER_HOST = '127.0.0.1'
Write-Host "Preview: http://127.0.0.1:$Port ; manage paths at /cloud unless .env or launch parameters set them"
Push-Location $PSScriptRoot
try {
    & (Join-Path $PSScriptRoot 'bin/windows-amd64/cloudharbor.exe')
    $exitCode = $LASTEXITCODE
} finally {
    Pop-Location
}
exit $exitCode
