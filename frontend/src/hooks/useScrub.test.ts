import { useState } from "react";
import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { clamp, useScrub, type ScrubPosition } from "./useScrub";
import { fetchSeriesByMetric } from "../api";
import type { Series } from "../types";

vi.mock("../api", () => ({ fetchSeriesByMetric: vi.fn() }));
const fetchMock = vi.mocked(fetchSeriesByMetric);

beforeEach(() => {
  fetchMock.mockResolvedValue({});
});

afterEach(() => {
  vi.clearAllMocks();
});

/** Drives useScrub with the position state that App normally owns. */
function useScrubHarness(metrics: string[], onResume: () => void) {
  const [position, setPosition] = useState<ScrubPosition | null>(null);
  return useScrub("target-1", metrics, onResume, position, setPosition);
}

describe("clamp", () => {
  it("bounds a timestamp to the domain", () => {
    expect(clamp(50, [0, 100])).toBe(50);
    expect(clamp(-10, [0, 100])).toBe(0);
    expect(clamp(999, [0, 100])).toBe(100);
  });
});

describe("useScrub", () => {
  it("starts live and enters scrub on begin, freezing the entry domain", () => {
    const { result } = renderHook(() => useScrubHarness(["m"], () => {}));
    expect(result.current.mode).toBe("live");

    act(() => {
      result.current.begin(5_500, [0, 5_000]);
    });

    expect(result.current.mode).toBe("scrub");
    expect(result.current.domain).toEqual([0, 5_000]);
    // t is clamped into the frozen live domain.
    expect(result.current.t).toBe(5_000);
  });

  it("keeps the frozen domain on a second begin and clamps within it", () => {
    const { result } = renderHook(() => useScrubHarness(["m"], () => {}));
    act(() => {
      result.current.begin(2_000, [0, 5_000]);
    });
    act(() => {
      // A later live domain must not slide the track; the original domain holds.
      result.current.begin(9_999, [1_000, 9_000]);
    });
    expect(result.current.domain).toEqual([0, 5_000]);
    expect(result.current.t).toBe(5_000);
  });

  it("loads the all-metric series once per scrub session", async () => {
    const loaded: Record<string, Series[]> = { m: [] };
    fetchMock.mockResolvedValue(loaded);

    const { result } = renderHook(() => useScrubHarness(["m", "n"], () => {}));
    await act(async () => {
      result.current.begin(1_000, [0, 5_000]);
      await Promise.resolve(); // let the scrub-load effect's fetch settle
    });

    expect(result.current.series).toBe(loaded);
    expect(result.current.loading).toBe(false);
    expect(fetchMock).toHaveBeenCalledExactlyOnceWith("target-1", ["m", "n"]);
  });

  it("goLive resets the session and notifies the caller", async () => {
    fetchMock.mockResolvedValue({ m: [] });
    const onResume = vi.fn();
    const { result } = renderHook(() => useScrubHarness(["m"], onResume));

    await act(async () => {
      result.current.begin(1_000, [0, 5_000]);
      await Promise.resolve(); // let the scrub-load effect's fetch settle
    });
    expect(result.current.series).not.toBeNull();

    act(() => {
      result.current.goLive();
    });

    expect(result.current.mode).toBe("live");
    expect(result.current.t).toBeNull();
    expect(result.current.series).toBeNull();
    expect(onResume).toHaveBeenCalledOnce();
  });
});
