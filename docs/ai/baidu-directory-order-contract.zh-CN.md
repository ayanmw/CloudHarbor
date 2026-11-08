# 目录排序接口证据与实现边界（v0.8.31）

核查日期：2026-11-06。仅接口/源码核查，不是本账号API验收，不变更安装程序。

## 一手证据

直接读取官方Go SDK固定提交472e4a1b111eac1d6307e38802f8fe7adf7e8c21的[file_list.go](https://github.com/baidu-netdisk/baidu-drive-sdk-go/blob/472e4a1b111eac1d6307e38802f8fe7adf7e8c21/baidudriver/api/file_list.go)。ListParams第22–37行定义Order=name/time/size、Desc、Start、Limit及Folder；第119–145行将这些字段映射到请求参数。SDK引用的[开放平台列表文档](https://pan.baidu.com/union/doc/nksg0sat9)本次无法读取正文，因此不将未读取文档当成证明。第三方封装和网页逆向参数不作为合同依据。

| 能力 | 已核实事实 | 不能推出的结论 |
|---|---|---|
| 名称、时间、大小 | 官方SDK暴露三种order，现有程序同样限制为三种 | 不能据此认定type是受支持排序字段 |
| 升降序与分页 | SDK将desc/start/limit传给远端；现有程序逐页保持返回顺序 | SDK源码本身不能证明本账号真实排序或多个分页是原子快照 |
| 文件夹过滤 | SDK将folder=1描述为仅文件夹 | 这不是目录优先，也不证明存在仅文件过滤或混合列表的目录优先参数 |
| 大小排序 | 现有请求按网盘size排序 | 不能按本地递归目录缓存大小对整盘远端结果排序 |

## 当前源码核对

- internal/baiduapi/client.go：DirectoryOrder.Valid只接受name/size/time；order/desc在分页请求中传给远端；不在当前页局部重排。
- internal/baiduapi/directory_order_test.go：模拟provider测试校验order/desc、start、limit+1探测及保持返回顺序；非法type在发请求前拒绝。这是封装证据，不是实际provider证据。
- internal/downloader/cloud.go：非默认排序的分页边界指纹包含directory/order/descending；不能用另一个排序的边界沿用分页。
- frontend/src/CloudFilesWorkspace.vue：响应排序与请求核对；缓存搜索不显示实时排序入口；列表说明大小排序采用网盘值，本地目录汇总不参与。

## 本轮决定与后续条件

保留现有三种远端排序；不添加无依据的类型/目录优先参数，不把当前页重排标注为整目录排序。目录优先若后续实施，需要可靠官方合同，或另行设计明确标注的完整清单快照模式：范围/内存/分页成本、取消、外部变化和新鲜度分别验收。不能为目录优先静默全盘扫描，也不能拼接两种过滤列表而跳过分页完整性证明。

本轮只补接口证据与验收边界，未改变UI或后台行为；当前安装仍是北京时间2026-11-06 10:00:27的v0.8.31，不重新打包。后续优先继续现有工作流与异常反馈收敛，不将扩展候选自动加入本版承诺。


## 后续源码实施更新：本地完整目录排序与混排

上文provider合同及已安装版本记录保留。后续实现没有增加未经证实的百度混排参数，而是完整读取当前目录直接子项后，由本工具按name/显示size/time排序并固定快照分页。默认混排，关闭目录优先且两组各自排序，mode与order/方向绑定token。接口沿用私有/api/cloud-display-size名称以兼容已有size调用，新增order=name/time、mixed=0/1，元数据display_size也沿用旧名。旧/api/cloud及SDK语义不变。当前源码最终615项前端及相关Go/vet通过；未合入运行包、未作为真实provider排序验收。
