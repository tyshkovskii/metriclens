import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { Dispatch, SetStateAction } from "react";
import type { ScrubPosition } from "../hooks/useScrub";
import { useTargetData } from "../hooks/useTargetData";
import { useTimelineControls } from "../hooks/useTimelineControls";
import { useWatchedSeries } from "../hooks/useWatchedSeries";
import { isEditable } from "../lib/dom";
import { shortDuration } from "../lib/format";
import { chartKindForMetric, chartMetric } from "../lib/series";
import { expandedKey, loadStringArray, pinsKey, saveStringArray } from "../lib/storage";
import type { ChartKind, MetricFamily, SuggestedPanel, Target } from "../types";
import { MetricList } from "./MetricList";
import { PanelChart } from "./PanelChart";
import { TimeScrubber } from "./TimeScrubber";

/** Per-target search text, kept for the session so tab switches don't lose it. */
const searchMemory = new Map<string, string>();

export function TargetView({
  target,
  retentionMs,
  scrapeIntervalMs,
  scrubPosition,
  onScrubPosition,
  linkedMetric,
}: {
  target: Target;
  retentionMs: number;
  scrapeIntervalMs: number;
  scrubPosition: ScrubPosition | null;
  onScrubPosition: Dispatch<SetStateAction<ScrubPosition | null>>;
  linkedMetric: string | null;
}) {
  const pausedRef = useRef(false);
  const [openedAt] = useState(Date.now);
  const searchRef = useRef<HTMLInputElement | null>(null);
  const [search, setSearch] = useState(() => linkedMetric ?? searchMemory.get(target.id) ?? "");
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
  const linkedFamily = families.find(
    (family) =>
      family.name === linkedMetric || family.samples.some((sample) => sample.metric === linkedMetric),
  )?.name;
  const openedLink = useRef(false);

  useEffect(() => {
    if (linkedFamily && !openedLink.current) {
      openedLink.current = true;
      setExpanded((current) => new Set(current).add(linkedFamily));
    }
  }, [linkedFamily]);

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

  // Charts only exist for expanded families and pinned metrics; poll series for exactly those.
  const watched = useMemo(() => {
    const names = new Set(pinned);
    expanded.forEach((familyName) => {
      const family = families.find((candidate) => candidate.name === familyName);
      if (family) {
        names.add(chartMetric(family));
      }
    });
    return [...names].sort();
  }, [pinned, expanded, families]);

  const { seriesByMetric, error: liveHistoryError } = useWatchedSeries(
    target.id,
    watched,
    pausedRef,
    scrubbing,
    scrapeIntervalMs,
  );
  const historyError = scrubbing ? controls.error : liveHistoryError;

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
        {scrubPosition ? (
          <p className="py-3 text-xs text-muted" role="status">
            Viewing samples from {new Date(scrubPosition.domain[0]).toISOString()} to{" "}
            {new Date(scrubPosition.domain[1]).toISOString()}. Target status and metric descriptions are
            current. History is retained for {shortDuration(retentionMs)}.
            {scrubPosition.domain[1] < openedAt - retentionMs
              ? " This time window has expired; historical samples are unavailable."
              : ""}
          </p>
        ) : null}
        {data.error ? <p className="py-3 text-xs text-danger">{data.error}</p> : null}
        {historyError ? (
          <p className="py-3 text-xs text-danger" role="alert">
            History unavailable: {historyError}
          </p>
        ) : null}
        {target.status === "down" && target.lastError ? (
          <p className="py-3 text-xs text-warn">target down — {target.lastError}</p>
        ) : null}

        {data.metrics ? (
          <>
            {linkedMetric && !linkedFamily ? (
              <p className="py-3 text-xs text-warn" role="status">
                Linked metric “{linkedMetric}” is not in the current scrape. Its history may have expired or
                the metric may have been removed.
              </p>
            ) : null}
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
                      start: controls.domain[0],
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

            {pinned.length ? (
              <section aria-label="Dashboard" className="mt-10">
                <div className="flex items-baseline gap-3 pb-3">
                  <h2 className="text-[11px] uppercase tracking-widest text-muted">dashboard</h2>
                  <span className="text-[11px] tabular-nums text-muted">
                    {pinned.length} panel{pinned.length === 1 ? "" : "s"}
                  </span>
                </div>
                <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
                  {pinned.map((metric) => (
                    <PanelChart
                      domain={chartDomain}
                      key={metric}
                      kind={kindFor(metric, families, data.panels)}
                      metric={metric}
                      onRemove={() => togglePin(metric)}
                      scrubT={scrubbing ? controls.t : null}
                      series={
                        scrubbing
                          ? (controls.series?.[metric] ?? seriesByMetric[metric] ?? [])
                          : (seriesByMetric[metric] ?? [])
                      }
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
