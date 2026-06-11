/**
 * localStorage wrappers that swallow private-mode / quota errors —
 * persistence is best-effort and the UI must work without it.
 */

export function loadString(key: string): string | null {
  try {
    return window.localStorage.getItem(key);
  } catch {
    return null;
  }
}

export function saveString(key: string, value: string): void {
  try {
    window.localStorage.setItem(key, value);
  } catch {
    // best-effort
  }
}

export function loadStringArray(key: string): string[] {
  try {
    const parsed: unknown = JSON.parse(window.localStorage.getItem(key) ?? "null");
    return Array.isArray(parsed)
      ? parsed.filter((entry): entry is string => typeof entry === "string")
      : [];
  } catch {
    return [];
  }
}

export function saveStringArray(key: string, value: string[]): void {
  saveString(key, JSON.stringify(value));
}

export function loadFlag(key: string): boolean {
  return loadString(key) === "1";
}

export function saveFlag(key: string, value: boolean): void {
  saveString(key, value ? "1" : "0");
}
