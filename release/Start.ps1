param(
    [string]$AccessToken = $env:BAIDU_ACCESS_TOKEN,
    [string]$RefreshToken = $env:BAIDU_REFRESH_TOKEN,
    [string]$AppKey = $env:BAIDU_APP_KEY,
    [string]$SecretKey = $env:BAIDU_SECRET_KEY,
    [string]$AdminPassword = $env:ADMIN_PASSWORD,
    [string]$QueueDir = '/NAS_Download_Queue_preview',
    [string]$DownloadDir = (Join-Path $PSScriptRoot 'downloads'),
    [int]$Port = 18089,
    [int]$MaxParallel = 2,
    [int]$MaxRetries = 3,
    [int]$RetryBackoffSeconds = 5
)
$ErrorActionPreference = 'Stop'
$env:BAIDU_ACCESS_TOKEN = $AccessToken
$env:BAIDU_REFRESH_TOKEN = $RefreshToken
$env:BAIDU_APP_KEY = $AppKey
$env:BAIDU_SECRET_KEY = $SecretKey
$env:ADMIN_PASSWORD = $AdminPassword
$env:QUEUE_DIR = $QueueDir
$env:LOCAL_DOWNLOAD_DIR = [IO.Path]::GetFullPath($DownloadDir)
$env:SCAN_INTERVAL = '30'
$env:MAX_PARALLEL = [string]$MaxParallel
$env:MAX_RETRIES = [string]$MaxRetries
$env:RETRY_BACKOFF_SECONDS = [string]$RetryBackoffSeconds
$env:SERVER_PORT = [string]$Port
$env:SERVER_HOST = '127.0.0.1'
Write-Host "Preview: http://127.0.0.1:$Port ; cloud queue: $QueueDir ; downloads: $env:LOCAL_DOWNLOAD_DIR"
& (Join-Path $PSScriptRoot 'bin/windows-amd64/baidu-downloader.exe')
exit $LASTEXITCODE
