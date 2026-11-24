import { useEffect } from "react";
import { useFleetStore } from "@/stores/fleetStore";

/** How often the fleet's data is reloaded as a safety net under the live feed. */
const RELOAD_MS = 60_000;

/**
 * Loads the fleet's data on first use and keeps it fresh. Live changes come
 * over the WebSocket; the periodic reload only covers what a missed message
 * would otherwise leave stale.
 */
export function useFleet() {
  const store = useFleetStore();
  const { loaded, load } = store;

  useEffect(() => {
    if (!loaded) void load();
    const timer = setInterval(() => void load(), RELOAD_MS);
    return () => clearInterval(timer);
  }, [loaded, load]);

  return store;
}
