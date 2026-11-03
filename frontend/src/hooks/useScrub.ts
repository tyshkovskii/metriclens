import { useCallback, useEffect, useState } from "react";
import { fetchSeriesByMetric } from "../api";
import type { Series } from "../types";

export type ScrubPosition = {
  /** Selected timestamp (epoch ms). */
  t: number;
  /** Domain frozen at scrub entry so the track doesn't slide under the thumb. */
  domain: [number, number];
};

/**
 * Live/scrub view for one target. The position lives in App and is shared by
 * every target, so switching tabs keeps the timeline where it was. The series
 * cache is per mount (key the component by target id) and loads once per
 * discovered metric set in a scrub session — including when this target mounts mid-scrub after a tab
 * switch, where `begin` never runs.
 */
export function useScrub(
  targetId: string,
  metricNames: string[],
  onResume: () => void,
  position: ScrubPosition | null,
  setPosition: React.Dispatch<React.SetStateAction<ScrubPosition | null>>,
) {
  const [series, setSeries] = useState<Record<string, Series[]> | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const begin = useCallback(
    (t: number, liveDomain: [number, number]) => {
      setPosition((current) =>
        current
          ? { ...current, t: clamp(t, current.domain) }
          : { t: clamp(t, liveDomain), domain: liveDomain },
      );
    },
    [setPosition],
  );

  const scrubbing = position !== null;
  const namesKey = metricNames.join(" ");
  useEffect(() => {
    if (!scrubbing || !namesKey) {
      return;
    }
    // eslint-disable-next-line react-hooks/set-state-in-effect -- synchronize loading with a new external history request
    setLoading(true);
    let cancelled = false;
    void fetchSeriesByMetric(targetId, namesKey.split(" ")).then(
      (result) => {
        if (!cancelled) {
          setSeries(result);
          setLoading(false);
          setError(null);
        }
      },
      (cause: unknown) => {
        if (!cancelled) {
          setLoading(false);
          setError(cause instanceof Error ? cause.message : "History request failed.");
        }
      },
    );
    return () => {
      cancelled = true;
    };
  }, [scrubbing, namesKey, targetId]);

  const goLive = useCallback(() => {
    setPosition(null);
    setSeries(null);
    setLoading(false);
    setError(null);
    onResume();
  }, [onResume, setPosition]);

  return {
    mode: scrubbing ? "scrub" : "live",
    t: position?.t ?? null,
    domain: position?.domain ?? null,
    series,
    loading,
    error,
    begin,
    goLive,
  };
}

export function clamp(t: number, domain: [number, number]): number {
  return Math.min(Math.max(t, domain[0]), domain[1]);
}
