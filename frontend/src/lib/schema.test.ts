import { describe, expect, it } from "vitest";
import {
  deltaText,
  familySummary,
  groupHistogram,
  inferUnit,
  isRuntimeFamily,
  resolveRows,
  stripBucketLabels,
  type ResolvedRow,
} from "./schema";
import type { MetricFamily } from "../types";

describe("inferUnit", () => {
  it("maps known suffixes to display units", () => {
    expect(inferUnit("request_duration_seconds")).toBe("s");
    expect(inferUnit("heap_bytes")).toBe("bytes");
    expect(inferUnit("cpu_ratio")).toBe("ratio");
  });

  it("looks through a _total suffix to the unit beneath", () => {
    expect(inferUnit("sent_bytes_total")).toBe("bytes");
  });

  it("returns null when no suffix matches", () => {
    expect(inferUnit("http_requests")).toBeNull();
  });
});

describe("stripBucketLabels", () => {
  it("removes le and quantile but keeps the rest", () => {
    expect(stripBucketLabels({ le: "0.5", quantile: "0.9", path: "/x" })).toEqual({ path: "/x" });
  });
});

describe("isRuntimeFamily", () => {
  it("flags built-in runtime/exporter prefixes", () => {
    expect(isRuntimeFamily("go_goroutines")).toBe(true);
    expect(isRuntimeFamily("process_cpu_seconds_total")).toBe(true);
  });

  it("leaves application metrics alone", () => {
    expect(isRuntimeFamily("orders_total")).toBe(false);
  });
});

describe("familySummary", () => {
  it("treats a single unlabeled non-distribution sample as a scalar", () => {
    const family: MetricFamily = {
      name: "up",
      type: "gauge",
      samples: [{ metric: "up", labels: {}, value: 1 }],
    };
    const summary = familySummary(family);
    expect(summary.scalar?.value).toBe(1);
    expect(summary.seriesCount).toBe(1);
  });

  it("never treats a distribution as a scalar and excludes le/quantile from label keys", () => {
    const family: MetricFamily = {
      name: "lat",
      type: "histogram",
      samples: [
        { metric: "lat_bucket", labels: { le: "0.5", path: "/a" }, value: 1 },
        { metric: "lat_bucket", labels: { le: "1", path: "/a" }, value: 2 },
      ],
    };
    const summary = familySummary(family);
    expect(summary.scalar).toBeNull();
    expect(summary.labelKeys).toEqual(["path"]);
    // Both samples share one series once le is stripped.
    expect(summary.seriesCount).toBe(1);
    expect(summary.unit).toBeNull();
  });
});

describe("groupHistogram", () => {
  it("folds rows into one group per label set with buckets sorted by bound", () => {
    const rows: ResolvedRow[] = [
      { metric: "lat_bucket", labels: { le: "+Inf", path: "/a" }, value: 9, delta: null },
      { metric: "lat_bucket", labels: { le: "0.5", path: "/a" }, value: 5, delta: null },
      { metric: "lat_count", labels: { path: "/a" }, value: 9, delta: null },
      { metric: "lat_sum", labels: { path: "/a" }, value: 4, delta: null },
    ];
    const [group, ...rest] = groupHistogram(rows);
    expect(rest).toHaveLength(0);
    expect(group?.labels).toEqual({ path: "/a" });
    expect(group?.buckets.map((b) => b.le)).toEqual(["0.5", "+Inf"]);
    expect(group?.count?.value).toBe(9);
    expect(group?.sum?.value).toBe(4);
  });

  it("routes summary quantiles into the quantiles list", () => {
    const rows: ResolvedRow[] = [{ metric: "rpc", labels: { quantile: "0.99" }, value: 0.2, delta: null }];
    const [group] = groupHistogram(rows);
    expect(group?.quantiles).toEqual([{ q: "0.99", value: 0.2 }]);
  });
});

describe("deltaText", () => {
  it("shows a positive per-second rate for counters above the noise floor", () => {
    expect(deltaText("counter", 100, 0, 10)).toBe("▲ +10.0/s");
  });

  it("suppresses counter rates at or below the noise floor", () => {
    expect(deltaText("counter", 0.001, 0, 10)).toBeNull();
  });

  it("never shows a negative counter rate (treated as a reset)", () => {
    expect(deltaText("counter", 5, 100, 10)).toBeNull();
  });

  it("shows signed absolute change for gauges", () => {
    expect(deltaText("gauge", 12, 10, 60)).toBe("▲ 2.0");
    expect(deltaText("gauge", 8, 10, 60)).toBe("▼ 2.0");
  });

  it("returns null for no meaningful change or invalid windows", () => {
    expect(deltaText("gauge", 10, 10, 60)).toBeNull();
    expect(deltaText("gauge", 10, null, 60)).toBeNull();
    expect(deltaText("counter", 10, 0, 0)).toBeNull();
  });
});

describe("resolveRows (live)", () => {
  it("pairs each sample's current value with a history-based delta", () => {
    const family: MetricFamily = {
      name: "orders_total",
      type: "counter",
      samples: [{ metric: "orders_total", labels: { shop: "a" }, value: 100 }],
    };
    const rows = resolveRows(family, null, () => ({ value: 0, seconds: 10 }));
    expect(rows[0]?.value).toBe(100);
    expect(rows[0]?.delta).toBe("▲ +10.0/s");
  });

  it("omits the delta when there is no history yet", () => {
    const family: MetricFamily = {
      name: "q",
      type: "gauge",
      samples: [{ metric: "q", labels: {}, value: 3 }],
    };
    const rows = resolveRows(family, null, () => null);
    expect(rows[0]).toEqual({ metric: "q", labels: {}, value: 3, delta: null });
  });
});
