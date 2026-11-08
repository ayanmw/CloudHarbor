import { historyCount, historyUTC } from "./cloud-history-types";
import { validCloudDirectory } from "./cloud-files-route";
export const reasons: Record<string, string> = {
  scanning: "扫描中",
  stale: "缓存过期",
  unknown: "状态未知",
  failed: "目录读取失败",
  missing_status: "缺少目录状态",
  orphan_status: "孤立目录状态",
  root_incomplete: "根目录未完整",
  path_mismatch: "缓存路径异常",
  parent_incomplete: "父目录未完整",
  structural: "其他结构问题",
};
export type Conditions = { reason: string; query: string };
export type Row = {
  path: string;
  reason: string;
  status: string;
  cached_directory: boolean;
  observed_at_utc?: string | null;
  last_run_id?: string;
};
export type Page = {
  entries: Row[];
  total: string;
  cache_generation: string;
  next_before_path: string;
  has_more: boolean;
  source: "local_cache";
  reason: string;
  query: string;
  before_path: string;
  limit: number;
  observed_at_utc: string;
};
// SQLite BINARY uses UTF-8 scalar order, unlike JavaScript UTF-16 comparison.
export function comparePaths(a: string, b: string) {
  const aa = Array.from(a),
    bb = Array.from(b);
  for (let i = 0; i < Math.min(aa.length, bb.length); i++) {
    const d = aa[i]!.codePointAt(0)! - bb[i]!.codePointAt(0)!;
    if (d) return d;
  }
  return aa.length - bb.length;
}
export function parsePage(
  d: any,
  c: Conditions,
  before: string,
  limit: number,
  generation = "",
  total = "",
): Page {
  if (
    !d ||
    d.source !== "local_cache" ||
    d.reason !== c.reason ||
    d.query !== c.query ||
    d.before_path !== before ||
    d.limit !== limit ||
    !historyCount(d.total) ||
    !historyCount(d.cache_generation) ||
    !historyUTC(d.observed_at_utc) ||
    typeof d.has_more !== "boolean" ||
    typeof d.next_before_path !== "string" ||
    !Array.isArray(d.entries) ||
    d.entries.length > limit ||
    (generation && d.cache_generation !== generation) ||
    (total && d.total !== total)
  )
    throw Error("缓存清单身份、筛选或代次不一致");
  let previous = before;
  for (const r of d.entries) {
    if (
      !r ||
      typeof r.path !== "string" ||
      !validCloudDirectory(r.path) ||
      Array.from(r.path as string).some((c) => {
        const n = c.codePointAt(0)!;
        return n >= 0xd800 && n <= 0xdfff;
      }) ||
      comparePaths(r.path, previous) <= 0 ||
      !Object.hasOwn(reasons, r.reason) ||
      (c.reason && r.reason !== c.reason) ||
      typeof r.status !== "string" ||
      r.status.length > 128 ||
      typeof r.cached_directory !== "boolean" ||
      (r.observed_at_utc != null && !historyUTC(r.observed_at_utc)) ||
      (r.last_run_id !== undefined &&
        (typeof r.last_run_id !== "string" || r.last_run_id.length > 256))
    )
      throw Error("缓存路径或观察证据无效");
    previous = r.path;
  }
  if (
    BigInt(d.total) < BigInt(d.entries.length) + (d.has_more ? 1n : 0n) ||
    (d.has_more &&
      (d.entries.length !== limit || d.next_before_path !== previous)) ||
    (!d.has_more && d.next_before_path !== "") ||
    (!before && !d.has_more && BigInt(d.total) !== BigInt(d.entries.length))
  )
    throw Error("缓存分页边界无效");
  return d;
}
