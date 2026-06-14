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
  onNudge,
}: {
  domain: [number, number];
  value: number;
  live: boolean;
  loading: boolean;
  lastUpdated: Date | null;
  onScrub: (t: number) => void;
  onLive: () => void;
  onNudge: (direction: -1 | 1) => void;
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
        <div className="mt-[3px] flex shrink-0 items-center gap-1.5">
          <TimelineButton
            className="hidden md:flex"
            keyLabel="←"
            label="back"
            onClick={() => onNudge(-1)}
            title="scrub left 5 seconds"
          />
          <TimelineButton
            className="hidden md:flex"
            disabled={live}
            keyLabel="→"
            label="forward"
            onClick={() => onNudge(1)}
            title="scrub right 5 seconds"
          />
          <TimelineButton
            active={live}
            dot
            keyLabel="L"
            label="live"
            onClick={onLive}
            pressed={live}
            title={live ? "live" : "go live  l"}
          />
        </div>
      </div>
    </div>
  );
}

function TimelineButton({
  label,
  keyLabel,
  onClick,
  active = false,
  disabled = false,
  dot = false,
  pressed,
  title,
  className = "",
}: {
  label: string;
  keyLabel: string;
  onClick: () => void;
  active?: boolean;
  disabled?: boolean;
  dot?: boolean;
  pressed?: boolean;
  title?: string;
  className?: string;
}) {
  return (
    <button
      aria-pressed={pressed}
      className={`h-6 w-[88px] shrink-0 items-center justify-between gap-1.5 border px-2 text-[11px] tracking-normal transition-colors disabled:cursor-default disabled:opacity-45 ${
        active
          ? "border-accent bg-accent text-bg"
          : "border-edge text-muted hover:border-accent hover:text-accent disabled:hover:border-edge disabled:hover:text-muted"
      } ${className || "flex"}`}
      disabled={disabled}
      onClick={onClick}
      title={title}
      type="button"
    >
      <span className="flex items-center gap-1.5">
        {dot ? (
          <span
            className={`h-1.5 w-1.5 shrink-0 rounded-full ${active ? "bg-bg motion-safe:animate-pulse" : "bg-muted"}`}
          />
        ) : null}
        <span>{label}</span>
      </span>
      <Keycap className={active ? "border-bg/35 bg-transparent text-bg" : ""} value={keyLabel} />
    </button>
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
