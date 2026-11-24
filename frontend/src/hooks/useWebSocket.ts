import { useEffect, useRef, useCallback, useState } from "react";
import type { WebSocketEvent } from "@/types/alice";

interface UseWebSocketOptions {
  onEvent?: (event: WebSocketEvent) => void;
  /**
   * The last fleet event ID seen, read at each (re)connect. The server then
   * replays the fleet events missed since, so nothing is lost across a drop.
   */
  getFleetAfterId?: () => number;
  reconnectDelay?: number;
  maxReconnectDelay?: number;
}

/** The WebSocket URL, asking for a replay of fleet events when resuming. */
export function wsUrl(protocol: string, host: string, fleetAfterId: number): string {
  const base = `${protocol}//${host}/ws`;
  return fleetAfterId > 0 ? `${base}?fleet_after_id=${Math.floor(fleetAfterId)}` : base;
}

export function useWebSocket(options: UseWebSocketOptions = {}) {
  const {
    onEvent,
    getFleetAfterId,
    reconnectDelay = 1000,
    maxReconnectDelay = 30000,
  } = options;

  const [isConnected, setIsConnected] = useState(false);
  const wsRef = useRef<WebSocket | null>(null);
  const delayRef = useRef(reconnectDelay);
  const retriesRef = useRef(0);
  const onEventRef = useRef(onEvent);
  onEventRef.current = onEvent;
  const fleetAfterRef = useRef(getFleetAfterId);
  fleetAfterRef.current = getFleetAfterId;

  const connect = useCallback(() => {
    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    const afterId = fleetAfterRef.current?.() ?? 0;
    const url = wsUrl(protocol, window.location.host, afterId);

    const ws = new WebSocket(url);
    wsRef.current = ws;

    ws.onopen = () => {
      setIsConnected(true);
      delayRef.current = reconnectDelay;
      retriesRef.current = 0;
    };

    ws.onmessage = (msg) => {
      try {
        const event = JSON.parse(msg.data) as WebSocketEvent;
        onEventRef.current?.(event);
      } catch {
        // ignore parse errors
      }
    };

    ws.onclose = () => {
      setIsConnected(false);
      wsRef.current = null;
      // Reconnect with exponential backoff
      const delay = Math.min(delayRef.current, maxReconnectDelay);
      delayRef.current = delay * 1.5;
      retriesRef.current += 1;
      setTimeout(connect, delay);
    };

    ws.onerror = () => {
      ws.close();
    };
  }, [reconnectDelay, maxReconnectDelay]);

  useEffect(() => {
    connect();
    return () => {
      wsRef.current?.close();
    };
  }, [connect]);

  const reconnect = useCallback(() => {
    wsRef.current?.close();
    delayRef.current = reconnectDelay;
    setTimeout(connect, 100);
  }, [connect, reconnectDelay]);

  return { isConnected, reconnect };
}
