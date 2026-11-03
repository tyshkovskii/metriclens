import { describe, expect, it } from "vitest";
import { readMetricLocation } from "./location";

const read = (url: string) => readMetricLocation(new URL(url, "http://localhost:9999"));

describe("metric links", () => {
  it("retains existing encoded target hashes", () => {
    expect(read("/#api%2Fone").targetId).toBe("api/one");
    expect(read("/").targetId).toBeNull();
  });

  it("selects the linked target and metric with an exact frozen RFC3339 window", () => {
    const start = "2026-11-02T18:00:00.123Z";
    const end = "2026-11-02T18:01:00.456Z";
    const route = read(
      `/?${new URLSearchParams({ target: "api/one", metric: "requests_total", start, end })}`,
    );
    expect(route).toEqual({
      targetId: "api/one",
      metric: "requests_total",
      error: null,
      position: { t: Date.parse(end), domain: [Date.parse(start), Date.parse(end)] },
    });
  });

  it.each([
    "?start=bad&end=2026-11-02T18:01:00Z",
    "?start=2026-11-02T18:00:00Z",
    "?start=2026-11-02T18:01:00Z&end=2026-11-02T18:00:00Z",
    "?start=2026-11-02T18:00:00Z&end=2026-11-02T18:00:00Z",
    "?start=1793642400&end=1793642460",
  ])("reports invalid windows rather than guessing timestamps: %s", (query) => {
    expect(read(`/${query}`).error).toMatch(/Invalid time window/);
    expect(read(`/${query}`).position).toBeNull();
  });

  it("reports malformed hashes and ambiguous metric-only links", () => {
    expect(read("/#%E0%A4%A").error).toBe("Invalid target link.");
    expect(read("/?metric=requests_total").error).toBe("Metric links must identify a target.");
  });
});
