export type TargetStatus = "up" | "down";

/** Effective backend timing config from /api/config; UI cadence derives from it. */
export type AppConfig = {
  scrapeIntervalMs: number;
  retentionMs: number;
};

export type Target = {
  id: string;
  serviceName: string;
  containerName: string;
  url?: string;
  status: TargetStatus;
  lastError?: string;
  lastScrapeAt?: string;
  lastScrapeDuration?: string;
  discoveredAt: string;
};

export type MetricFamily = {
  name: string;
  help?: string;
  type: "counter" | "gauge" | "histogram" | "summary" | "untyped";
  samples: MetricSample[];
};

export type MetricSample = {
  metric: string;
  labels: Record<string, string>;
  value: number;
  timestamp?: number;
};

export type TargetMetricsResponse = {
  target: Target;
  families: MetricFamily[];
};

export type PanelKind = "counter_rate" | "gauge";

export type MetricQualityIssue = {
  severity: "info" | "warning" | "error";
  metric: string;
  message: string;
  suggestion?: string;
};

export type SeriesPoint = {
  ts: string;
  value: number;
};

export type Series = {
  targetId: string;
  metric: string;
  labels: Record<string, string>;
  points: SeriesPoint[];
};

export type TargetData = {
  metrics?: TargetMetricsResponse;
  issues: MetricQualityIssue[];
  error?: string;
};
