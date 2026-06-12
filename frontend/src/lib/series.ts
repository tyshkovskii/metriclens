import { labelsText } from "./format";
import type { MetricFamily, PanelKind, Series, SeriesPoint } from "../types";

export type NamedSeries = {
  name: string;
  points: SeriesPoint[];
};

export type StackRow = { ts: number } & Record<string, number | null>;

const MAX_SERIES = 8;

/** Last value at or before t (epoch ms); null if t precedes the series. */
export function valueAt(points: SeriesPoint[], t: number): number | null {
  let low = 0;
  let high = points.length - 1;
  let found = -1;
  while (low <= high) {
    const mid = (low + high) >> 1;
    if (Date.parse(points[mid].ts) <= t) {
      found = mid;
      low = mid + 1;
    } else {
      high = mid - 1;
    }
  }
  return found >= 0 ? points[found].value : null;
}

/**
 * How to chart a metric: cumulative metrics as per-second rates, everything
 * else as raw values. This is deliberately simpler than the backend's
 * classifier (internal/classifier, served at /api/targets/{id}/panels), which
 * the UI does not consume; if charting rules grow, prefer consuming /panels
 * over extending this heuristic.
 */
export function chartKind(metric: string, type: MetricFamily["type"] | undefined): PanelKind {
  if (type === "gauge") {
    return "gauge";
  }
  if (type === "summary") {
    return metric.endsWith("_count") ? "counter_rate" : "gauge";
  }
  const cumulative =
    type === "counter" ||
    type === "histogram" ||
    metric.endsWith("_total") ||
    metric.endsWith("_count");
  return cumulative ? "counter_rate" : "gauge";
}

/** The sample metric a family's chart plots: throughput for distributions, the raw samples otherwise. */
export function chartMetric(family: MetricFamily): string {
  if (family.type === "histogram") {
    return `${family.name}_count`;
  }
  if (family.type === "summary") {
    const hasQuantiles = family.samples.some((sample) => sample.metric === family.name);
    return hasQuantiles ? family.name : `${family.name}_count`;
  }
  return family.samples[0]?.metric ?? family.name;
}

export function transformSeries(kind: PanelKind, series: Series[]): Series[] {
  return kind === "counter_rate" ? perSeriesRates(series) : series;
}

export function perSeriesRates(series: Series[]): Series[] {
  return series.map((entry) => ({ ...entry, points: ratePoints(entry.points) }));
}

function ratePoints(points: SeriesPoint[]): SeriesPoint[] {
  const rates: SeriesPoint[] = [];
  for (let index = 1; index < points.length; index += 1) {
    const previous = points[index - 1];
    const current = points[index];
    const seconds = (Date.parse(current.ts) - Date.parse(previous.ts)) / 1000;
    if (seconds <= 0) {
      continue;
    }
    const delta = current.value - previous.value;
    // A decrease means the counter reset; assume it restarted from zero.
    rates.push({ ts: current.ts, value: (delta >= 0 ? delta : current.value) / seconds });
  }
  return rates;
}

/**
 * Assign legend names from the most discriminating label key, merge duplicates,
 * and fold everything past the top MAX_SERIES into "other".
 */
export function nameSeries(series: Series[]): NamedSeries[] {
  const nonEmpty = series.filter((entry) => entry.points.length > 0);
  if (!nonEmpty.length) {
    return [];
  }
  if (nonEmpty.length === 1) {
    return [{ name: legendName(nonEmpty[0].labels, null), points: nonEmpty[0].points }];
  }

  const distinctValues = new Map<string, Set<string>>();
  nonEmpty.forEach((entry) => {
    Object.entries(entry.labels).forEach(([key, value]) => {
      const values = distinctValues.get(key) || new Set<string>();
      values.add(value);
      distinctValues.set(key, values);
    });
  });
  let bestKey: string | null = null;
  let bestCount = 1;
  distinctValues.forEach((values, key) => {
    if (values.size > bestCount) {
      bestCount = values.size;
      bestKey = key;
    }
  });

  const grouped = new Map<string, SeriesPoint[][]>();
  nonEmpty.forEach((entry) => {
    const name = legendName(entry.labels, bestKey);
    grouped.set(name, [...(grouped.get(name) || []), entry.points]);
  });

  let named = [...grouped.entries()].map(([name, lists]) => ({ name, points: sumPoints(lists) }));
  if (named.length > MAX_SERIES) {
    named.sort((left, right) => lastValue(right.points) - lastValue(left.points));
    const top = named.slice(0, MAX_SERIES - 1);
    const rest = named.slice(MAX_SERIES - 1);
    top.push({ name: "other", points: sumPoints(rest.map((entry) => entry.points)) });
    named = top;
  }
  return named;
}

function legendName(labels: Record<string, string>, key: string | null) {
  if (key && labels[key]) {
    return labels[key];
  }
  return labelsText(labels) || "value";
}

function sumPoints(lists: SeriesPoint[][]): SeriesPoint[] {
  if (lists.length === 1) {
    return lists[0];
  }
  const byTime = new Map<string, number>();
  lists.forEach((points) => {
    points.forEach((point) => {
      byTime.set(point.ts, (byTime.get(point.ts) || 0) + point.value);
    });
  });
  return [...byTime.entries()]
    .map(([ts, value]) => ({ ts, value }))
    .sort((left, right) => Date.parse(left.ts) - Date.parse(right.ts));
}

function lastValue(points: SeriesPoint[]) {
  return points.length ? points[points.length - 1].value : 0;
}

/** Union of timestamps across series; gaps filled with `fill` (0 for stacking, null for lines). */
export function buildRows(named: NamedSeries[], fill: 0 | null): StackRow[] {
  const timestamps = new Set<number>();
  named.forEach((entry) => entry.points.forEach((point) => timestamps.add(Date.parse(point.ts))));
  const sorted = [...timestamps].sort((left, right) => left - right);
  const lookups = named.map((entry) => {
    const byTs = new Map<number, number>();
    entry.points.forEach((point) => byTs.set(Date.parse(point.ts), point.value));
    return byTs;
  });
  return sorted.map((ts) => {
    const row: StackRow = { ts };
    named.forEach((entry, index) => {
      row[entry.name] = lookups[index].get(ts) ?? fill;
    });
    return row;
  });
}

export function bucketRank(value?: string) {
  if (value === "+Inf") {
    return Number.POSITIVE_INFINITY;
  }
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : Number.POSITIVE_INFINITY;
}
