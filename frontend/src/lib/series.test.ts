import { describe, expect, it } from "vitest";
import {
  bucketRank,
  buildRows,
  chartKind,
  chartKindForMetric,
  chartMetric,
  nameSeries,
  perSeriesRates,
  transformSeries,
  valueAt,
} from "./series";
import type { MetricFamily, Series, SeriesPoint, SuggestedPanel } from "../types";

/** Build a point from an epoch-ms timestamp (series timestamps are ISO strings). */
const p = (ms: number, value: number): SeriesPoint => ({ ts: new Date(ms).toISOString(), value });

const series = (labels: Record<string, string>, points: SeriesPoint[]): Series => ({
  targetId: "t",
  metric: "m",
  labels,
  points,
});

describe("valueAt", () => {
  const points = [p(1000, 10), p(2000, 20), p(3000, 30)];

  it("returns null for an empty series", () => {
    expect(valueAt([], 1500)).toBeNull();
  });

  it("returns null when t precedes the first point", () => {
    expect(valueAt(points, 999)).toBeNull();
  });

  it("returns the exact value on a timestamp hit", () => {
    expect(valueAt(points, 2000)).toBe(20);
  });

  it("returns the last value at or before t between points", () => {
    expect(valueAt(points, 2500)).toBe(20);
  });

  it("returns the final value for t past the end", () => {
    expect(valueAt(points, 99_999)).toBe(30);
  });

  it("handles a single-point series", () => {
    expect(valueAt([p(1000, 7)], 999)).toBeNull();
    expect(valueAt([p(1000, 7)], 1000)).toBe(7);
  });
});

describe("chartKind", () => {
  it("maps gauge families to a gauge", () => {
    expect(chartKind("temp", "gauge")).toBe("gauge");
  });

  it("rates a summary's _count stream but gauges its quantiles", () => {
    expect(chartKind("rpc_count", "summary")).toBe("counter_rate");
    expect(chartKind("rpc", "summary")).toBe("gauge");
  });

  it("treats counters and histograms as cumulative rates", () => {
    expect(chartKind("x", "counter")).toBe("counter_rate");
    expect(chartKind("x", "histogram")).toBe("counter_rate");
  });

  it("infers a rate from _total/_count suffixes when the type is unknown", () => {
    expect(chartKind("http_requests_total", undefined)).toBe("counter_rate");
    expect(chartKind("events_count", undefined)).toBe("counter_rate");
  });

  it("defaults an unsuffixed, untyped metric to a gauge", () => {
    expect(chartKind("queue_depth", undefined)).toBe("gauge");
  });
});

describe("chartKindForMetric", () => {
  const panel = (metric: string, kind: SuggestedPanel["kind"]): SuggestedPanel => ({
    id: `${metric}:${kind}`,
    title: metric,
    kind,
    metric,
    confidence: 1,
    reason: "",
  });

  it("lets a matching panel override the fallback inference", () => {
    // Without the panel, an untyped "latency" gauges; the panel forces a rate.
    expect(chartKindForMetric("latency", undefined, [panel("latency", "http_rate")])).toBe("counter_rate");
  });

  it("ignores panels naming a different metric", () => {
    expect(chartKindForMetric("latency", undefined, [panel("other", "counter_rate")])).toBe("gauge");
  });

  it("ignores panels whose kind has no chart mapping", () => {
    expect(chartKindForMetric("latency", "gauge", [panel("latency", "histogram_latency")])).toBe("gauge");
  });
});

describe("chartMetric", () => {
  const family = (over: Partial<MetricFamily>): MetricFamily => ({
    name: "thing",
    type: "gauge",
    samples: [],
    ...over,
  });

  it("plots a histogram's _count throughput", () => {
    expect(chartMetric(family({ name: "lat", type: "histogram" }))).toBe("lat_count");
  });

  it("plots a summary's quantiles when present, else its _count", () => {
    const withQuantiles = family({
      name: "rpc",
      type: "summary",
      samples: [{ metric: "rpc", labels: { quantile: "0.9" }, value: 1 }],
    });
    expect(chartMetric(withQuantiles)).toBe("rpc");

    const countOnly = family({
      name: "rpc",
      type: "summary",
      samples: [{ metric: "rpc_count", labels: {}, value: 1 }],
    });
    expect(chartMetric(countOnly)).toBe("rpc_count");
  });

  it("plots the first raw sample for plain families", () => {
    expect(chartMetric(family({ name: "up", samples: [{ metric: "up", labels: {}, value: 1 }] }))).toBe("up");
    // Falls back to the family name when there are no samples.
    expect(chartMetric(family({ name: "up", samples: [] }))).toBe("up");
  });
});

