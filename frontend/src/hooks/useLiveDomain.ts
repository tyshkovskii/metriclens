import { useEffect, useMemo, useState } from "react";

/** Domain endpoints snap to this grid so every mount computes the same window. */
const QUANTUM_MS = 5000;
const CHECK_MS = 1000;

function quantizedNow() {
  return Math.floor(Date.now() / QUANTUM_MS) * QUANTUM_MS;
}

/**
 * Sliding live window: the last `spanMs`, ending at the wall clock quantized
 * to 5s. Pure clock, no data involved — so every tab derives the identical
 * domain and the timeline axis stays put across tab switches, instead of
 * jumping with whichever series the tab happens to watch.
 */
export function useLiveDomain(spanMs: number): [number, number] {
  const [now, setNow] = useState(quantizedNow);

  useEffect(() => {
    // Checking faster than the quantum keeps the window within ~1s of the
    // grid; setNow with an unchanged value skips the re-render entirely.
    const timer = window.setInterval(() => setNow(quantizedNow()), CHECK_MS);
    return () => window.clearInterval(timer);
  }, []);

  return useMemo(() => [now - spanMs, now], [now, spanMs]);
}
