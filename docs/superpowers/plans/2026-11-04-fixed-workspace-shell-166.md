# .166 同版修订：固定工作区框架

用户明确要求固定顶部Logo/核心选项，下方固定左菜单/右内容；入口必须有明显可点击样式。沿用Vue/Go内嵌资源，保留dirty主目录，不worktree/commit/push。此为.166布局修正，不创建.167。

设计：顶部统一64px Header，Logo、传输/网盘/记录/设置模块、账号/登出；每个模块左侧显示本模块菜单。筛选/刷新/新建/上传/下载设置等具体操作留在内容区，危险操作继续既有确认。共享WorkspaceSidebar渲染Header和菜单，下载页也复用它，消除两套导航。顶层inert显式同时作用Header/aside，模态与焦点不回归。
网盘相关页共用有图标/边框/hover/选中态的快捷导航条，简化为文件/缓存与刷新/重复与空目录/扫描历史/高级管理，保留目录参数。高级旧页同步顶部和模块菜单布局，不改业务事件监听。窄屏顶部缩短标签、侧栏图标保留可访问名称和提示、表格只在容器内滚动。

- [x] 先写共享Header/模块切换/导航可点击样式契约回归并观察RED。
- [x] 实现共享导航数据、Header/Sidebar；下载页复用，分区路径选中正确。
- [x] 网盘快捷入口及旧高级页面框架一致；固定位置和响应式CSS。
- [x] 定向/完整前端/legacy、可见桌面/390px/键盘、最终一次fresh review；同版打包门禁与产物校验。真实状态保留在.166；服务若正在运行先核实再操作，不随意中断上传。

Ledger：既有.166已交付并移入唯一账号46文件，服务上次交付时停止，当前需重新核实。此次仅布局与可发现性，不改变上传/下载/网盘写操作协议。

实现与验收：共享WorkspaceHeader/Sidebar和导航数据，下载页移除独立aside/topbar并复用，Sidebar多根时inert显式传两根，状态slot保留，登出失败不误跳转。四网盘页快捷导航图标/边框/选中/hover，缓存目录参数保留；高级旧页同步header/侧栏/标签样式。305/305完整Vue、172/172 legacy通过，同一.166最终review的本次UI补充复核无Critical/Important。
CUA本机fixture桌面、滚动838px Header top0/侧栏top64；390px Header60且documentWidth375<viewport390；模块切换记录页菜单正确；新建弹窗Escape后焦点返回；高级旧页Header/侧栏/内容顶部64一致。初次窄屏截图接口一次捕获失败，读DOM确认页面正确后用已文档化tab.screenshot完成；截图同级166-header-desktop/scroll/mobile/advanced.png。fixture已停脚本已删，viewport复原、tab关闭。
当前无真实下载器进程，唯一真实状态仍在.166，不再次移动或复制。进入同版UpdatePreview完整发布门禁；最终源码快照/ZIP哈希由外部166-validation.md更新，避免写入包内循环哈希。
