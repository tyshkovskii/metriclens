import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { fetchTargets } from "./api";
import { MetricList } from "./components/MetricList";
import { PanelChart } from "./components/PanelChart";
import { TargetTabs } from "./components/TargetTabs";
import { TimeScrubber } from "./components/TimeScrubber";
import { useConfig } from "./hooks/useConfig";
import { useLiveDomain } from "./hooks/useLiveDomain";
import { useScrub } from "./hooks/useScrub";
import type { ScrubPosition } from "./hooks/useScrub";
import { useTargetData } from "./hooks/useTargetData";
import { useTheme } from "./hooks/useTheme";
import { useWatchedSeries } from "./hooks/useWatchedSeries";
import { chartKind, chartMetric } from "./lib/series";
import { loadString, loadStringArray, saveString, saveStringArray } from "./lib/storage";
import type { AppConfig, MetricFamily, ChartKind, Target } from "./types";

const NUDGE_MS = 5000;
const LIVE_SNAP_MS = 2500;
const LAST_TARGET_KEY = "ml-last-target";

/** Per-target search text, kept for the session so tab switches don't lose it. */
const searchMemory = new Map<string, string>();

export default function App() {
  const { toggle } = useTheme();
  const config = useConfig();
  const [targets, setTargets] = useState<Target[]>([]);
  const [targetsError, setTargetsError] = useState<string | null>(null);
  const [selectedId, setSelectedId] = useState<string | null>(
    () => hashTarget() ?? loadString(LAST_TARGET_KEY),
  );
  // Lifted scrub position, shared by every target so the timeline survives tab switches.
  const [scrubPosition, setScrubPosition] = useState<ScrubPosition | null>(null);

  useEffect(() => {
    let cancelled = false;

    async function load() {
      try {
        const next = await fetchTargets();
        if (!cancelled) {
          setTargets(next);
          setTargetsError(null);
        }
      } catch (error) {
        if (!cancelled) {
          setTargetsError(error instanceof Error ? error.message : "request failed");
        }
      }
    }

    void load();
    const timer = window.setInterval(load, config.scrapeIntervalMs);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [config.scrapeIntervalMs]);

  useEffect(() => {
    const onHash = () => setSelectedId(hashTarget());
    window.addEventListener("hashchange", onHash);
    return () => window.removeEventListener("hashchange", onHash);
  }, []);

  const select = useCallback((id: string) => {
    window.location.hash = encodeURIComponent(id);
  }, []);

  const targetsRef = useRef(targets);
  targetsRef.current = targets;

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (isEditable(event.target)) {
        return;
      }
      if (event.key === "t") {
        toggle();
        return;
      }
      const digit = Number(event.key);
      if (digit >= 1 && digit <= 9) {
        const target = targetsRef.current[digit - 1];
        if (target) {
          window.location.hash = encodeURIComponent(target.id);
        }
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [toggle]);

  const selected = targets.find((target) => target.id === selectedId) ?? targets[0] ?? null;
  const selectedTargetId = selected?.id ?? null;

  useEffect(() => {
    if (selectedTargetId) {
      saveString(LAST_TARGET_KEY, selectedTargetId);
    }
  }, [selectedTargetId]);

  return (
    <div className="min-h-screen text-fg">
      <header className="border-b border-edge">
        <div className="mx-auto flex h-12 max-w-6xl items-stretch gap-6 px-6">
          <span className="flex shrink-0 items-center text-sm tracking-tight">
            metriclens<span className="animate-blink text-accent">_</span>
          </span>
          <TargetTabs
            onSelect={select}
            selectedId={selected?.id ?? null}
            staleMs={config.scrapeIntervalMs * 3}
            targets={targets}
          />
          <button
            aria-label="Toggle theme"
            className="-m-2 shrink-0 self-center p-2 text-sm text-muted hover:text-fg"
            onClick={toggle}
            title="toggle theme  t"
            type="button"
          >
            ◐
          </button>
        </div>
      </header>

      {targetsError ? (
        <p className="mx-auto max-w-6xl px-6 py-3 text-xs text-danger">{targetsError}</p>
      ) : null}

      {selected ? (
        <TargetView
          config={config}
          key={selected.id}
          onScrubPosition={setScrubPosition}
          scrubPosition={scrubPosition}
          target={selected}
        />
      ) : (
        <EmptyState error={targetsError} />
      )}
    </div>
  );
}

function TargetView({
  target,
  config,
  scrubPosition,
  onScrubPosition,
}: {
  target: Target;
  config: AppConfig;
  scrubPosition: ScrubPosition | null;
  onScrubPosition: React.Dispatch<React.SetStateAction<ScrubPosition | null>>;
}) {
  const pausedRef = useRef(false);
  const searchRef = useRef<HTMLInputElement | null>(null);
  const [search, setSearch] = useState(() => searchMemory.get(target.id) ?? "");
  const [expanded, setExpanded] = useState<Set<string>>(
    () => new Set(loadStringArray(`ml-expanded:${target.id}`)),
  );
  const [pinned, setPinned] = useState<string[]>(() => loadStringArray(`ml-pins:${target.id}`));

  useEffect(() => {
    searchMemory.set(target.id, search);
  }, [search, target.id]);

  useEffect(() => {
    saveStringArray(`ml-expanded:${target.id}`, [...expanded]);
  }, [expanded, target.id]);

  const { data, lastUpdated, refresh, previousValue } = useTargetData(
    target.id,
    pausedRef,
    config.scrapeIntervalMs,
  );
  const families = useMemo(() => data.metrics?.families ?? [], [data.metrics]);

  const sampleNames = useMemo(() => {
    const names = new Set<string>();
    families.forEach((family) => family.samples.forEach((sample) => names.add(sample.metric)));
    return [...names];
  }, [families]);

  const liveDomain = useLiveDomain(config.retentionMs);
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
  }, [scrub, domain, handleScrub]);

  useEffect(() => {
    saveStringArray(`ml-pins:${target.id}`, pinned);
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
    config.scrapeIntervalMs,
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

function EmptyState({ error }: { error: string | null }) {
  return (
    <div className="flex h-[70vh] flex-col items-center justify-center gap-3">
      <span className="h-2 w-2 animate-pulse rounded-full bg-accent" />
      <p className="text-xs text-muted">
        {error ? "backend unreachable" : "scanning docker compose services…"}
      </p>
      <p className="text-[11px] text-muted">services exposing a /metrics endpoint appear here as tabs</p>
    </div>
  );
}

function kindFor(metric: string, families: MetricFamily[]): ChartKind {
  const family = families.find((candidate) => candidate.samples.some((sample) => sample.metric === metric));
  return chartKind(metric, family?.type);
}

function hashTarget(): string | null {
  const hash = window.location.hash.slice(1);
  return hash ? decodeURIComponent(hash) : null;
}

function isEditable(target: EventTarget | null) {
  if (!(target instanceof HTMLElement)) {
    return false;
  }
  const tag = target.tagName;
  if (target instanceof HTMLInputElement) {
    // The scrubber handles its own arrow keys; treat other inputs as editable.
    return target.type !== "range";
  }
  return tag === "TEXTAREA" || tag === "SELECT" || target.isContentEditable;
}
