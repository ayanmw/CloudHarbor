function New-PackageVersionNotes {
    param(
        [Parameter(Mandatory)][string]$Version,
        [Parameter(Mandatory)][string]$Revision,
        [Parameter(Mandatory)][string]$SourceSnapshot,
        [Parameter(Mandatory)][string]$BuiltAtUTC,
        [Parameter(Mandatory)][string]$SourceNotes
    )
    $summaryMatch = [regex]::Match($SourceNotes, '(?ms)^## 本版已交付能力[ \t]*\r?\n(?<content>.*?)(?=^## |\z)')
    $summary = $summaryMatch.Groups['content'].Value.Trim()
    if (!$summaryMatch.Success -or !$summary) { throw 'Version notes must contain a nonempty published-capabilities section.' }
    # Keep the source record verbatim; its earlier installation status is historical.
    return @"
# $Version 程序包说明

## 本包构建身份

| 项目 | 值 |
|---|---|
| 版本 | $Version |
| 构建 UTC | $BuiltAtUTC |
| 源码标识 | $Revision |
| 完整源码快照 SHA256 | $SourceSnapshot |

此表标识本程序包，用于区分同版本的不同构建。启动后可在“设置 → 版本与诊断”核对当前进程；BUILD-INFO.txt、SOURCE-SNAPSHOT.txt 和 SHA256SUMS.txt 可用于进一步核对。
构建检查通过不等于真实百度、NAS 或完整视觉验收通过，具体边界仍见下方说明。

## 本版已交付能力

$summary

## 完整开发记录（构建前冻结）

附录保留生成本包时的源码说明原文。其中“当前安装构建”“待发布”等属于构建前的进度记录，不用于识别本包；本包身份以上表为准。未验收项按原文保留，不自动改写为通过。附录中的相对链接以源码仓库为基准。

<details>
<summary>展开完整开发记录与验证边界</summary>

$SourceNotes

</details>
"@
}
