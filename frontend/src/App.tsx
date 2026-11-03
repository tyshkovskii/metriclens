import { useCallback, useEffect, useRef, useState } from "react";
import { fetchTargets } from "./api";
import { EmptyState } from "./components/EmptyState";
import { Keycap } from "./components/HotkeyHint";
import { ShortcutOverlay } from "./components/ShortcutOverlay";
import { TargetTabs } from "./components/TargetTabs";
import { TargetView } from "./components/TargetView";
import { useConfig } from "./hooks/useConfig";
import type { ScrubPosition } from "./hooks/useScrub";
import { useTheme } from "./hooks/useTheme";
import { isEditable } from "./lib/dom";
import { readMetricLocation } from "./lib/location";
import { LAST_TARGET_KEY, loadString, saveString } from "./lib/storage";
import type { Target } from "./types";

export default function App() {
  const { dark, toggle } = useTheme();
  const config = useConfig();
  const [targets, setTargets] = useState<Target[]>([]);
  const [targetsError, setTargetsError] = useState<string | null>(null);
  const [targetsLoaded, setTargetsLoaded] = useState(false);
  const [route, setRoute] = useState(() => readMetricLocation(window.location));
  const [selectedId, setSelectedId] = useState<string | null>(
    () => route.targetId ?? loadString(LAST_TARGET_KEY),
  );
  const [shortcutsOpen, setShortcutsOpen] = useState(false);
  // Lifted scrub position, shared by every target so the timeline survives tab switches.
  const [scrubPosition, setScrubPosition] = useState<ScrubPosition | null>(route.position);

  useEffect(() => {
    let cancelled = false;

    async function load() {
      try {
        const next = await fetchTargets();
        if (!cancelled) {
          setTargets(next);
          setTargetsError(null);
          setTargetsLoaded(true);
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
    const onNavigation = () => {
      const next = readMetricLocation(window.location);
      setRoute(next);
      setSelectedId(next.targetId);
      setScrubPosition(next.position);
    };
    window.addEventListener("hashchange", onNavigation);
    window.addEventListener("popstate", onNavigation);
    return () => {
      window.removeEventListener("hashchange", onNavigation);
      window.removeEventListener("popstate", onNavigation);
    };
  }, []);

  const select = useCallback((id: string) => {
    const next = new URL(window.location.href);
    ["target", "metric", "start", "end"].forEach((key) => next.searchParams.delete(key));
    next.hash = encodeURIComponent(id);
    window.history.pushState(null, "", next);
    setRoute({ targetId: id, metric: null, position: null, error: null });
    setSelectedId(id);
  }, []);

  const targetsRef = useRef(targets);
  useEffect(() => {
    targetsRef.current = targets;
  }, [targets]);

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape" && shortcutsOpen) {
        setShortcutsOpen(false);
        return;
      }
      if (isEditable(event.target)) {
        return;
      }
      if (event.key === "?") {
        event.preventDefault();
        setShortcutsOpen((open) => !open);
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
          select(target.id);
        }
        return;
      }
      if (event.key === "n" || event.key === "p") {
        selectRelativeTarget(targetsRef.current, selectedId, event.key === "n" ? 1 : -1, select);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [selectedId, shortcutsOpen, toggle, select]);

  const selected =
    targets.find((target) => target.id === selectedId) ?? (route.targetId ? null : (targets[0] ?? null));
  const selectedTargetId = selected?.id ?? null;
  const linkedTargetMissing = route.targetId && !selected && targetsLoaded && !targetsError;

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
            aria-expanded={shortcutsOpen}
            className="hidden shrink-0 items-center gap-2 self-center text-[11px] text-muted hover:text-fg sm:flex"
            onClick={() => setShortcutsOpen((open) => !open)}
            title="show shortcuts  ?"
            type="button"
          >
            shortcuts
            <Keycap value="?" />
          </button>
          <button
            aria-checked={dark}
            aria-label="Dark theme"
            className="flex shrink-0 items-center gap-2 self-center text-[11px] text-muted hover:text-fg"
            onClick={toggle}
            role="switch"
            title="toggle theme  t"
            type="button"
          >
            <span>{dark ? "dark" : "light"}</span>
            <Keycap className="hidden sm:inline-flex" value="T" />
          </button>
        </div>
      </header>

      {shortcutsOpen ? <ShortcutOverlay onClose={() => setShortcutsOpen(false)} /> : null}

      {targetsError ? (
        <p className="mx-auto max-w-6xl px-6 py-3 text-xs text-danger">{targetsError}</p>
      ) : null}

      {route.error ? (
        <p className="mx-auto max-w-6xl px-6 py-3 text-xs text-danger" role="alert">
          {route.error}
        </p>
      ) : null}

      {linkedTargetMissing ? (
        <p className="mx-auto max-w-6xl px-6 py-6 text-xs text-warn" role="status">
          Linked target “{route.targetId}” is no longer available. Select an available service or wait for
          discovery.
        </p>
      ) : null}

      {selected ? (
        <TargetView
          key={`${selected.id}:${route.metric ?? ""}:${route.position?.domain.join(":") ?? ""}`}
          linkedMetric={route.metric}
          onScrubPosition={setScrubPosition}
          retentionMs={config.retentionMs}
          scrapeIntervalMs={config.scrapeIntervalMs}
          scrubPosition={scrubPosition}
          target={selected}
        />
      ) : linkedTargetMissing ? null : (
        <EmptyState error={targetsError} />
      )}
    </div>
  );
}

function selectRelativeTarget(
  targets: Target[],
  selectedId: string | null,
  offset: number,
  select: (id: string) => void,
) {
  if (!targets.length) {
    return;
  }
  const current = targets.findIndex((target) => target.id === selectedId);
  const nextIndex = current === -1 ? 0 : (current + offset + targets.length) % targets.length;
  const next = targets[nextIndex];
  if (next) {
    select(next.id);
  }
}
