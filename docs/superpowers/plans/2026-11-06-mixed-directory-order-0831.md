# v0.8.31 文件夹与文件混合排序

用户已授权直接实现。保留dirty，主目录内实现，不提交/推送、切版或启动服务。

设计：网盘目录浏览增加默认开启的混合排序checkbox，关闭后固定目录在前、文件在后，两组各自按名称/显示大小/修改时间及方向排序。大小组内未知最后，同值名称升序；时间相同名称升序；名称采用现有Go字典序。复用当前目录完整清单快照，扩展三种排序与mixed模式绑定，不能依赖未证实的provider分组或局部重排。旧size方法与HTTP默认size行为保留；私有快照接口扩展order=name/time及mixed=0/1。混排状态随URL及快照恢复，浏览器保存偏好，不改服务器配置或下载任务。缓存搜索保留既有合同，不显示未实现的排序控件。

- [x] 后端：全清单比较器支持三字段/混排与分组；快照绑定order、desc、mixed，复用取消/代次/身份边界；TDD与跨页测试。
- [x] HTTP：显式校验mixed、order，回显模式证据，兼容旧size请求与只读鉴权；测试参数/跨模式快照拒绝。
- [x] 前端：紧凑checkbox默认开，本机偏好失败可用；所有实时排序走完整快照，URL保存模式并支持历史与刷新，成功响应才标注已应用模式，失败保留旧结果；旧fixture明确更新合同。
- [x] 类型/Vite、完整前端、相关Go/vet及限定独立复审，更新版本说明；不自动发布运行包。


## 实现与验证结果

复用既有进程内快照和私有只读接口（为兼容保留cloud-display-size及display_size命名），支持三字段及mixed参数。ListCloudDisplaySize旧签名保留为size/mixed=true包装，旧/api/cloud与provider调用合同不变。快照同时校对order、方向与mixed；组内排序用精确int64，目录优先不受方向反转。原始ProviderSize和变更前在线身份校对保持原状。界面默认开启，URL显式mode优先浏览器偏好；本地存储失败仍可当前页使用；失败响应不提前更新loadedMixed。

回归：三种字段采用不同的测试数值顺序，覆盖12种模式/方向组合；分组的真0/未知及同值名称；1108项name/time跨页分组和快照翻页无额外provider读取；跨字段/模式token拒绝；HTTP非法参数及回显；前端默认/切换/浏览器保存/历史显式覆盖/存储失败/模式不匹配后重试。旧UI模拟列表显式使用orderedListing构造新合同，不在fetch拦截器自动修补响应，不放宽生产验证。相关分页fixture调整为真实100项页合同。

第一次完整回归发现布局和批量预检两组旧fixture尚未补快照字段；批量预检初始化失败发生在测试finally前，导致JSDOM计时器残留，已仅结束核实为本次的node测试进程，并修复fixture及失败清理，未触碰真实服务。该中断日志保留为mixed-order-initial-full-tests.log，不计通过。

最终vue-tsc/Vite通过（构建2.76s），完整前端615/615通过（49.322s）。相关Go串行定向taskstore4.239s、downloader4.659s、intakeapi0.084s、主程序2.674s通过，四包vet通过。定向diff检查通过；限定独立静态复审无新缺陷。外部证据：releases/v0.8.31-mixed-order-build.log、v0.8.31-mixed-order-frontend-tests.log。

本轮源码及嵌入前端资产已更新；未更新安装程序/ZIP、未访问真实账号或启动服务/ .164，未验证可见UI/窄屏、真实百度及NAS。不提交/推送，不新增版本或标签。
