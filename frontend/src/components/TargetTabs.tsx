import type { Target } from "../types";
import { Keycap } from "./HotkeyHint";

export function TargetTabs({
  targets,
  selectedId,
  staleMs,
  onSelect,
}: {
  targets: Target[];
  selectedId: string | null;
  /** A scrape older than this is shown as stale; App passes 3x the scrape interval. */
  staleMs: number;
  onSelect: (id: string) => void;
}) {
  return (
    <nav aria-label="Detected services" className="flex min-w-0 flex-1 items-center gap-1 overflow-x-auto">
      {targets.map((target, index) => {
        const active = target.id === selectedId;
        return (
          <button
            aria-current={active ? "page" : undefined}
            className={`flex h-7 shrink-0 items-center gap-2 border px-2 text-xs transition-colors ${
              active
                ? "border-edge bg-fg/[0.06] text-fg"
                : "border-transparent text-muted hover:border-edge hover:text-fg"
            }`}
            key={target.id}
            onClick={() => onSelect(target.id)}
            type="button"
          >
            {index < 9 ? <Keycap value={String(index + 1)} /> : null}
            <span>{target.serviceName || target.containerName || target.id}</span>
            <StatusDot active={active} staleMs={staleMs} target={target} />
          </button>
        );
      })}
    </nav>
  );
}

function StatusDot({ target, staleMs, active }: { target: Target; staleMs: number; active: boolean }) {
  if (target.status === "down") {
    return <span aria-label="down" className="h-1.5 w-1.5 rounded-full bg-danger" />;
  }
  // Staleness is a relative-time readout; the parent re-renders every poll, so
  // reading the wall clock here is intentional and fresh enough.
  // eslint-disable-next-line react-hooks/purity -- intentional relative-time read, see above
  const stale = target.lastScrapeAt && Date.now() - Date.parse(target.lastScrapeAt) > staleMs;
  if (stale) {
    return (
      <span
        aria-label="up, but last scrape is stale"
        className="h-1.5 w-1.5 rounded-full border border-warn"
        title={`last scrape ${target.lastScrapeAt}`}
      />
    );
  }
  return (
    <span aria-label="up" className={`h-1.5 w-1.5 rounded-full bg-accent ${active ? "animate-pulse" : ""}`} />
  );
}
