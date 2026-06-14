import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { Dispatch, SetStateAction } from "react";
import { useLiveDomain } from "../hooks/useLiveDomain";
import { useScrub } from "../hooks/useScrub";
import type { ScrubPosition } from "../hooks/useScrub";
import { useTargetData } from "../hooks/useTargetData";
import { useWatchedSeries } from "../hooks/useWatchedSeries";
import { isEditable } from "../lib/dom";
import { chartKind, chartMetric } from "../lib/series";
import { expandedKey, loadStringArray, pinsKey, saveStringArray } from "../lib/storage";
import type { ChartKind, MetricFamily, Target } from "../types";
import { MetricList } from "./MetricList";
import { PanelChart } from "./PanelChart";
import { TimeScrubber } from "./TimeScrubber";

const NUDGE_MS = 5000;
const LIVE_SNAP_MS = 2500;

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
      } else if (event.key === "r") {
        event.preventDefault();
        refresh();
      } else if (event.key === "l") {
        if (scrub.mode === "scrub") {
          scrub.goLive();
        }
      } else if (event.key === "ArrowLeft" || event.key === "ArrowRight") {
        event.preventDefault();
        const step = event.key === "ArrowLeft" ? -NUDGE_MS : NUDGE_MS;
        if (scrub.mode === "scrub" && scrub.t !== null) {
          handleScrub(scrub.t + step);
        } else if (step < 0) {
          handleScrub(domain[1] + step);
        }
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [scrub, domain, handleScrub, refresh]);

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
        onRefresh={refresh}
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
            />

            {pinned.length ? (
              <section aria-label="Dashboard" className="mt-10">
                <div className="flex items-baseline gap-3 pb-3">
                  <h2 className="text-[11px] uppercase tracking-widest text-muted">dashboard</h2>
                  <span className="text-[11px] tabular-nums text-muted">{pinned.length} pinned</span>
                </div>
                <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
                  {pinned.map((metric) => (
                    <PanelChart
                      domain={chartDomain}
                      key={metric}
                      kind={kindFor(metric, families)}
                      metric={metric}
                      onRemove={() => togglePin(metric)}
                      scrubT={scrubbing ? scrub.t : null}
                      series={
                        scrubbing
                          ? (scrub.series?.[metric] ?? seriesByMetric[metric] ?? [])
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

function kindFor(metric: string, families: MetricFamily[]): ChartKind {
  const family = families.find((candidate) => candidate.samples.some((sample) => sample.metric === metric));
  return chartKind(metric, family?.type);
}