describe("perSeriesRates / transformSeries", () => {
  it("passes gauge series through untouched", () => {
    const input = [series({}, [p(1000, 5), p(2000, 9)])];
    expect(transformSeries("gauge", input)).toBe(input);
  });

  it("computes per-second deltas between consecutive points", () => {
    const [rated] = perSeriesRates([series({}, [p(0, 0), p(2000, 10), p(4000, 30)])]);
    expect(rated?.points).toEqual([p(2000, 5), p(4000, 10)]);
  });

  it("treats a counter decrease as a reset and rebases on the new value", () => {
    // 100 -> 5 over 1s: a reset, so the rate is the post-reset value (5/s),
    // not the negative delta.
    const [rated] = perSeriesRates([series({}, [p(0, 100), p(1000, 5)])]);
    expect(rated?.points).toEqual([p(1000, 5)]);
  });

  it("skips intervals with non-positive elapsed time", () => {
    const [rated] = perSeriesRates([series({}, [p(1000, 0), p(1000, 10)])]);
    expect(rated?.points).toEqual([]);
  });

  it("yields no points for a single-sample series", () => {
    const [rated] = perSeriesRates([series({}, [p(1000, 1)])]);
    expect(rated?.points).toEqual([]);
  });
});

describe("nameSeries", () => {
  it("drops empty series and returns [] when nothing remains", () => {
    expect(nameSeries([series({ a: "1" }, [])])).toEqual([]);
  });

  it("names a lone series from its full label set", () => {
    const named = nameSeries([series({ path: "/x" }, [p(0, 1)])]);
    expect(named).toEqual([{ name: '{path="/x"}', points: [p(0, 1)] }]);
  });

  it("names a lone unlabeled series 'value'", () => {
    expect(nameSeries([series({}, [p(0, 1)])])).toEqual([{ name: "value", points: [p(0, 1)] }]);
  });

  it("legends by the most discriminating label key", () => {
    // `code` varies (200/500); `job` is constant — so names come from `code`.
    const named = nameSeries([
      series({ job: "api", code: "200" }, [p(0, 1)]),
      series({ job: "api", code: "500" }, [p(0, 2)]),
    ]);
    expect(named.map((n) => n.name).sort()).toEqual(["200", "500"]);
  });

  it("merges series sharing the chosen legend name by summing points", () => {
    // `region` is the discriminating key; the two us-series collide on it and
    // are summed, while eu stays separate.
    const named = nameSeries([
      series({ region: "us" }, [p(0, 1), p(1000, 2)]),
      series({ region: "us" }, [p(0, 10), p(1000, 20)]),
      series({ region: "eu" }, [p(0, 5)]),
    ]);
    expect(named.find((n) => n.name === "us")?.points).toEqual([p(0, 11), p(1000, 22)]);
    expect(named.find((n) => n.name === "eu")?.points).toEqual([p(0, 5)]);
  });

  it("folds everything past the top 8 into an 'other' bucket", () => {
    const inputs = Array.from({ length: 10 }, (_, i) => series({ id: `s${i}` }, [p(0, i)]));
    const named = nameSeries(inputs);
    expect(named).toHaveLength(8);
    expect(named.some((n) => n.name === "other")).toBe(true);
  });
});

describe("buildRows", () => {
  it("unions timestamps and fills gaps with the fill value", () => {
    const rows = buildRows(
      [
        { name: "a", points: [p(0, 1), p(2000, 3)] },
        { name: "b", points: [p(1000, 5)] },
      ],
      0,
    );
    expect(rows).toEqual([
      { ts: 0, a: 1, b: 0 },
      { ts: 1000, a: 0, b: 5 },
      { ts: 2000, a: 3, b: 0 },
    ]);
  });

  it("fills gaps with null for line charts", () => {
    const rows = buildRows(
      [
        { name: "a", points: [p(0, 1)] },
        { name: "b", points: [p(1000, 5)] },
      ],
      null,
    );
    expect(rows).toEqual([
      { ts: 0, a: 1, b: null },
      { ts: 1000, a: null, b: 5 },
    ]);
  });
});

describe("bucketRank", () => {
  it("orders numeric bucket bounds and sinks +Inf and junk to the top", () => {
    expect(bucketRank("0.5")).toBe(0.5);
    expect(bucketRank("+Inf")).toBe(Number.POSITIVE_INFINITY);
    expect(bucketRank("nonsense")).toBe(Number.POSITIVE_INFINITY);
    expect(bucketRank(undefined)).toBe(Number.POSITIVE_INFINITY);
  });
});
