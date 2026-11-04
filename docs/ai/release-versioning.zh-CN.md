# 版本维护规则（2026-11-04起生效）

用户已结束v0.8.30并明确进入v0.8.31。当前版本由[配置](../../release/current-version.json)指定；[v0.8.31内容说明](../releases/v0.8.31.zh-CN.md)持续维护。

## 固定版本

1. 当前版本内持续合并功能、修复、UI/UE和验证，同一运行目录与ZIP覆盖更新，不再递增preview号。
2. 只有用户明确说“开始下一个版本”或明确指定新版本才切版。功能数、验收通过、普通“继续”及打包请求都不构成切版授权。
3. 切版时归档本地源码，在新版本启动基线建立同名annotated Git tag。此次创建v0.8.31，含义为启动基线；同版后续更新不移动该tag。最终内容用持续版本说明、Git提交与每包SOURCE-SNAPSHOT定位。
4. 旧的至少五项门槛和自动规划/发布下一编号机制停止生效；历史包不清理、不改名。[旧规则与交付记录](release-versioning-history.zh-CN.md)仅供回溯。

## 内容说明与更新证据

每版说明固定包含：产品目标；承接基线；本版已完成/进行中/待做；功能与交互变更；验证层级与未验收项；已知限制；构建UTC、Git基线/快照和包校验记录链接。规划不能混写成成果，不把历史状态当成当前执行情况。
同版版本号保持不变，通过BUILD-INFO中的提交、内容快照和构建UTC区分每次更新。默认打包读取current-version.json；VERSION-NOTES.zh-CN.md随包交付。

## 程序与用户数据

只替换程序白名单：bin、启动脚本、部署模板、说明、版本说明、BUILD-INFO、SOURCE-SNAPSHOT及SHA256SUMS。保留.nas-downloader、SQLite/WAL/SHM、授权/管理员sidecar、.env、downloads和其他用户文件。不能整个删除发布目录再重建。
构建、测试和三平台包先在staging完成。用户已授权：若当前版本正在运行，可告知后按目标exe绝对路径结束，再进行事务式替换；失败回滚程序，不回滚用户数据。不得按名称终止其他版本。更新后是否启动遵循用户当前任务，不擅自恢复服务。
跨版本先停止/确认无writer，再移动唯一真实状态，逐文件SHA核对、原目录消失并做SQLite只读完整性检查；不复制真实授权，不改变保存的本地下载目录。

## 当前命令

```powershell
# 默认当前版本；同版存在时原位更新。
.\scripts\package.ps1 -AllowDirty
# 显式指定当前版本。
.\scripts\package.ps1 -Version v0.8.31 -AllowDirty -UpdateCurrent
```

本地提交不包含push。下次版本名称和切版时点由用户决定；路线图候选不提前创建v0.8.32。
