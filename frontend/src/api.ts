import type {
  AppConfig,
  MetricQualityIssue,
  Series,
  Target,
  TargetData,
  TargetMetricsResponse,
} from "./types";

export async function fetchConfig(): Promise<AppConfig> {
  return fetchJSON<AppConfig>("/api/config");
}

export async function fetchTargets(): Promise<Target[]> {
  return fetchJSON<Target[]>("/api/targets");
}

export async function fetchSeries(targetId: string, metric: string): Promise<Series[]> {
  return fetchJSON<Series[]>(
    `/api/targets/${encodeURIComponent(targetId)}/series?metric=${encodeURIComponent(metric)}`,
  );
}

/**
 * Series for several metrics at once, keyed by metric name. A metric whose
 * fetch fails maps to an empty list so one bad metric doesn't drop the rest.
 */
export async function fetchSeriesByMetric(
  targetId: string,
  metrics: string[],
): Promise<Record<string, Series[]>> {
  const entries = await Promise.all(
    metrics.map((metric) =>
      fetchSeries(targetId, metric).then(
        (series) => [metric, series] as const,
        () => [metric, [] as Series[]] as const,
      ),
    ),
  );
  return Object.fromEntries(entries);
}

export async function loadTargetData(targetId: string): Promise<TargetData> {
  try {
    const [metrics, issues] = await Promise.all([
      fetchJSON<TargetMetricsResponse>(`/api/targets/${encodeURIComponent(targetId)}/metrics`),
      fetchJSON<MetricQualityIssue[]>(`/api/targets/${encodeURIComponent(targetId)}/quality`).catch(() => []),
    ]);
    return { metrics, issues };
  } catch (error) {
    return { issues: [], error: messageFromError(error) };
  }
}

async function fetchJSON<T>(path: string): Promise<T> {
  const response = await fetch(path, { headers: { Accept: "application/json" } });
  const body = await response.json().catch(() => null);
  if (!response.ok) {
    throw new Error(body?.error || response.statusText || "request failed");
  }
  return body as T;
}

function messageFromError(error: unknown) {
  return error instanceof Error ? error.message : "request failed";
}
