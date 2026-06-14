import { useState } from "react";
import { formatNumber, labelsText, sampleKey } from "../lib/format";
import { groupHistogram, resolveRows } from "../lib/schema";
import type { FamilySummary, HistogramGroup, ResolvedRow, ScrubView } from "../lib/schema";
import { chartKind, chartMetric } from "../lib/series";
import { ChartBody, KindLabel } from "./PanelChart";
import { QualityBadge, QualityIssueList } from "./QualityBadge";
import type { PreviousValue } from "../hooks/useTargetData";
import type { MetricFamily, MetricQualityIssue, ChartKind, Series } from "../types";

export function FamilyRow({
  family,
  summary,
  issues,
  expanded,
  onToggleExpand,
  scrub,
  previousValue,
  pinned,
  onTogglePin,
  seriesByMetric,
  domain,
}: {
  family: MetricFamily;
  summary: FamilySummary;
  issues: MetricQualityIssue[];
  expanded: boolean;
  onToggleExpand: () => void;
  scrub: ScrubView | null;
  previousValue: (key: string) => PreviousValue | null;
  pinned: string[];
  onTogglePin: (metric: string) => void;
  seriesByMetric: Record<string, Series[]>;
  domain: [number, number] | null;
}) {
  const [issuesOpen, setIssuesOpen] = useState(false);
  const distribution = family.type === "histogram" || family.type === "summary";
  const metric = chartMetric(family);
  const kind = chartKind(metric, family.type);
  const isPinned = pinned.includes(metric);
  const badge = issues.length ? (
    <QualityBadge issues={issues} onToggle={() => setIssuesOpen((open) => !open)} open={issuesOpen} />
  ) : null;

  const scalarRow = summary.scalar ? (resolveRows(family, scrub, previousValue)[0] ?? null) : null;

  return (
    <div className="border-b border-edge">
      <div className="flex min-w-0 items-baseline gap-2 px-2">
        <button
          aria-expanded={expanded}
          className="flex min-w-0 flex-1 items-baseline gap-2 overflow-hidden py-1.5 text-left hover:bg-fg/[0.06]"
          onClick={onToggleExpand}
          type="button"
        >
          <span aria-hidden="true" className="w-3 shrink-0 text-[11px] text-muted">
            {expanded ? "▾" : "▸"}
          </span>
          <span className="min-w-0 truncate text-[13px] font-medium">{family.name}</span>
          <TypeBadge type={family.type} unit={summary.unit} />
          {isPinned ? (
            <span className="shrink-0 text-[11px] text-accent" title="pinned to dashboard">
              ◆
            </span>
          ) : null}
          {!summary.scalar && summary.labelKeys.length ? (
            <span className="min-w-0 truncate text-[11px] text-muted" title={summary.labelKeys.join(", ")}>
              {`{${summary.labelKeys.join(",")}}`}
            </span>
          ) : null}
          {!summary.scalar ? (
            <span className="shrink-0 text-[11px] tabular-nums text-muted">
              {summary.seriesCount} series
            </span>
          ) : null}
        </button>
        {badge}
        <span className="hidden min-w-0 truncate text-[11px] text-muted md:block" title={family.help}>
          {family.help}
        </span>
        {scalarRow?.delta ? <span className="shrink-0 text-[11px] text-muted">{scalarRow.delta}</span> : null}
        {summary.scalar ? (
          <span className="shrink-0 text-xs tabular-nums">{displayValue(scalarRow, scrub)}</span>
        ) : null}
      </div>
      {issuesOpen ? <QualityIssueList issues={issues} /> : null}
      {expanded ? (
        <>
          <InlineChart
            domain={domain}
            kind={kind}
            metric={metric}
            onTogglePin={onTogglePin}
            pinned={isPinned}
            scrub={scrub}
            series={seriesByMetric[metric]}
          />
          {summary.scalar ? null : distribution ? (
            <DistributionBody family={family} previousValue={previousValue} scrub={scrub} unit={summary.unit} />
          ) : (
            <SeriesBody family={family} previousValue={previousValue} scrub={scrub} />
          )}
        </>
      ) : null}
    </div>
  );
}

/** Chart for one expanded family; pinning it keeps it on the dashboard below. */
function InlineChart({
  metric,
  kind,
  series,
  scrub,
  domain,
  pinned,
  onTogglePin,
}: {
  metric: string;
  kind: ChartKind;
  series: Series[] | undefined;
  scrub: ScrubView | null;
  domain: [number, number] | null;
  pinned: boolean;
  onTogglePin: (metric: string) => void;
}) {
  const scrubbing = scrub?.active ?? false;
  const resolved = scrubbing ? (scrub?.seriesByMetric?.[metric] ?? series) : series;
  const loading = scrubbing ? !scrub?.seriesByMetric && !series : series === undefined;

  return (
    <div className="mb-3 ml-7 mr-2 mt-1 border border-edge">
      <div className="flex items-baseline gap-2 border-b border-edge px-3 py-1.5">
        <KindLabel kind={kind} />
        <span className="min-w-0 truncate text-[11px] text-muted">{metric}</span>
        <button
          className={`-my-1 ml-auto shrink-0 px-1 py-1 text-[11px] ${
            pinned ? "text-accent hover:text-fg" : "text-muted hover:text-accent"
          }`}
          onClick={() => onTogglePin(metric)}
          type="button"
        >
          {pinned ? "◆ unpin" : "+ pin to dashboard"}
        </button>
      </div>
      <div className="h-40 p-2">
        {loading ? (
          <div className="flex h-full items-center justify-center text-[11px] text-muted">loading history…</div>
        ) : (
          <ChartBody domain={domain} kind={kind} scrubT={scrubbing ? (scrub?.t ?? null) : null} series={resolved ?? []} />
        )}
      </div>
    </div>
  );
}

