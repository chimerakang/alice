import { useEffect, useState } from "react";

/**
 * The current time, refreshed on an interval, so that "elapsed" and
 * "waiting" figures move on their own instead of only when data arrives.
 */
export function useNow(intervalMs = 10_000): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), intervalMs);
    return () => clearInterval(timer);
  }, [intervalMs]);
  return now;
}
