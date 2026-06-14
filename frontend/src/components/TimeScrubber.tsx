import { Fragment, useEffect, useState } from "react";
import { clockTime, shortDuration, shortTime } from "../lib/format";
import { Keycap } from "./HotkeyHint";

/* Hide major tick labels this close to the track edges so they don't clip. */
const EDGE_PCT = 4;

export function TimeScrubber({
  domain,
  value,
  live,
  loading,
  lastUpdated,
  onScrub,
  onLive,
  onRefresh,
}: {
  domain: [number, number];
  value: number;
  live: boolean;
  loading: boolean;
  lastUpdated: Date | null;
  onScrub: (t: number) => void;
  onLive: () => void;
  onRefresh: () => void;
}) {
  const [min, max] = domain;
  const span = max - min || 1;
  const now = useNow();

  const ticks: { t: number; major: boolean; pct: number }[] = [];
  for (let t = Math.ceil(min / 60_000) * 60_000; t <= max; t += 60_000) {
    ticks.push({ t, major: t % 300_000 === 0, pct: ((t - min) / span) * 100 });
  }

  const selected = live ? max : value;
  const fillPct = Math.min(100, Math.max(0, ((selected - min) / span) * 100));

  const status = loading
    ? "loading history…"
    : live
      ? lastUpdated
        ? `updated ${clockTime(lastUpdated)}`
        : "connecting…"
      : `paused −${shortDuration(now - value)}`;

  return (
    <div className="border-b border-edge">
      <div className="mx-auto flex max-w-6xl items-start gap-4 px-6 py-3">
        <div className="flex h-10 w-28 shrink-0 flex-col justify-between">
          <span className="pt-2 text-xs leading-none tabular-nums">
            {clockTime(live ? now : value)}
          </span>
          <span
            className={`truncate text-[10px] leading-none tabular-nums ${
              !loading && !live ? "text-warn" : "text-muted"
            }`}
          >
            {status}
          </span>
        </div>
        <div className="relative h-10 min-w-0 flex-1">
          <div aria-hidden="true" className="pointer-events-none absolute inset-0">
            {ticks.map(({ t, major, pct }) => (
              <Fragment key={t}>
                <span
                  className={`absolute top-[14px] w-px -translate-y-1/2 bg-edge ${major ? "h-3" : "h-2"}`}
                  style={{ left: `${pct}%` }}
                />
                {major && pct > EDGE_PCT && pct < 100 - EDGE_PCT ? (
                  <span
                    className="absolute bottom-0 -translate-x-1/2 text-[10px] leading-none tabular-nums text-muted"
                    style={{ left: `${pct}%` }}
                  >
                    {shortTime(t)}
                  </span>
                ) : null}
              </Fragment>
            ))}
          </div>
          <input
            aria-label="Time scrubber"
            aria-valuetext={clockTime(selected)}
            className="scrubber absolute inset-x-0 top-0 h-7"
            max={max}
            min={min}
            onChange={(event) => onScrub(Number(event.target.value))}
            step={1000}
            style={{ "--scrub-pct": `${fillPct}%` } as React.CSSProperties}
            type="range"
            value={selected}
          />
        </div>
        <span className="mt-[5px] hidden shrink-0 items-center gap-1 text-[11px] text-muted md:flex">
          scrub
          <Keycap value="Left" />
          <Keycap value="Right" />
        </span>
        <button
          className="mt-[3px] flex h-[22px] shrink-0 items-center gap-1.5 border border-edge px-2 text-[11px] uppercase tracking-widest text-muted transition-colors hover:border-accent hover:text-accent"
          onClick={onRefresh}
          title="refresh target  r"
          type="button"
        >
          refresh
          <Keycap value="R" />
        </button>
        <button
          aria-pressed={live}
          className={`mt-[3px] flex h-[22px] shrink-0 items-center gap-1.5 border px-2 text-[11px] uppercase tracking-widest transition-colors ${
            live
              ? "border-accent bg-accent text-bg"
              : "border-edge text-muted hover:border-accent hover:text-accent"
          }`}
          onClick={onLive}
          title={live ? "live" : "go live  l"}
          type="button"
        >
          <span
            className={`h-1.5 w-1.5 rounded-full ${live ? "bg-bg motion-safe:animate-pulse" : "bg-muted"}`}
          />
          live
          <Keycap className={live ? "border-bg/35 bg-bg/10 text-bg shadow-none" : ""} value="L" />
        </button>
      </div>
    </div>
  );
}

/** Wall clock ticking once per second; drives the live readout and the paused delta. */
function useNow() {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, []);
  return now;
}
