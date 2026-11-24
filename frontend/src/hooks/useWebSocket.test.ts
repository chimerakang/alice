import { describe, expect, it } from "vitest";
import { wsUrl } from "./useWebSocket";

describe("wsUrl", () => {
  it("asks for a replay only when there is something to resume from", () => {
    expect(wsUrl("ws:", "localhost:3939", 0)).toBe("ws://localhost:3939/ws");
    expect(wsUrl("wss:", "alice.example", 42)).toBe("wss://alice.example/ws?fleet_after_id=42");
    expect(wsUrl("ws:", "h", 42.9)).toBe("ws://h/ws?fleet_after_id=42");
  });
});
