export function formatNumber(value: number) {
  if (!Number.isFinite(value)) {
    return String(value);
  }
  return new Intl.NumberFormat(undefined, { maximumFractionDigits: 4 }).format(value);
}

export function compactNumber(value: number) {
  if (!Number.isFinite(value)) {
    return String(value);
  }
  return new Intl.NumberFormat(undefined, { notation: "compact", maximumFractionDigits: 1 }).format(value);
}

export function labelsText(labels: Record<string, string>) {
  const entries = Object.entries(labels).sort(([left], [right]) => left.localeCompare(right));
  if (!entries.length) {
    return "";
  }
  return `{${entries.map(([key, value]) => `${key}="${value}"`).join(",")}}`;
}

export function clockTime(input: number | string | Date) {
  const date = input instanceof Date ? input : new Date(input);
  if (Number.isNaN(date.getTime())) {
    return String(input);
  }
  return date.toLocaleTimeString(undefined, { hour12: false });
}

export function shortTime(input: number | string | Date) {
  const date = input instanceof Date ? input : new Date(input);
  if (Number.isNaN(date.getTime())) {
    return String(input);
  }
  return date.toLocaleTimeString(undefined, { hour12: false, hour: "2-digit", minute: "2-digit" });
}

export function shortDuration(ms: number) {
  const total = Math.max(0, Math.round(ms / 1000));
  const hours = Math.floor(total / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  const seconds = total % 60;
  if (hours) {
    return `${hours}h ${String(minutes).padStart(2, "0")}m`;
  }
  if (minutes) {
    return `${minutes}m ${String(seconds).padStart(2, "0")}s`;
  }
  return `${seconds}s`;
}

export function sampleKey(metric: string, labels: Record<string, string>) {
  return `${metric}${labelsText(labels)}`;
}
