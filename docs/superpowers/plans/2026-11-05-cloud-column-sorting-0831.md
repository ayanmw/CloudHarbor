# 网盘表头排序（v0.8.31，同版UI后续）

按已授权P0-A/UE路线图内联执行；不worktree/提交、不启动真实服务、不覆盖已交付包。

设计：复用已有name/size/time远端排序，不在本页重排。名称/大小初选升序，时间初选最新优先，再点同列反向。表头按钮明确有边框，aria-sort仅标记已成功加载的排序列；dropdown保留。新请求未成功时保留旧rows和旧标记并提示，重试仍可用。切排序继续回第一页/清选择；缓存搜索表头不提供不可用排序。

- [x] RED：表头点击参数/选择清理及标记；失败和待定请求不误标；缓存搜索无按钮。
- [x] GREEN：增加loadedSort只在响应校验成功后更新，三列表头/方向图标、复用changeSort；不改后端/URL协议。
- [x] Vue类型/构建、相关及全前端、可见桌面与窄屏检查；版本说明区分源码和交付。

Ledger：表头缺失/aria-sort回归先失败后实现。loadedSort仅在目录响应、排序和分页身份全部校验后提交；pending/failed保留已加载rows/marker，并明确提示。Header按loadedSort而不是尚未成功的dropdown草稿决定下次方向。只读排序失败可明确重试，加载时禁重复请求；仅改变字段复用既有远端全目录排序/清选择/第一页流程。缓存搜索/缓存列表保持普通表头，不伪装支持排序。无后端/URL合同变化。
验证：三列请求/重复点击方向/时间默认降序/清选择/offset0、Pending/503不误标及显式重试、缓存搜索无按钮/标记等新增3项通过；文件UI63项，最终Vue408/408（14.530s）、vue-tsc/Vite、diff检查通过。
可见QA：纯虚构localhost文件列表，点击大小显示1/4/8MiB升序，再键盘Enter显示8/4/1MiB降序；URL order=size/desc和唯一aria-sort匹配。桌面截图releases/v0.8.31-cloud-column-sort-desktop.png。390px窄屏表内横向滚动后点击时间列，URL order=time&desc=1、DOM唯一标记时间descending，page/client均375；窄屏截图捕获请求加载期，保留为mobile-pending.png，不冒称最终方向截图。tab关闭、viewport重置；核实脚本绝对路径后结束mock，18091无监听，临时脚本删除，无真实账号/服务操作。
未发布：已交付ZIP仍973016F819B75B83C93255B07AA1635F7B3F6C5447794D37A435CB2AD74DC889；与密度一起纳入后续同版UI交付，不自动切版或覆盖本次测试包。