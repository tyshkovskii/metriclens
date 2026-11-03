import type { ScrubPosition } from "../hooks/useScrub";

export type MetricLocation = {
  targetId: string | null;
  metric: string | null;
  position: ScrubPosition | null;
  error: string | null;
};

/** URL state shared by agent findings and human metric links. Times are RFC3339. */
export function readMetricLocation(location: Pick<Location, "search" | "hash">): MetricLocation {
  const params = new URLSearchParams(location.search);
  let targetId = params.get("target") || null;
  if (!targetId && location.hash.length > 1) {
    try {
      targetId = decodeURIComponent(location.hash.slice(1));
    } catch {
      return { targetId: null, metric: null, position: null, error: "Invalid target link." };
    }
  }
  const metric = params.get("metric") || null;
  const result: MetricLocation = { targetId, metric, position: null, error: null };
  if (metric && !targetId) {
    return { ...result, error: "Metric links must identify a target." };
  }
  const start = params.get("start");
  const end = params.get("end");
  if (start === null && end === null) {
    return result;
  }
  const timestamp = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/i;
  const from = start && timestamp.test(start) ? Date.parse(start) : NaN;
  const to = end && timestamp.test(end) ? Date.parse(end) : NaN;
  if (!Number.isFinite(from) || !Number.isFinite(to) || from >= to) {
    return {
      ...result,
      error: "Invalid time window: use RFC3339 start and end times with start before end.",
    };
  }
  return { ...result, position: { t: to, domain: [from, to] } };
}
