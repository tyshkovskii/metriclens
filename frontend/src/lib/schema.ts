import { labelsText, sampleKey } from "./format";
import { bucketRank, valueAt } from "./series";
import type { PreviousValue } from "../hooks/useTargetData";
import type { MetricFamily, MetricSample, Series } from "../types";

const DELTA_WINDOW_MS = 60_000;

const UNIT_SUFFIXES: Array<[string, string]> = [
  ["_seconds", "s"],
  ["_milliseconds", "ms"],
  ["_microseconds", "µs"],
  ["_bytes", "bytes"],
  ["_bits", "bits"],
  ["_ratio", "ratio"],
  ["_percent", "%"],
  ["_celsius", "°C"],
  ["_info", "info"],
];

const RUNTIME_PREFIXES = ["go_", "process_", "promhttp_", "python_", "jvm_", "dotnet_", "nodejs_", "scrape_"];

export type ScrubView = {
  active: boolean;
  loading: boolean;
  t: number;
  seriesByMetric: Record<string, Series[]> | null;
};

export type FamilySummary = {
  unit: string | null;
  labelKeys: string[];
  seriesCount: number;
  /** The single unlabeled sample of a plain family; lets the UI inline the value with nothing to expand. */
  scalar: MetricSample | null;
};

export type ResolvedRow = {
  metric: string;
  labels: Record<string, string>;
  value: number | null;
  delta: string | null;
};

export type HistogramGroup = {
  key: string;
  labels: Record<string, string>;
  count: ResolvedRow | null;
  sum: ResolvedRow | null;
  buckets: Array<{ le: string; value: number | null }>;
  quantiles: Array<{ q: string; value: number | null }>;
};

export function inferUnit(name: string): string | null {
  const base = name.endsWith("_total") ? name.slice(0, -"_total".length) : name;
  for (const [suffix, unit] of UNIT_SUFFIXES) {
    if (base.endsWith(suffix)) {
      return unit;
    }
  }
  return null;
}

export function stripBucketLabels(labels: Record<string, string>): Record<string, string> {
  const { le: _le, quantile: _quantile, ...rest } = labels;
  return rest;
}

export function isRuntimeFamily(name: string) {
  return RUNTIME_PREFIXES.some((prefix) => name.startsWith(prefix));
}

export function familySummary(family: MetricFamily): FamilySummary {
  const labelKeys = new Set<string>();
  const seriesKeys = new Set<string>();
  family.samples.forEach((sample) => {
    Object.keys(sample.labels).forEach((key) => {
      if (key !== "le" && key !== "quantile") {
        labelKeys.add(key);
      }
    });
    seriesKeys.add(labelsText(stripBucketLabels(sample.labels)));
  });
  const distribution = family.type === "histogram" || family.type === "summary";
  const only = family.samples.length === 1 ? family.samples[0] : null;
  const scalar = !distribution && only && !Object.keys(only.labels).length ? only : null;
  return {
    unit: inferUnit(family.name),
    labelKeys: [...labelKeys].sort(),
    seriesCount: seriesKeys.size,
    scalar,
  };
}

/** Current value + trend per series, from live samples or from history at the scrub time. */
export function resolveRows(
  family: MetricFamily,
  scrub: ScrubView | null,
  previousValue: (key: string) => PreviousValue | null,
): ResolvedRow[] {
  if (scrub?.active) {
    if (!scrub.seriesByMetric) {
      return family.samples.map((sample) => ({
        metric: sample.metric,
        labels: sample.labels,
        value: null,
        delta: null,
      }));
    }
    const names = [...new Set(family.samples.map((sample) => sample.metric))];
    return names.flatMap((name) =>
      (scrub.seriesByMetric?.[name] || []).map((entry) => {
        const current = valueAt(entry.points, scrub.t);
        const previous = valueAt(entry.points, scrub.t - DELTA_WINDOW_MS);
        return {
          metric: name,
          labels: entry.labels,
          value: current,
          delta: deltaText(rowType(family.type, name), current, previous, DELTA_WINDOW_MS / 1000),
        };
      }),
    );
  }
  return family.samples.map((sample) => {
    const previous = previousValue(sampleKey(sample.metric, sample.labels));
    return {
      metric: sample.metric,
      labels: sample.labels,
      value: sample.value,
      delta: previous
        ? deltaText(rowType(family.type, sample.metric), sample.value, previous.value, previous.seconds)
        : null,
    };
  });
}

/** Fold a distribution family's rows into one group per label set (le/quantile stripped). */
export function groupHistogram(rows: ResolvedRow[]): HistogramGroup[] {
  const groups = new Map<string, HistogramGroup>();
  rows.forEach((row) => {
    const labels = stripBucketLabels(row.labels);
    const key = labelsText(labels);
    let group = groups.get(key);
    if (!group) {
      group = { key, labels, count: null, sum: null, buckets: [], quantiles: [] };
      groups.set(key, group);
    }
    if (row.metric.endsWith("_bucket")) {
      group.buckets.push({ le: row.labels.le || "?", value: row.value });
    } else if (row.metric.endsWith("_count")) {
      group.count = row;
    } else if (row.metric.endsWith("_sum")) {
      group.sum = row;
    } else {
      group.quantiles.push({ q: row.labels.quantile || "?", value: row.value });
    }
  });
  const list = [...groups.values()];
  list.forEach((group) => group.buckets.sort((left, right) => bucketRank(left.le) - bucketRank(right.le)));
  list.sort((left, right) => left.key.localeCompare(right.key));
  return list;
}

/** Histogram/summary components are cumulative, so their trends use counter (rate) semantics. */
function rowType(type: MetricFamily["type"], metric: string): MetricFamily["type"] {
  if (
    (type === "histogram" || type === "summary") &&
    (metric.endsWith("_count") || metric.endsWith("_sum") || metric.endsWith("_bucket"))
  ) {
    return "counter";
  }
  return type;
}

export function deltaText(
  type: MetricFamily["type"],
  current: number | null,
  previous: number | null,
  seconds: number,
): string | null {
  if (current === null || previous === null || seconds <= 0) {
    return null;
  }
  if (type === "counter") {
    const rate = Math.max(0, (current - previous) / seconds);
    return rate > 0.0005 ? `▲ +${formatRate(rate)}/s` : null;
  }
  const diff = current - previous;
  if (Math.abs(diff) < 1e-9) {
    return null;
  }
  return `${diff > 0 ? "▲" : "▼"} ${formatRate(Math.abs(diff))}`;
}

function formatRate(value: number) {
  if (value >= 100) {
    return Math.round(value).toString();
  }
  if (value >= 1) {
    return value.toFixed(1);
  }
  return value.toFixed(2);
}
