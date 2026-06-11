import { useCallback, useEffect, useRef, useState } from "react";
import { fetchSeries } from "../api";
import type { Series } from "../types";

export type ScrubPosition = {
  /** Selected timestamp (epoch ms). */
  t: number;
  /** Domain frozen at scrub entry so the track doesn't slide under the thumb. */
  domain: [number, number];
};

export type Scrub = {
  mode: "live" | "scrub";
  /** Selected timestamp (epoch ms) while scrubbing. */
  t: number | null;
  domain: [number, number] | null;
  /** All-metric series cache, keyed by sample metric name. Null until loaded. */
  series: Record<string, Series[]> | null;
  loading: boolean;
  begin: (t: number, liveDomain: [number, number]) => void;
  goLive: () => void;
};

/**
 * Live/scrub view for one target. The position lives in App and is shared by
 * every target, so switching tabs keeps the timeline where it was. The series
 * cache is per mount (key the component by target id) and loads once per
 * scrub session — including when this target mounts mid-scrub after a tab
 * switch, where `begin` never runs.
 */
export function useScrub(
  targetId: string,
  metricNames: string[],
  onResume: () => void,
  position: ScrubPosition | null,
  setPosition: React.Dispatch<React.SetStateAction<ScrubPosition | null>>,
): Scrub {
  const [series, setSeries] = useState<Record<string, Series[]> | null>(null);
  const [loading, setLoading] = useState(false);
  const fetchedRef = useRef(false);

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
    if (!scrubbing || fetchedRef.current || !namesKey) {
      return;
    }
    fetchedRef.current = true;
    setLoading(true);
    let cancelled = false;
    void Promise.all(
      namesKey.split(" ").map((name) =>
        fetchSeries(targetId, name).then(
          (result) => [name, result] as const,
          () => [name, [] as Series[]] as const,
        ),
      ),
    ).then((entries) => {
      if (!cancelled) {
        setSeries(Object.fromEntries(entries));
        setLoading(false);
      }
    });
    return () => {
      cancelled = true;
    };
  }, [scrubbing, namesKey, targetId]);

  const goLive = useCallback(() => {
    setPosition(null);
    setSeries(null);
    setLoading(false);
    fetchedRef.current = false;
    onResume();
  }, [onResume, setPosition]);

  return {
    mode: scrubbing ? "scrub" : "live",
    t: position?.t ?? null,
    domain: position?.domain ?? null,
    series,
    loading,
    begin,
    goLive,
  };
}

function clamp(t: number, domain: [number, number]): number {
  return Math.min(Math.max(t, domain[0]), domain[1]);
}
