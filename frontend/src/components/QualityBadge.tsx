import type { MetricQualityIssue } from "../types";

const DOT: Record<MetricQualityIssue["severity"], string> = {
  warning: "bg-warn",
  info: "bg-muted",
};

export function worstSeverity(issues: MetricQualityIssue[]): MetricQualityIssue["severity"] {
  if (issues.some((issue) => issue.severity === "warning")) {
    return "warning";
  }
  return "info";
}

export function QualityBadge({
  issues,
  open,
  onToggle,
}: {
  issues: MetricQualityIssue[];
  open: boolean;
  onToggle: () => void;
}) {
  return (
    <button
      aria-expanded={open}
      className={`-m-1.5 flex items-center gap-1 p-1.5 text-[11px] tabular-nums ${open ? "text-fg" : "text-muted hover:text-fg"}`}
      onClick={onToggle}
      title={`${issues.length} quality issue${issues.length === 1 ? "" : "s"}`}
      type="button"
    >
      <span className={`h-1.5 w-1.5 rounded-full ${DOT[worstSeverity(issues)]}`} />
      {issues.length}
    </button>
  );
}

export function QualityIssueList({ issues }: { issues: MetricQualityIssue[] }) {
  return (
    <ul className="mx-2 mb-2 border-l border-edge pl-3">
      {issues.map((issue) => (
        <li className="py-1 text-[11px]" key={`${issue.metric}:${issue.message}`}>
          <span
            className={`mr-2 uppercase tracking-widest ${
              issue.severity === "warning" ? "text-warn" : "text-muted"
            }`}
          >
            {issue.severity}
          </span>
          {issue.message}
          {issue.suggestion ? <span className="text-muted"> — {issue.suggestion}</span> : null}
        </li>
      ))}
    </ul>
  );
}
