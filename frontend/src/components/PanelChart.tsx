import { useMemo } from "react";
import {
  Area,
  AreaChart,
  CartesianGrid,
  Line,
  LineChart,
  ReferenceLine,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import type { TooltipContentProps, TooltipValueType } from "recharts";
import { clockTime, compactNumber, formatNumber } from "../lib/format";
import { buildRows, nameSeries, transformSeries } from "../lib/series";
import type { ChartKind, Series } from "../types";

const TICK = { fill: "var(--muted)", fontSize: 11, fontFamily: "var(--font-mono)" } as const;
const MARGIN = { top: 8, right: 8, left: 0, bottom: 0 } as const;

function chartColor(index: number) {
  return `var(--chart-${(index % 8) + 1})`;
}

/** Legend + time-series chart for one metric, shared by inline charts and dashboard cards. */
export function ChartBody({
  kind,
  series,
  scrubT,
  domain,
}: {
  kind: ChartKind;
  series: Series[];
  scrubT: number | null;
  domain: [number, number] | null;
}) {
  const named = useMemo(() => nameSeries(transformSeries(kind, series)), [kind, series]);
  const rows = useMemo(() => buildRows(named, kind === "gauge" ? null : 0), [named, kind]);

  if (rows.length < 2) {
    return <EmptyChart />;
  }
  return (
    <div className="flex h-full min-h-0 flex-col">
      {named.length > 1 ? (
        <div className="flex flex-wrap gap-x-3 gap-y-1 pb-2">
          {named.map((entry, index) => (
            <span className="flex items-center gap-1.5 text-[11px] text-muted" key={entry.name}>
              <span className="h-1.5 w-1.5" style={{ background: chartColor(index) }} />
              {entry.name}
            </span>
          ))}
        </div>
      ) : null}
      <div className="min-h-0 flex-1">
        {kind === "gauge" ? (
          <GaugeChart domain={domain} named={named.map((entry) => entry.name)} rows={rows} scrubT={scrubT} />
        ) : (
          <StackedChart
            domain={domain}
            named={named.map((entry) => entry.name)}
            rows={rows}
            scrubT={scrubT}
          />
        )}
      </div>
    </div>
  );
}

export function PanelChart({
  title,
  metric,
  kind,
  series,
  scrubT,
  domain,
  onRemove,
}: {
  title?: string;
  metric: string;
  kind: ChartKind;
  series: Series[];
  scrubT: number | null;
  domain: [number, number] | null;
  onRemove?: (() => void) | undefined;
}) {
  const label = title ?? metric;
  return (
    <article className="min-w-0 border border-edge">
      <header className="flex items-baseline gap-2 border-b border-edge px-3 py-1.5">
        <h3 className="min-w-0 truncate text-xs font-medium">{label}</h3>
        {label !== metric ? (
          <span className="hidden min-w-0 truncate text-[11px] text-muted md:inline">{metric}</span>
        ) : null}
        <KindLabel kind={kind} />
        {onRemove ? (
          <button
            className="-my-1 ml-auto shrink-0 px-1 py-1 text-[11px] text-muted hover:text-fg"
            onClick={onRemove}
            type="button"
          >
            unpin ×
          </button>
        ) : null}
      </header>
      <div className="h-44 p-2">
        <ChartBody domain={domain} kind={kind} scrubT={scrubT} series={series} />
      </div>
    </article>
  );
}

export function KindLabel({ kind }: { kind: ChartKind }) {
  return (
    <span className="shrink-0 text-[11px] uppercase tracking-widest text-muted">
      {kind === "counter_rate" ? "rate/s" : "value"}
    </span>
  );
}

function StackedChart({
  rows,
  named,
  scrubT,
  domain,
}: {
  rows: Array<Record<string, number | null>>;
  named: string[];
  scrubT: number | null;
  domain: [number, number] | null;
}) {
  return (
    <ResponsiveContainer height="100%" width="100%">
      <AreaChart data={rows} margin={MARGIN}>
        <CartesianGrid stroke="var(--edge)" strokeDasharray="3 3" vertical={false} />
        <XAxis
          axisLine={{ stroke: "var(--edge)" }}
          dataKey="ts"
          domain={domain ?? ["dataMin", "dataMax"]}
          tick={TICK}
          tickFormatter={(value: number) => clockTime(value)}
          tickLine={false}
          type="number"
        />
        <YAxis axisLine={false} tick={TICK} tickFormatter={compactNumber} tickLine={false} width={44} />
        <Tooltip content={renderTip} cursor={{ stroke: "var(--edge)" }} isAnimationActive={false} />
        {named.map((name, index) => (
          <Area
            dataKey={name}
            fill={chartColor(index)}
            fillOpacity={0.18}
            isAnimationActive={false}
            key={name}
            stackId="a"
            stroke={chartColor(index)}
            strokeWidth={1.25}
            type="monotone"
          />
        ))}
        {scrubT !== null ? <ReferenceLine stroke="var(--accent)" strokeDasharray="2 2" x={scrubT} /> : null}
      </AreaChart>
    </ResponsiveContainer>
  );
}

function GaugeChart({
  rows,
  named,
  scrubT,
  domain,
}: {
  rows: Array<Record<string, number | null>>;
  named: string[];
  scrubT: number | null;
  domain: [number, number] | null;
}) {
  return (
    <ResponsiveContainer height="100%" width="100%">
      <LineChart data={rows} margin={MARGIN}>
        <CartesianGrid stroke="var(--edge)" strokeDasharray="3 3" vertical={false} />
        <XAxis
          axisLine={{ stroke: "var(--edge)" }}
          dataKey="ts"
          domain={domain ?? ["dataMin", "dataMax"]}
          tick={TICK}
          tickFormatter={(value: number) => clockTime(value)}
          tickLine={false}
          type="number"
        />
        <YAxis axisLine={false} tick={TICK} tickFormatter={compactNumber} tickLine={false} width={44} />
        <Tooltip content={renderTip} cursor={{ stroke: "var(--edge)" }} isAnimationActive={false} />
        {named.map((name, index) => (
          <Line
            connectNulls
            dataKey={name}
            dot={false}
            isAnimationActive={false}
            key={name}
            stroke={chartColor(index)}
            strokeWidth={1.25}
            type="monotone"
          />
        ))}
        {scrubT !== null ? <ReferenceLine stroke="var(--accent)" strokeDasharray="2 2" x={scrubT} /> : null}
      </LineChart>
    </ResponsiveContainer>
  );
}

function EmptyChart({ label = "waiting for samples" }: { label?: string }) {
  return <div className="flex h-full items-center justify-center text-[11px] text-muted">{label}</div>;
}

function renderTip(props: TooltipContentProps<TooltipValueType, number | string>) {
  const { active, payload, label } = props;
  if (!active || !payload.length) {
    return null;
  }
  return (
    <div className="border border-edge bg-bg px-2 py-1 font-mono text-[11px]">
      <div className="text-muted">{typeof label === "number" ? clockTime(label) : label}</div>
      {payload.map((entry) => (
        <div className="flex items-center gap-1.5" key={String(entry.dataKey)}>
          <span className="h-1.5 w-1.5" style={{ background: entry.color }} />
          <span className="text-muted">{entry.name}</span>
          <span className="ml-auto pl-3 tabular-nums">{formatNumber(Number(entry.value))}</span>
        </div>
      ))}
    </div>
  );
}
