import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { Dispatch, SetStateAction } from "react";
import type { ScrubPosition } from "../hooks/useScrub";
import { useTargetData } from "../hooks/useTargetData";
import { useTimelineControls } from "../hooks/useTimelineControls";
import { useWatchedSeries } from "../hooks/useWatchedSeries";
import { isEditable } from "../lib/dom";
import { chartKindForMetric, chartMetric } from "../lib/series";
import type { ChartSpec } from "../lib/series";
import { expandedKey, loadStringArray, pinsKey, saveStringArray } from "../lib/storage";
import type { ChartKind, MetricFamily, SuggestedPanel, Target } from "../types";
import { MetricList } from "./MetricList";
import { PanelChart } from "./PanelChart";
import { TimeScrubber } from "./TimeScrubber";

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

  const { data, lastUpdated, refresh, previousValue } = useTargetData(target.id, pausedRef, scrapeIntervalMs);
  const families = useMemo(() => data.metrics?.families ?? [], [data.metrics]);

  const sampleNames = useMemo(() => {
    const names = new Set<string>();
    families.forEach((family) => family.samples.forEach((sample) => names.add(sample.metric)));
    return [...names];
  }, [families]);

  const controls = useTimelineControls(
    target.id,
    retentionMs,
    sampleNames,
    refresh,
    scrubPosition,
    onScrubPosition,
  );
  const scrubbing = controls.scrubbing;

  useEffect(() => {
    pausedRef.current = scrubbing;
  }, [scrubbing]);

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
        if (controls.scrubbing) {
          controls.goLive();
        }
      } else if (event.key === "ArrowLeft" || event.key === "ArrowRight") {
        event.preventDefault();
        controls.nudge(event.key === "ArrowLeft" ? -1 : 1);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [controls]);

  useEffect(() => {
    saveStringArray(pinsKey(target.id), pinned);
  }, [pinned, target.id]);

  // The dashboard holds only the metrics the user has pinned — nothing is added
  // by default. Backend `data.panels` are still used below for chart-kind hints.
  const dashboardCharts = useMemo<DashboardChart[]>(
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

  // Charts only exist for expanded families and pinned metrics; poll series for exactly those.
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

  const seriesByMetric = useWatchedSeries(target.id, watched, pausedRef, scrubbing, scrapeIntervalMs);

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

  const chartDomain = controls.chartDomain;

  return (
    <>
      <TimeScrubber
        domain={controls.domain}
        lastUpdated={lastUpdated}
        live={!scrubbing}
        loading={controls.loading}
        onLive={controls.goLive}
        onNudge={controls.nudge}
        onScrub={controls.scrubTo}
        value={controls.t ?? controls.domain[1]}
      />

      <main className="mx-auto max-w-6xl px-6 pb-16">
        {data.error ? <p className="py-3 text-xs text-danger">{data.error}</p> : null}
        {target.status === "down" && target.lastError ? (
          <p className="py-3 text-xs text-warn">target down — {target.lastError}</p>
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
                scrubbing && controls.t !== null
                  ? {
                      active: true,
                      loading: controls.loading,
                      t: controls.t,
                      seriesByMetric: controls.series,
                    }
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
                      scrubT={scrubbing ? controls.t : null}
                      series={
                        scrubbing
                          ? (controls.series?.[chart.metric] ?? seriesByMetric[chart.metric] ?? [])
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
