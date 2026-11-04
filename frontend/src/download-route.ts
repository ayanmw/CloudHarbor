export type DownloadRoute = { task: string; status: string; q: string; offset: number; manifest: string };
const statuses = new Set(["pending", "downloading", "paused", "retry_wait", "auth_wait", "source_unknown", "failed", "cancelled", "done"]);
function validTask(value: string) {
  return /^[1-9][0-9]{0,18}$/.test(value) && (value.length < 19 || value <= "9223372036854775807");
}
export function readDownloadRoute(search: string): DownloadRoute {
  const params = new URLSearchParams(search);
  const rawTask = params.get("task") || "", task = validTask(rawTask) ? rawTask : "";
  const rawStatus = params.get("status") || "", rawQuery = (params.get("q") || "").trim();
  const rawOffset = params.get("offset") || "0", number = Number(rawOffset);
  const rawManifest = params.get("manifest") || "";
  return { task, status: statuses.has(rawStatus) ? rawStatus : "", q: Array.from(rawQuery).length <= 128 ? rawQuery : "", offset: /^(0|[1-9][0-9]*)$/.test(rawOffset) && Number.isSafeInteger(number) ? number : 0, manifest: task && /^[a-f0-9]{32}$/.test(rawManifest) ? rawManifest : "" };
}
export function downloadRouteURL(route: DownloadRoute): string {
  const raw = new URLSearchParams({ task: route.task, status: route.status, q: route.q, offset: String(route.offset), manifest: route.manifest });
  const clean = readDownloadRoute(raw.toString()), params = new URLSearchParams();
  if (clean.task) params.set("task", clean.task);
  if (clean.status) params.set("status", clean.status);
  if (clean.q) params.set("q", clean.q);
  if (clean.offset) params.set("offset", String(clean.offset));
  if (clean.manifest) params.set("manifest", clean.manifest);
  return params.size ? "/?" + params : "/";
}
