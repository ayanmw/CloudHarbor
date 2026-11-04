# .159 账号与授权工作台实施计划

> 使用superpowers:executing-plans原目录内联；任务先行为回归失败再实现。不得自动commit/push或worktree。

Goal：把/oauth迁移为一致的Vue账号工作台，保留首次空配置、掩码、设备授权与续期能力。
Architecture：OAuthWorkspace、OAuthAppConfig、OAuthDeviceAuthorization按当前页面职责拆分；oauth-types仅校验真实DTO。main.ts与HandlePageOAuth切嵌入入口，旧模板保留到能力核对完成。
Tech Stack：现有Vue3/TypeScript/Vite和Go，不引入依赖。
Spec：docs/superpowers/specs/2026-11-03-oauth-workspace-159.zh-CN.md。

## 约束与审查重点

- 保存前GET必须明确editable=true，非空/读取失败不能变成可写；明文不放持久化或URL。
- 掩码不是原凭据；.env/env优先、queue fallback、空才可写和立即生效由现有后端约束，不扩展覆盖入口。
- 设备授权请求替换/取消/401/离页后旧响应不得复活；后端可能已完成，取消不等于撤销。
- 所有反馈绑定操作上下文；状态未知不能复用旧可执行快照。
- 全站现有布局类与窄屏列宽需可见验收，模拟数据不代表真实服务证据。

## 任务

- [x] Task1：阅读index_oauth.html全部控件和authorization/server.go、main.handleAppConfig接口，建立能力对照。frontend/scripts/ui-oauth.test.mjs先验证/oauth仍非Vue及状态空/失败/到期/续期信息缺失，再接OAuthWorkspace、main.ts、webui入口与Go权限回归；已有5秒读取改为单在途且离页/401停止，使用计时器控制测试。
- [x] Task2：先覆盖首次配置允许/非空禁止/部分配置禁止/GET失败不出现表单、四字段校验、重复提交、成功清空明文与立即读回，再接OAuthAppConfig.vue。对照后端201响应与GET不同DTO，失败不误报保存成功。
- [x] Task3：先覆盖四项掩码/source展示、只读状态、无重置/覆盖按钮、配置保存后设备码按钮可用、查询迟到不恢复表单，再完成掩码/引导界面和布局。保留开发者平台官方入口与设备流程无回调说明。
- [x] Task4：先覆盖设备码获取/替换/安全链接/有效期/旧响应隔离、用户码与device_code隔离、验证等待状态，再接OAuthDeviceAuthorization.vue。只向既有token端点传内存device_code，不自动开启用户未确认的授权等待。
- [x] Task5：先覆盖停止等待/手动续期互斥、断网/401/过期及迟到反馈，完成旧能力对照。一次最终独立审查、桌面/390px/键盘模拟验收、完整Go/vet/前端/旧页/脚本和三平台构建，再独立ZIP/源码快照校验后提供.159测试包。

Ledger：.158五项完成并冻结，最终ZIP哈希见releases同级.158-validation.md。当前只建立.159设计和计划，未改产品代码/创建发布包/访问真实账号。下一步Task1能力对照及失败测试。

## 能力对照与执行记录

旧页核对：GET /api/oauth/status展示state/ready/expires_at/can_refresh/next_attempt/last_error，5秒读取；GET /api/app-config提供configured/editable/source及四项mask，POST成功201没有editable字段，必须重新GET决定表单。获取设备码POST /device-code，token仅传device_code并由服务端轮询，Abort停止本次等待；手动POST /refresh后读状态。保留百度https域名链接白名单、开发平台指导与当前设备流程无回调说明。旧模板目前保留，其完整迁移在后续任务对照。

Task1：先观察6条Vue挂载/时间字段/单在途/401/离页回归全失败及Go入口回归失败，再接OAuthWorkspace.vue、oauth-types.ts、main.ts与HandlePageOAuth。使用现有workspace/main布局；状态到期、下次续期、更新时间按原时区/纳秒文本分别展示。GET失败保留旧证据并明确历史，首次失败不推断未配置续期（新增回归先失败再改未知）。DTO无效不采纳为可用。

5秒单请求在途，pagehide取消并隔离迟到结果；BFCache pageshow只恢复一个定时器/新读，401清理并停止。前端最终120/120（本页9/9）、TypeScript/构建、Go资源1.247s、管理入口/全局历史5.254s、diff通过。没有产品凭据写入接口或设备授权接线；尚未接配置表单/掩码/设备流程/续期动作。

当前1/5，当前开发页不打包；.158保持冻结。无服务启动、可见UI验收、真实账号请求、凭据复制迁移、Git提交或worktree。下一步Task2先写配置只空可写/重复提交/成功清明文及读回失败回归，再接首次配置组件。

Task2：先观察6条首次配置回归失败，再接OAuthAppConfig.vue和GET DTO校验。仅明确configured=false/editable=true/source空才显示四项表单；非空、部分env和读取失败不允许填写。四字段非空/长度校验、防重复POST；配置成功201响应不当作GET DTO，清除本次明文后GET重新确认。只有接受成功且读回configured=true才显示已保存并立即启用。

