import { useEffect, useMemo, useState } from "react";
import type { RefObject } from "react";
import { familySummary, isRuntimeFamily } from "../lib/schema";
import type { ScrubView } from "../lib/schema";
import { loadFlag, runtimeKey, saveFlag } from "../lib/storage";
import { FamilyRow } from "./FamilyRow";
import type { PreviousValue } from "../hooks/useTargetData";
import type { MetricFamily, MetricQualityIssue, Series } from "../types";

export function MetricList({
  targetId,
  families,
  issues,
  search,
  setSearch,
  searchRef,
  scrub,
  previousValue,
  expanded,
  onToggleExpand,
  pinned,
  onTogglePin,
  seriesByMetric,
  domain,
}: {
  targetId: string;
  families: MetricFamily[];
  issues: MetricQualityIssue[];
  search: string;
  setSearch: (value: string) => void;
  searchRef: RefObject<HTMLInputElement | null>;
  scrub: ScrubView | null;
  previousValue: (key: string) => PreviousValue | null;
  expanded: Set<string>;
  onToggleExpand: (name: string) => void;
  pinned: string[];
  onTogglePin: (metric: string) => void;
  seriesByMetric: Record<string, Series[]>;
  domain: [number, number] | null;
}) {
  const [runtimeOpen, setRuntimeOpen] = useState(() => loadFlag(runtimeKey(targetId)));

  useEffect(() => {
    saveFlag(runtimeKey(targetId), runtimeOpen);
  }, [runtimeOpen, targetId]);

  const issuesByFamily = useMemo(() => {
    const grouped = new Map<string, MetricQualityIssue[]>();
    issues.forEach((issue) => grouped.set(issue.metric, [...(grouped.get(issue.metric) || []), issue]));
    return grouped;
  }, [issues]);

  const summaries = useMemo(
    () => new Map(families.map((family) => [family.name, familySummary(family)])),
    [families],
  );

  const normalized = search.trim().toLowerCase();
  const searching = normalized.length > 0;

  const { service, runtime } = useMemo(() => {
    const filtered = families
      .filter(
        (family) =>
          !normalized ||
          family.name.toLowerCase().includes(normalized) ||
          family.samples.some((sample) => sample.metric.toLowerCase().includes(normalized)),
      )
      .sort((left, right) => left.name.localeCompare(right.name));
    return {
      service: filtered.filter((family) => !isRuntimeFamily(family.name)),
      runtime: filtered.filter((family) => isRuntimeFamily(family.name)),
    };
  }, [families, normalized]);

  const filteredCount = service.length + runtime.length;
  const runtimeShown = runtimeOpen || (searching && runtime.length > 0);

  const renderFamily = (family: MetricFamily) => (
    <FamilyRow
      domain={domain}
      expanded={expanded.has(family.name)}
      family={family}
      issues={issuesByFamily.get(family.name) || []}
      key={family.name}
      onToggleExpand={() => onToggleExpand(family.name)}
      onTogglePin={onTogglePin}
      pinned={pinned}
      previousValue={previousValue}
      scrub={scrub}
      seriesByMetric={seriesByMetric}
      summary={summaries.get(family.name) ?? familySummary(family)}
    />
  );

  return (
    <section aria-label="Metrics">
      <div className="sticky top-0 z-10 flex items-baseline gap-3 border-b border-edge bg-bg py-2">
        <h2 className="text-[11px] uppercase tracking-widest text-muted">metrics</h2>
        <input
          className="w-56 border border-edge bg-transparent px-2 py-1 text-xs placeholder:text-muted focus:border-muted"
          onChange={(event) => setSearch(event.target.value)}
          placeholder="search  /"
          ref={searchRef}
          type="search"
          value={search}
        />
        <span className="text-[11px] text-muted">
          {filteredCount}/{families.length} families
        </span>
        {issues.length ? (
          <span className="ml-auto text-[11px] text-warn">
            {issues.length} issue{issues.length === 1 ? "" : "s"}
          </span>
        ) : null}
      </div>

      {filteredCount === 0 ? (
        <p className="py-6 text-[11px] text-muted">no metrics match the current search</p>
      ) : (
        <>
          {service.map(renderFamily)}
          {runtime.length ? (
            <>
              <button
                aria-expanded={runtimeShown}
                className="flex w-full items-baseline gap-2 border-b border-edge px-2 py-2 text-left hover:bg-fg/[0.06]"
                onClick={() => setRuntimeOpen((open) => !open)}
                type="button"
              >
                <span aria-hidden="true" className="w-3 shrink-0 text-[11px] text-muted">
                  {runtimeShown ? "▾" : "▸"}
                </span>
                <span className="text-[11px] uppercase tracking-widest text-muted">runtime</span>
                <span className="text-[11px] tabular-nums text-muted">{runtime.length} families</span>
              </button>
              {runtimeShown ? runtime.map(renderFamily) : null}
            </>
          ) : null}
        </>
      )}
    </section>
  );
}
