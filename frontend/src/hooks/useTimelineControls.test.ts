import { describe, expect, it } from "vitest";
import { isNearLiveEdge, liveSnap } from "./useTimelineControls";

// The snap zone is max(2500ms floor, 2% of the visible span). Small windows are
// governed by the floor; large windows by the 2% term. These spans bracket the
// crossover at 125_000ms (2% == 2500ms).
const SMALL_SPAN = 60_000; // 2% = 1200ms < floor, so snap = 2500ms
const LARGE_SPAN = 600_000; // 2% = 12_000ms > floor, so snap = 12_000ms

describe("liveSnap", () => {
  it("uses the 2500ms floor when 2% of the span is smaller", () => {
    expect(liveSnap([0, SMALL_SPAN])).toBe(2500);
  });

  it("uses 2% of the span when that exceeds the floor", () => {
    expect(liveSnap([0, LARGE_SPAN])).toBe(12_000);
  });

  it("switches from floor to 2% exactly at a 125s span", () => {
    expect(liveSnap([0, 125_000])).toBe(2500);
    expect(liveSnap([0, 125_001])).toBeCloseTo(2500.02, 2);
  });

  it("ignores the domain's absolute offset, depending only on the span", () => {
    const base = 1_700_000_000_000;
    expect(liveSnap([base, base + LARGE_SPAN])).toBe(liveSnap([0, LARGE_SPAN]));
  });
});

describe("isNearLiveEdge", () => {
  it("treats the right edge itself as live", () => {
    expect(isNearLiveEdge(SMALL_SPAN, [0, SMALL_SPAN])).toBe(true);
  });

  it("is exclusive at the inner boundary of the snap zone", () => {
    const domain: [number, number] = [0, SMALL_SPAN];
    const boundary = domain[1] - liveSnap(domain); // 57_500
    expect(isNearLiveEdge(boundary, domain)).toBe(true); // `>=`, so on-boundary snaps
    expect(isNearLiveEdge(boundary - 1, domain)).toBe(false); // a hair earlier stays scrubbed
  });

  it("uses the wider 2% zone for large spans", () => {
    const domain: [number, number] = [0, LARGE_SPAN];
    // 590_000 is 10_000ms from the edge: inside the 12_000ms zone for a large
    // span, but would be far outside the 2500ms floor of a small one.
    expect(isNearLiveEdge(590_000, domain)).toBe(true);
    expect(isNearLiveEdge(587_999, domain)).toBe(false); // just past the 12_000ms zone
  });

  it("never snaps a timestamp well inside the window", () => {
    expect(isNearLiveEdge(LARGE_SPAN / 2, [0, LARGE_SPAN])).toBe(false);
  });
});