function TypeBadge({ type, unit }: { type: MetricFamily["type"]; unit: string | null }) {
  return (
    <>
      <span className="shrink-0 text-[11px] uppercase tracking-wider text-muted">{type || "untyped"}</span>
      {unit ? <span className="shrink-0 text-[11px] text-muted">{unit}</span> : null}
    </>
  );
}

function SeriesBody({
  family,
  scrub,
  previousValue,
}: {
  family: MetricFamily;
  scrub: ScrubView | null;
  previousValue: (key: string) => PreviousValue | null;
}) {
  const rows = resolveRows(family, scrub, previousValue);
  return (
    <div className="pb-1">
      {rows.map((row) => (
        <div
          className="flex items-baseline gap-3 px-2 py-1 pl-7 text-xs"
          key={sampleKey(row.metric, row.labels)}
        >
          <span className="min-w-0 flex-1 truncate">
            {row.metric}
            <span className="text-muted">{labelsText(row.labels)}</span>
          </span>
          {row.delta ? <span className="shrink-0 text-[11px] text-muted">{row.delta}</span> : null}
          <span className="shrink-0 tabular-nums">{displayValue(row, scrub)}</span>
        </div>
      ))}
    </div>
  );
}

function DistributionBody({
  family,
  unit,
  scrub,
  previousValue,
}: {
  family: MetricFamily;
  unit: string | null;
  scrub: ScrubView | null;
  previousValue: (key: string) => PreviousValue | null;
}) {
  const groups = groupHistogram(resolveRows(family, scrub, previousValue));
  return (
    <div className="pb-1">
      {groups.map((group) => (
        <GroupRow group={group} key={group.key || "all"} scrub={scrub} unit={unit} />
      ))}
    </div>
  );
}

function GroupRow({
  group,
  unit,
  scrub,
}: {
  group: HistogramGroup;
  unit: string | null;
  scrub: ScrubView | null;
}) {
  const [bucketsOpen, setBucketsOpen] = useState(false);
  const count = group.count?.value ?? null;
  const sum = group.sum?.value ?? null;
  const avg = count !== null && count > 0 && sum !== null ? sum / count : null;
  const total = group.buckets.find((bucket) => bucket.le === "+Inf")?.value ?? count;

  return (
    <>
      <div className="flex items-baseline gap-3 px-2 py-1 pl-7 text-xs">
        <span className="min-w-0 flex-1 truncate">{labelsText(group.labels) || "all series"}</span>
        {group.count?.delta ? <span className="shrink-0 text-[11px] text-muted">{group.count.delta}</span> : null}
        {group.count ? (
          <Stat label="count" value={displayValue(group.count, scrub)} />
        ) : null}
        {group.sum ? (
          <Stat label="sum" value={withUnit(displayValue(group.sum, scrub), sum, unit)} />
        ) : null}
        {avg !== null ? <Stat label="avg" value={withUnit(formatNumber(avg), avg, unit)} /> : null}
      </div>
      {group.quantiles.map((quantile) => (
        <div className="flex items-baseline gap-3 px-2 py-0.5 pl-10 text-[11px] text-muted" key={quantile.q}>
          <span className="min-w-0 flex-1 truncate">q={quantile.q}</span>
          <span className="tabular-nums">
            {quantile.value === null ? "—" : withUnit(formatNumber(quantile.value), quantile.value, unit)}
          </span>
        </div>
      ))}
      {group.buckets.length ? (
        <>
          <button
            aria-expanded={bucketsOpen}
            className="px-2 py-0.5 pl-10 text-[11px] text-muted hover:text-fg"
            onClick={() => setBucketsOpen((open) => !open)}
            type="button"
          >
            buckets · {group.buckets.length} {bucketsOpen ? "▾" : "▸"}
          </button>
          {bucketsOpen ? (
            <ul className="mb-1 ml-10 border-l border-edge pl-3">
              {group.buckets.map((bucket) => {
                const pct =
                  bucket.value !== null && total !== null && total > 0
                    ? Math.round((bucket.value / total) * 100)
                    : null;
                return (
                  <li className="flex items-baseline gap-3 py-0.5 text-[11px] text-muted" key={bucket.le}>
                    <span className="min-w-0 flex-1 truncate">≤ {bucket.le}</span>
                    <span className="tabular-nums">
                      {bucket.value === null ? "—" : formatNumber(bucket.value)}
                      {pct !== null ? ` (${pct}%)` : ""}
                    </span>
                  </li>
                );
              })}
            </ul>
          ) : null}
        </>
      ) : null}
    </>
  );
}

function displayValue(row: ResolvedRow | null, scrub: ScrubView | null) {
  if (!row || row.value === null) {
    return scrub?.active && !scrub.seriesByMetric ? "…" : "—";
  }
  return formatNumber(row.value);
}

function withUnit(text: string, value: number | null, unit: string | null) {
  return unit && value !== null ? `${text}${unit}` : text;
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <span className="shrink-0">
      <span className="text-[11px] text-muted">{label} </span>
      <span className="tabular-nums">{value}</span>
    </span>
  );
}