保存响应丢失或异常只GET核对，不自动重发；读回失败时旧可写快照不可用。读回报告已有配置则只读；报告为空则明确响应未确认、需核对后重新填写。pagehide清明文/取消并隔离迟到，恢复读状态。测试只用合成test-*凭据和已有Go临时夹具，未触碰真实sidecar。

最终TypeScript/构建、前端126/126（本页15/15）、Go资源1.348s、管理/全局历史/AppConfig仅空可写及掩码回归5.724s、diff通过。当前2/5，四项掩码/来源/开发平台指导展示、设备授权、续期/取消仍未完成；开发页不打包。无服务启动、真实账号或凭据写入、Git提交或worktree。下一步Task3读回掩码/来源、查询迟到与配置状态联动。

Task3展示与时序：先观察6条掩码/来源/指导/历史/离页/旧校验回归失败，再接四项只读掩码表、env/.env优先与queue来源、本机读回UTC、官方开发者平台指导和当前设备码流程无需回调说明。GET额外的明文字段不会进入精选DTO或页面。刷新失败保留掩码但明确历史，不开放表单。

离页可写表单未隐藏的问题已先红后绿：active改响应式并参与editable，离页清明文、恢复重新读。迟到空配置不能覆盖新configured结果。新读清旧校验提示；另先复现旧保存成功提示在新空状态下残留，再将反馈限定于原保存上下文，新手动读不延用旧成功。

本轮TypeScript/构建、前端133/133（本页22/22）、Go资源1.246s、管理/全局历史/AppConfig回归5.946s、diff通过。Task3不勾选：保存后设备码按钮启用的联动需随Task4实际设备授权组件接入后验证，不能用占位按钮冒充完成。当前完成2项，Task3展示部分已完成；下一轮接Task4并补齐Task3联动。尚未可见UI验收/独立审查/完整门禁，不打包。无真实账号、服务启动、凭据文件操作、Git提交或worktree。

Task3联动完成：新增配置保存→GET确认→设备码按钮启用回归（先失败再接线），不因POST201单独启用，不自动获取/验证。OAuthAppConfig configured事件已接父页，读配置期间/失败/锁定未知都会使设备入口不可执行。此前掩码/来源/指南及时序回归仍全部通过，Task3现在勾选。

Task4：核实DeviceCodeResponse及提供方校验边界（interval 1..60，expires_in 1..3600，device_code<=4096）。8条设备流程回归先失败，再接OAuthDeviceAuthorization.vue及纯DTO/https百度域名白名单。用户码/授权链接/二维码与私有device_code隔离，后者只在内存传token请求，不写地址/存储/日志/界面。获取后手动验证，重复验证只一请求；换码取消旧等待并隔离迟到，坏新响应不保留旧可执行码。

本机估算截止从请求开始时间保守计算，并明确服务端最终校验；本机到期禁发新的token请求，不中途断言正在验证的结果失败。离页/配置未知/登录变化清码和取消请求。验证响应仅解析公开Status；结束后父页取消旧状态读再重新查询，旧状态响应不覆盖新授权观察。基本停止等待控件已接入，取消/401/续期互斥的完整回归留Task5。

最终构建、前端141/141（本页30/30）、Go资源1.383s、authorization1.409s、管理/全局历史/AppConfig6.013s、diff通过。当前4/5，剩余续期和跨操作互斥/失败恢复、一次最终审查、可见桌面/390px/键盘与完整发布门禁。尚未打包，无真实账号、凭据文件操作、服务启动或Git提交。.158冻结。

Task5代码：新增续期/互斥/取消/失败/401/离页回归先失败，再接手动续期、配置读取/保存busy与device busy互斥。仅当前有效Status.can_refresh且应用配置已核对才允许续期；读失败不使用旧权限，POST结果不替代后续GET观察。续期/取消等待结束强制新读，迟到结果不复活。取消不声称撤销已完成授权；失败不自动重发token/续期。前端148/148、Go资源1.550s/authorization1.551s、管理/AppConfig7.754s通过。

最终独立审查未发现确定可复现需修改缺陷，定向37/37通过。可见本机模拟验收：首次四字段填写→保存→明文消失/掩码只读、设备码/验证等待互斥/成功状态读回、手动续期、桌面/390px及键盘Enter展开指导通过。窄屏掩码表格改为容器宽度，不再强制横向滚动；调整后前端148/148通过。仅mock-*合成数据与内存模拟，未加载百度二维码或访问真实网盘，二维码链接过滤由自动化验证。

临时标签页关闭、viewport reset、fixture服务停止并删除脚本；截图在releases同级.159-setup/desktop/mobile/mobile-config.jpg。完整发布门禁与最终ZIP校验待执行，Task5暂不勾选，无真实账号/凭据文件/打包程序启动或Git提交。

Task5最终：完整package退出0，Go全包/vet、前端148/148、legacy172/172、userscript3/3、Windows AMD64/Linux AMD64/ARM64构建通过。独立ZIP13项/12载荷、617源码逐项快照、发布目录一致、PE/ELF架构及私有状态排除校验通过。独立审查无确定需修复缺陷，桌面/390px/键盘及完整模拟流程完成。补齐文档后更新同版再校验最终哈希；真实百度/NAS/SMB、百度二维码资源和打包二进制启动未验收，未迁移授权或提交Git。
