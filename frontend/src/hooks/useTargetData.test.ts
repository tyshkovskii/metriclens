import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useTargetData } from "./useTargetData";
import { loadTargetData } from "../api";
import type { Target, TargetData } from "../types";

vi.mock("../api", () => ({ loadTargetData: vi.fn() }));
const loadMock = vi.mocked(loadTargetData);

const target: Target = {
  id: "t",
  serviceName: "svc",
  containerName: "c",
  status: "up",
  discoveredAt: "2024-01-01T00:00:00Z",
};

/** A metrics payload carrying a single counter sample at `value`. */
function withValue(value: number): TargetData {
  return {
    panels: [],
    issues: [],
    metrics: {
      target,
      families: [
        { name: "orders_total", type: "counter", samples: [{ metric: "orders_total", labels: {}, value }] },
      ],
    },
  };
}

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
  vi.clearAllMocks();
});

describe("useTargetData", () => {
  // A long poll interval keeps the real setInterval from firing during the test;
  // it is cleared on unmount. Second loads are driven explicitly via refresh().
  const POLL = 1_000_000;

  it("loads on mount and exposes data plus a last-updated time", async () => {
    loadMock.mockResolvedValue(withValue(10));
    const pausedRef = { current: false };

    const { result } = renderHook(() => useTargetData("t", pausedRef, POLL));

    await waitFor(() => {
      expect(result.current.lastUpdated).toBeInstanceOf(Date);
    });
    expect(loadMock).toHaveBeenCalledOnce();
    expect(result.current.data.metrics?.families[0]?.samples[0]?.value).toBe(10);
  });

  it("surfaces a degraded payload without throwing", async () => {
    loadMock.mockResolvedValue({ panels: [], issues: [], error: "backend down" });
    const pausedRef = { current: false };

    const { result } = renderHook(() => useTargetData("t", pausedRef, POLL));

    await waitFor(() => {
      expect(result.current.data.error).toBe("backend down");
    });
  });

  it("refresh() forces a load even while paused", async () => {
    loadMock.mockResolvedValue(withValue(10));
    const pausedRef = { current: true };

    const { result } = renderHook(() => useTargetData("t", pausedRef, POLL));
    // The mount's load(true) is forced, so it runs despite paused.
    await waitFor(() => {
      expect(loadMock).toHaveBeenCalledOnce();
    });
    loadMock.mockClear();

    act(() => {
      result.current.refresh();
    });
    await waitFor(() => {
      expect(loadMock).toHaveBeenCalledOnce();
    });
  });

  it("reports a previous value once two snapshots straddle the minimum delta window", async () => {
    // Fake ONLY Date so history timestamps are deterministic; performance.now and
    // setTimeout stay real, which React's scheduler and waitFor need.
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(0);
    let calls = 0;
    loadMock.mockImplementation(() => Promise.resolve(withValue(++calls * 10)));
    const pausedRef = { current: false };

    const { result } = renderHook(() => useTargetData("t", pausedRef, POLL));
    await waitFor(() => {
      expect(result.current.lastUpdated).toBeInstanceOf(Date);
    });
    // Only one snapshot so far — no previous value to compare against.
    expect(result.current.previousValue("orders_total")).toBeNull();

    vi.setSystemTime(30_000); // 30s later
    act(() => {
      result.current.refresh();
    });

    await waitFor(() => {
      const previous = result.current.previousValue("orders_total");
      expect(previous).not.toBeNull();
      expect(previous?.value).toBe(10);
      expect(previous?.seconds).toBe(30);
    });
  });
});
