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
import { LAST_TARGET_KEY, loadString, saveString } from "./lib/storage";
import type { Target } from "./types";

export default function App() {
  const { toggle } = useTheme();
  const config = useConfig();
  const [targets, setTargets] = useState<Target[]>([]);
  const [targetsError, setTargetsError] = useState<string | null>(null);
  const [selectedId, setSelectedId] = useState<string | null>(
    () => hashTarget() ?? loadString(LAST_TARGET_KEY),
  );
  const [shortcutsOpen, setShortcutsOpen] = useState(false);
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
      if (event.key === "Tab") {
        event.preventDefault();
        return;
      }
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
          window.location.hash = encodeURIComponent(target.id);
        }
        return;
      }
      if (event.key === "n" || event.key === "p") {
        selectRelativeTarget(targetsRef.current, selectedId, event.key === "n" ? 1 : -1);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [selectedId, shortcutsOpen, toggle]);

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
            aria-label="Toggle theme"
            className="-m-2 flex shrink-0 items-center gap-2 self-center p-2 text-sm text-muted hover:text-fg"
            onClick={toggle}
            title="toggle theme  t"
            type="button"
          >
            ◐
            <Keycap className="hidden sm:inline-flex" value="T" />
          </button>
        </div>
      </header>

      {shortcutsOpen ? <ShortcutOverlay onClose={() => setShortcutsOpen(false)} /> : null}

      {targetsError ? (
        <p className="mx-auto max-w-6xl px-6 py-3 text-xs text-danger">{targetsError}</p>
      ) : null}

      {selected ? (
        <TargetView
          key={selected.id}
          onScrubPosition={setScrubPosition}
          retentionMs={config.retentionMs}
          scrapeIntervalMs={config.scrapeIntervalMs}
          scrubPosition={scrubPosition}
          target={selected}
        />
      ) : (
        <EmptyState error={targetsError} />
      )}
    </div>
  );
}

function selectRelativeTarget(targets: Target[], selectedId: string | null, offset: number) {
  if (!targets.length) {
    return;
  }
  const current = targets.findIndex((target) => target.id === selectedId);
  const nextIndex = current === -1 ? 0 : (current + offset + targets.length) % targets.length;
  window.location.hash = encodeURIComponent(targets[nextIndex].id);
}

function hashTarget(): string | null {
  const hash = window.location.hash.slice(1);
  return hash ? decodeURIComponent(hash) : null;
}
