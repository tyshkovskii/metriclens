import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { Dispatch, SetStateAction } from "react";
import { useLiveDomain } from "../hooks/useLiveDomain";
import { useScrub } from "../hooks/useScrub";
import type { ScrubPosition } from "../hooks/useScrub";
import { useTargetData } from "../hooks/useTargetData";
import { useWatchedSeries } from "../hooks/useWatchedSeries";
import { isEditable } from "../lib/dom";
import { chartKindForMetric, chartMetric, chartSpecForPanel } from "../lib/series";
import type { ChartSpec } from "../lib/series";
import { expandedKey, loadStringArray, pinsKey, saveStringArray } from "../lib/storage";
import type { ChartKind, MetricFamily, SuggestedPanel, Target } from "../types";
import { MetricList } from "./MetricList";
import { PanelChart } from "./PanelChart";
import { TimeScrubber } from "./TimeScrubber";

const NUDGE_MS = 5000;
const LIVE_SNAP_MS = 2500;

type DashboardChart = ChartSpec & {
  removable: boolean;
};

/** Per-target search text, kept for the session so tab switches don't lose it. */
const searchMemory = new Map<string, string>();

export function TargetView({
  target,
  retentionMs,
  scrapeIntervalMs,
  scrubPosition,
  onScrubPosition,
}: {
  target: Target;
  retentionMs: number;
  scrapeIntervalMs: number;
  scrubPosition: ScrubPosition | null;
  onScrubPosition: Dispatch<SetStateAction<ScrubPosition | null>>;
}) {
  const pausedRef = useRef(false);
  const searchRef = useRef<HTMLInputElement | null>(null);
  const [search, setSearch] = useState(() => searchMemory.get(target.id) ?? "");
  const [expanded, setExpanded] = useState<Set<string>>(
    () => new Set(loadStringArray(expandedKey(target.id))),
  );
  const [pinned, setPinned] = useState<string[]>(() => loadStringArray(pinsKey(target.id)));

  useEffect(() => {
    searchMemory.set(target.id, search);
  }, [search, target.id]);

  useEffect(() => {
    saveStringArray(expandedKey(target.id), [...expanded]);
  }, [expanded, target.id]);

  const { data, lastUpdated, refresh, previousValue } = useTargetData(
    target.id,
    pausedRef,
    scrapeIntervalMs,
  );
  const families = useMemo(() => data.metrics?.families ?? [], [data.metrics]);

  const sampleNames = useMemo(() => {
    const names = new Set<string>();
    families.forEach((family) => family.samples.forEach((sample) => names.add(sample.metric)));
    return [...names];
  }, [families]);

  const liveDomain = useLiveDomain(retentionMs);
  const scrub = useScrub(target.id, sampleNames, refresh, scrubPosition, onScrubPosition);

  const scrubbing = scrub.mode === "scrub";

  useEffect(() => {
    pausedRef.current = scrubbing;
  }, [scrubbing]);

  const domain = scrub.domain ?? liveDomain;

  const handleScrub = useCallback(
    (t: number) => {
      // Snap zone scales with the visible span so "drag to the end" lands
      // within a comfortable ~2% of track instead of a couple of pixels.
      const snap = Math.max(LIVE_SNAP_MS, (domain[1] - domain[0]) * 0.02);
      if (scrub.mode === "scrub" && t >= domain[1] - snap) {
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

  const nudgeScrub = useCallback(
    (step: number) => {
      if (step > 0 && scrubPosition) {
        const snap = Math.max(LIVE_SNAP_MS, (scrubPosition.domain[1] - scrubPosition.domain[0]) * 0.02);
        if (scrubPosition.t + step >= scrubPosition.domain[1] - snap) {
          scrub.goLive();
          return;
        }
      }
      onScrubPosition((current) => {
        if (current) {
          return { ...current, t: clampTime(current.t + step, current.domain) };
        }
        if (step > 0) {
          return current;
        }
        return { t: clampTime(liveDomain[1] + step, liveDomain), domain: liveDomain };
      });
    },
    [liveDomain, onScrubPosition, scrub, scrubPosition],
  );

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (isEditable(event.target)) {
        if (event.key === "Escape") {
          (event.target as HTMLElement).blur();
        }
        return;
      }
      if (event.key === "/") {
        event.preventDefault();
        searchRef.current?.focus();
      } else if (event.key === "l") {
        if (scrub.mode === "scrub") {
          scrub.goLive();
        }
      } else if (event.key === "ArrowLeft" || event.key === "ArrowRight") {
        event.preventDefault();
        const step = event.key === "ArrowLeft" ? -NUDGE_MS : NUDGE_MS;
        nudgeScrub(step);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [scrub, nudgeScrub]);

  useEffect(() => {
    saveStringArray(pinsKey(target.id), pinned);
  }, [pinned, target.id]);

  const suggestedCharts = useMemo(
    () =>
      data.panels
        .map(chartSpecForPanel)
        .filter((panel): panel is ChartSpec => panel !== null)
        .map((panel) => ({ ...panel, removable: false })),
    [data.panels],
  );

  const pinnedCharts = useMemo(
    () =>
      pinned.map((metric) => ({
        id: `pin:${metric}`,
        title: metric,
        metric,
        kind: kindFor(metric, families, data.panels),
        removable: true,
      })),
    [pinned, families, data.panels],
  );

  const dashboardCharts = useMemo(
    () => mergeDashboardCharts(suggestedCharts, pinnedCharts),
    [suggestedCharts, pinnedCharts],
  );

  // Charts only exist for suggested panels, expanded families, and pinned metrics; poll series for exactly those.
  const watched = useMemo(() => {
    const names = new Set(dashboardCharts.map((chart) => chart.metric));
    expanded.forEach((familyName) => {
      const family = families.find((candidate) => candidate.name === familyName);
      if (family) {
        names.add(chartMetric(family));
      }
    });
    return [...names].sort();
  }, [dashboardCharts, expanded, families]);

  const seriesByMetric = useWatchedSeries(
    target.id,
    watched,
    pausedRef,
    scrubbing,
    scrapeIntervalMs,
  );

  const toggleExpand = useCallback((name: string) => {
    setExpanded((current) => {
      const next = new Set(current);
      if (next.has(name)) {
        next.delete(name);
      } else {
        next.add(name);
      }
      return next;
    });
  }, []);

  const togglePin = useCallback((metric: string) => {
    setPinned((current) =>
      current.includes(metric) ? current.filter((name) => name !== metric) : [...current, metric],
    );
  }, []);

  const chartDomain = scrubbing ? domain : liveDomain;

  return (
    <>
      <TimeScrubber
        domain={domain}
        lastUpdated={lastUpdated}
        live={!scrubbing}
        loading={scrub.loading}
        onLive={scrub.goLive}
        onNudge={(direction) => nudgeScrub(direction * NUDGE_MS)}
        onScrub={handleScrub}
        value={scrub.t ?? domain[1]}
      />

      <main className="mx-auto max-w-6xl px-6 pb-16">
        {data.error ? (
          <p className="py-3 text-xs text-danger">{data.error}</p>
        ) : null}
        {target.status === "down" && target.lastError ? (
          <p className="py-3 text-xs text-warn">
            target down — {target.lastError}
          </p>
        ) : null}

        {data.metrics ? (
          <>
            <MetricList
              domain={chartDomain}
              expanded={expanded}
              families={families}
              issues={data.issues}
              onToggleExpand={toggleExpand}
              onTogglePin={togglePin}
              pinned={pinned}
              previousValue={previousValue}
              scrub={
                scrubbing && scrub.t !== null
                  ? { active: true, loading: scrub.loading, t: scrub.t, seriesByMetric: scrub.series }
                  : null
              }
              search={search}
              searchRef={searchRef}
              seriesByMetric={seriesByMetric}
              setSearch={setSearch}
              targetId={target.id}
              panels={data.panels}
            />

            {dashboardCharts.length ? (
              <section aria-label="Dashboard" className="mt-10">
                <div className="flex items-baseline gap-3 pb-3">
                  <h2 className="text-[11px] uppercase tracking-widest text-muted">dashboard</h2>
                  <span className="text-[11px] tabular-nums text-muted">
                    {dashboardCharts.length} panel{dashboardCharts.length === 1 ? "" : "s"}
                  </span>
                </div>
                <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
                  {dashboardCharts.map((chart) => (
                    <PanelChart
                      domain={chartDomain}
                      key={chart.id}
                      kind={chart.kind}
                      metric={chart.metric}
                      onRemove={chart.removable ? () => togglePin(chart.metric) : undefined}
                      scrubT={scrubbing ? scrub.t : null}
                      series={
                        scrubbing
                          ? (scrub.series?.[chart.metric] ?? seriesByMetric[chart.metric] ?? [])
                          : (seriesByMetric[chart.metric] ?? [])
                      }
                      title={chart.title}
                    />
                  ))}
                </div>
              </section>
            ) : null}
          </>
        ) : (
          <p className="py-8 text-[11px] text-muted">waiting for target metrics…</p>
        )}
      </main>
    </>
  );
}

function kindFor(metric: string, families: MetricFamily[], panels: SuggestedPanel[]): ChartKind {
  const family = families.find((candidate) => candidate.samples.some((sample) => sample.metric === metric));
  return chartKindForMetric(metric, family?.type, panels);
}

function mergeDashboardCharts(suggested: DashboardChart[], pinned: DashboardChart[]): DashboardChart[] {
  const seen = new Set<string>();
  const charts: DashboardChart[] = [];
  for (const chart of [...suggested, ...pinned]) {
    const key = `${chart.kind}:${chart.metric}`;
    if (seen.has(key)) {
      continue;
    }
    seen.add(key);
    charts.push(chart);
  }
  return charts;
}

function clampTime(t: number, domain: [number, number]): number {
  return Math.min(Math.max(t, domain[0]), domain[1]);
}
