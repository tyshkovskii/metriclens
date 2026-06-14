import { useEffect, useState } from "react";
import { fetchSeriesByMetric } from "../api";
import type { Series } from "../types";

/**
 * Polls history for the watched metrics (expanded families and pinned
 * charts), keyed by metric name. Skips ticks while `pausedRef.current` is
 * true (time scrubbing).
 */
export function useWatchedSeries(
  targetId: string,
  metrics: string[],
  pausedRef: React.RefObject<boolean>,
  scrubbing: boolean,
  pollMs: number,
): Record<string, Series[]> {
  const [seriesByMetric, setSeriesByMetric] = useState<Record<string, Series[]>>({});
  const metricsKey = metrics.join(" ");

  useEffect(() => {
    const names = metricsKey ? metricsKey.split(" ") : [];
    if (!names.length) {
      // Clears previously-loaded series when the watch set empties; runs only on
      // that transition, so there is no cascading-render concern here.
      // eslint-disable-next-line react-hooks/set-state-in-effect -- intentional reset, see above
      setSeriesByMetric({});
      return;
    }
    let cancelled = false;

    async function load() {
      if (pausedRef.current) {
        return;
      }
      const next = await fetchSeriesByMetric(targetId, names);
      if (!cancelled) {
        setSeriesByMetric(next);
      }
    }

    void load();
    const timer = window.setInterval(load, pollMs);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
    // `scrubbing` re-runs this on resume so fresh series load immediately
    // instead of waiting out the poll interval with a stale timeline end.
  }, [metricsKey, targetId, scrubbing, pollMs, pausedRef]);

  return seriesByMetric;
}
