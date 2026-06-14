import { useCallback, useEffect, useRef, useState } from "react";
import { loadTargetData } from "../api";
import { sampleKey } from "../lib/format";
import type { TargetData } from "../types";

const HISTORY_MS = 70_000;
const DELTA_TARGET_MS = 55_000;
const DELTA_MIN_MS = 15_000;

const EMPTY: TargetData = { panels: [], issues: [] };

type Snapshot = {
  at: number;
  values: Map<string, number>;
};

export type PreviousValue = {
  value: number;
  seconds: number;
};

/**
 * Polls metrics/quality for a target every `pollMs` (the backend's scrape interval).
 * Skips ticks while `pausedRef.current` is true (time scrubbing).
 */
export function useTargetData(targetId: string, pausedRef: React.RefObject<boolean>, pollMs: number) {
  const [data, setData] = useState<TargetData>(EMPTY);
  const [lastUpdated, setLastUpdated] = useState<Date | null>(null);
  const historyRef = useRef<Snapshot[]>([]);
  const loadRef = useRef<(force?: boolean) => void>(() => {});

  useEffect(() => {
    let cancelled = false;

    async function load(force = false) {
      if (!force && pausedRef.current) {
        return;
      }
      const next = await loadTargetData(targetId);
      if (cancelled) {
        return;
      }
      setData(next);
      setLastUpdated(new Date());
      if (next.metrics) {
        const values = new Map<string, number>();
        next.metrics.families.forEach((family) => {
          family.samples.forEach((sample) =>
            values.set(sampleKey(sample.metric, sample.labels), sample.value),
          );
        });
        const history = historyRef.current;
        history.push({ at: Date.now(), values });
        const cutoff = Date.now() - HISTORY_MS;
        let oldest = history[0];
        while (oldest && oldest.at < cutoff) {
          history.shift();
          oldest = history[0];
        }
      }
    }

    loadRef.current = (force) => void load(force);
    void load(true);
    const timer = window.setInterval(load, pollMs);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [targetId, pausedRef, pollMs]);

  const refresh = useCallback(() => loadRef.current(true), []);

  /** Value of a sample ~60s ago, for trend deltas. Null when not enough history. */
  const previousValue = useCallback((key: string): PreviousValue | null => {
    const history = historyRef.current;
    const latest = history[history.length - 1];
    if (history.length < 2 || !latest) {
      return null;
    }
    const now = latest.at;
    let snapshot: Snapshot | null = null;
    for (const candidate of history) {
      if (now - candidate.at >= DELTA_TARGET_MS) {
        snapshot = candidate;
      } else {
        break;
      }
    }
    snapshot ??= history[0] ?? null;
    if (!snapshot || now - snapshot.at < DELTA_MIN_MS) {
      return null;
    }
    const value = snapshot.values.get(key);
    if (value === undefined) {
      return null;
    }
    return { value, seconds: (now - snapshot.at) / 1000 };
  }, []);

  return { data, lastUpdated, refresh, previousValue };
}
