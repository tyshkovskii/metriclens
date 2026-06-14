import { useCallback } from "react";
import { useLiveDomain } from "./useLiveDomain";
import { clamp, useScrub } from "./useScrub";
import type { ScrubPosition } from "./useScrub";
import type { Series } from "../types";

/** One scrub step for the arrow keys and the timeline back/forward buttons. */
const NUDGE_MS = 5000;
/**
 * Releasing the thumb (or stepping) within this distance of the right edge
 * counts as "go live" rather than pinning to a near-now timestamp that would
 * immediately go stale.
 */
const LIVE_SNAP_MS = 2500;

export type TimelineControls = {
  /** True while paused on a past timestamp. */
  scrubbing: boolean;
  loading: boolean;
  /** Selected timestamp while scrubbing; null when live. */
  t: number | null;
  /** Scrubber track domain: a frozen span while scrubbing, the live window otherwise. */
  domain: [number, number];
  /** Domain charts render against. */
  chartDomain: [number, number];
  /** All-metric series cache for the current scrub session; null until loaded. */
  series: Record<string, Series[]> | null;
  /** Jump to an absolute timestamp, snapping back to live near the right edge. */
  scrubTo: (t: number) => void;
  /** Step the selection by one NUDGE_MS in `direction` (-1 = past, 1 = future). */
  nudge: (direction: number) => void;
  goLive: () => void;
};

/**
 * Timeline interaction policy for one target. Composes the live window
 * (useLiveDomain) and scrub state (useScrub), and owns the snap-to-live and
 * nudge rules so the "near the right edge means live" decision lives in one
 * place instead of being repeated at each call site.
 */
export function useTimelineControls(
  targetId: string,
  retentionMs: number,
  metricNames: string[],
  onResume: () => void,
  position: ScrubPosition | null,
  setPosition: React.Dispatch<React.SetStateAction<ScrubPosition | null>>,
): TimelineControls {
  const liveDomain = useLiveDomain(retentionMs);
  const scrub = useScrub(targetId, metricNames, onResume, position, setPosition);

  const scrubbing = scrub.mode === "scrub";
  const domain = scrub.domain ?? liveDomain;

  const scrubTo = useCallback(
    (t: number) => {
      if (scrub.mode === "scrub" && isNearLiveEdge(t, domain)) {
        scrub.goLive();
        return;
      }
      if (scrub.mode === "live" && t >= domain[1] - 1000) {
        return;
      }
      scrub.begin(t, domain);
    },
    [scrub, domain],
  );

  const nudge = useCallback(
    (direction: number) => {
      const step = direction * NUDGE_MS;
      if (step > 0 && position && isNearLiveEdge(position.t + step, position.domain)) {
        scrub.goLive();
        return;
      }
      setPosition((current) => {
        if (current) {
          return { ...current, t: clamp(current.t + step, current.domain) };
        }
        if (step > 0) {
          return current;
        }
        return { t: clamp(liveDomain[1] + step, liveDomain), domain: liveDomain };
      });
    },
    [liveDomain, position, scrub, setPosition],
  );

  return {
    scrubbing,
    loading: scrub.loading,
    t: scrub.t,
    domain,
    chartDomain: scrubbing ? domain : liveDomain,
    series: scrub.series,
    scrubTo,
    nudge,
    goLive: scrub.goLive,
  };
}

/**
 * Distance from the right edge that counts as "live": the larger of a floor and
 * ~2% of the visible span, so "drag to the end" lands within a comfortable slice
 * of the track instead of a couple of pixels.
 */
export function liveSnap(domain: [number, number]): number {
  return Math.max(LIVE_SNAP_MS, (domain[1] - domain[0]) * 0.02);
}

/** True when `t` falls inside the snap zone at the right (live) edge of `domain`. */
export function isNearLiveEdge(t: number, domain: [number, number]): boolean {
  return t >= domain[1] - liveSnap(domain);
}
