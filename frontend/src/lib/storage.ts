/**
 * localStorage wrappers that swallow private-mode / quota errors —
 * persistence is best-effort and the UI must work without it.
 */

/**
 * Every persisted key lives here so the schema is visible in one place.
 * THEME_KEY is also read by the inline bootstrap script in index.html, which
 * cannot import this module — keep the two literals in sync.
 */
export const THEME_KEY = "ml-theme";
export const LAST_TARGET_KEY = "ml-last-target";

export const expandedKey = (targetId: string) => `ml-expanded:${targetId}`;
export const pinsKey = (targetId: string) => `ml-pins:${targetId}`;
export const runtimeKey = (targetId: string) => `ml-runtime:${targetId}`;

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
