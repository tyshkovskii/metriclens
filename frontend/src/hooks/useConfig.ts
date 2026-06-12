import { useEffect, useState } from "react";
import { fetchConfig } from "../api";
import type { AppConfig } from "../types";

/** Fallback matching the backend defaults, used until /api/config answers. */
const DEFAULTS: AppConfig = { scrapeIntervalMs: 5000, retentionMs: 15 * 60_000 };

/**
 * Effective backend timing config. The backend's scrape interval and retention
 * are env-configurable, so the UI fetches them instead of hardcoding the
 * defaults; on fetch failure the defaults stay in place.
 */
export function useConfig(): AppConfig {
  const [config, setConfig] = useState(DEFAULTS);

  useEffect(() => {
    let cancelled = false;
    fetchConfig().then(
      (next) => {
        if (!cancelled && next.scrapeIntervalMs > 0 && next.retentionMs > 0) {
          setConfig(next);
        }
      },
      () => {
        // keep defaults
      },
    );
    return () => {
      cancelled = true;
    };
  }, []);

  return config;
}
