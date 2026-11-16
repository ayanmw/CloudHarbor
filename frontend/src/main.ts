import { createApp, type Component } from "vue";
import "./style.css";

const workspaces: Record<string, () => Promise<{default: Component}>> = {
  "/library": () => import("./LocalLibraryWorkspace.vue"),
  "/directory-analysis": () => import("./DirectoryAnalysisWorkspace.vue"),
  "/sync": () => import("./FolderSyncWorkspace.vue"),
  "/settings/about": () => import("./BuildInfoWorkspace.vue"),
  "/uploads": () => import("./UploadWorkspace.vue"),
  "/cloud/history": () => import("./CloudHistoryWorkspace.vue"),
  "/cloud/cleanup": () => import("./CloudCleanupWorkspace.vue"),
  "/cloud/cache": () => import("./CloudCacheWorkspace.vue"),
  "/cloud/files": () => import("./CloudFilesWorkspace.vue"),
  "/settings/migrations": () => import("./MigrationWorkspace.vue"),
  "/settings/paths": () => import("./PathSettingsWorkspace.vue"),
  "/oauth": () => import("./OAuthWorkspace.vue"),
  "/intake": () => import("./IntakeWorkspace.vue"),
  "/intake-history": () => import("./IntakeWorkspace.vue"),
  "/task-events": () => import("./EventWorkspace.vue"),
  "/monitor-history": () => import("./MonitorWorkspace.vue"),
  "/operations": () => import("./OperationWorkspace.vue"),
};
const loadWorkspace = workspaces[location.pathname] ?? (() => import("./App.vue"));
const host = document.querySelector<HTMLElement>("#app")!;
let loading = false, mounted = false, active = true, generation = 0;
function showLoading() {
  const status = document.createElement("p");
  status.className = "workspace-bootstrap";
  status.setAttribute("role", "status");
  status.textContent = "正在加载页面…";
  host.replaceChildren(status);
}
function showFailure() {
  const panel = document.createElement("section");
  panel.className = "workspace-bootstrap";
  const message = document.createElement("p");
  message.setAttribute("role", "alert");
  message.textContent = "页面加载失败。请检查连接后重试；如果程序刚更新，请刷新页面。";
  const retry = document.createElement("button");
  retry.type = "button";
  retry.className = "button secondary";
  retry.dataset.testid = "workspace-load-retry";
  retry.textContent = "重试加载";
  retry.addEventListener("click", () => { void mountWorkspace(); });
  panel.append(message, retry);
  host.replaceChildren(panel);
}
async function mountWorkspace() {
  if (!active || loading || mounted) return;
  const request = ++generation;
  loading = true;
  showLoading();
  try {
    const workspace = await loadWorkspace();
    if (!active || request !== generation) return;
    createApp(workspace.default).mount(host);
    mounted = true;
    window.removeEventListener("pagehide", hideBeforeMount);
    window.removeEventListener("pageshow", restoreBeforeMount);
  } catch {
    if (active && request === generation) showFailure();
  } finally {
    if (request === generation) loading = false;
  }
}
function hideBeforeMount() { active = false; generation++; loading = false; }
function restoreBeforeMount(event: PageTransitionEvent) {
  if (event.persisted && !mounted) { active = true; void mountWorkspace(); }
}
window.addEventListener("pagehide", hideBeforeMount);
window.addEventListener("pageshow", restoreBeforeMount);
await mountWorkspace();
