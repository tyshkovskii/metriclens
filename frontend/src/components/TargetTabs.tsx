import type { Target } from "../types";

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
    // -mb-px lets tab borders sit on the header rule: inactive bottoms blend
    // into it, the active tab's bg-colored bottom punches through to the page.
    <nav aria-label="Detected services" className="-mb-px flex min-w-0 flex-1 items-end gap-1 overflow-x-auto">
      {targets.map((target, index) => {
        const active = target.id === selectedId;
        return (
          <button
            aria-current={active ? "page" : undefined}
            className={`flex h-9 shrink-0 items-center gap-2 border px-3 text-xs transition-colors ${
              active
                ? "border-edge border-b-bg border-t-accent bg-bg text-fg"
                : "border-edge text-muted hover:bg-fg/[0.04] hover:text-fg"
            }`}
            key={target.id}
            onClick={() => onSelect(target.id)}
            type="button"
          >
            {index < 9 ? <span className="text-[11px] text-muted">{index + 1}</span> : null}
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
