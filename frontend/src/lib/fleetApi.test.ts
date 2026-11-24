import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { FleetApiError, fleetApi, setApiToken } from "./fleetApi";

interface Call {
  url: string;
  method: string;
  headers: Record<string, string>;
  body: unknown;
}

let calls: Call[] = [];
let reply: { status: number; body: unknown } = { status: 200, body: {} };

beforeEach(() => {
  calls = [];
  reply = { status: 200, body: {} };
  const store = new Map<string, string>();
  vi.stubGlobal("window", {
    localStorage: {
      getItem: (k: string) => store.get(k) ?? null,
      setItem: (k: string, v: string) => void store.set(k, v),
      removeItem: (k: string) => void store.delete(k),
    },
  });
  vi.stubGlobal("fetch", async (url: string, init: RequestInit = {}) => {
    calls.push({
      url,
      method: init.method ?? "GET",
      headers: (init.headers ?? {}) as Record<string, string>,
      body: init.body ? JSON.parse(String(init.body)) : undefined,
    });
    return {
      ok: reply.status >= 200 && reply.status < 300,
      status: reply.status,
      json: async () => {
        if (reply.body === "not-json") throw new Error("bad json");
        return reply.body;
      },
    } as Response;
  });
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("fleetApi", () => {
  it("builds query strings and unwraps lists", async () => {
    reply.body = { tasks: [{ id: "a" }] };
    const tasks = await fleetApi.listTasks({ project: "my app", status: ["failed", "queued"], limit: 20 });
    expect(tasks).toEqual([{ id: "a" }]);
    expect(calls[0].url).toBe("/api/fleet/tasks?project=my+app&status=failed%2Cqueued&limit=20");
    expect(calls[0].method).toBe("GET");

    reply.body = {};
    expect(await fleetApi.listTasks()).toEqual([]);
    expect(calls[1].url).toBe("/api/fleet/tasks");
    expect(await fleetApi.listProjects()).toEqual([]);
  });

  it("pages events by ID", async () => {
    reply.body = { events: [], next_after_id: 7, has_more: false };
    await fleetApi.getTaskEvents("abc", { afterId: 5, limit: 100, types: ["lifecycle"] });
    expect(calls[0].url).toBe("/api/fleet/tasks/abc/events?after_id=5&limit=100&types=lifecycle");
  });

  it("sends the token with commands only", async () => {
    setApiToken("s3cret");
    await fleetApi.getDigest();
    expect(calls[0].headers.Authorization).toBeUndefined();

    await fleetApi.dispatch({ project_id: "app", goal: "fix it", budget_usd: 2 });
    expect(calls[1]).toMatchObject({
      url: "/api/fleet/tasks",
      method: "POST",
      body: { project_id: "app", goal: "fix it", budget_usd: 2 },
    });
    expect(calls[1].headers.Authorization).toBe("Bearer s3cret");

    await fleetApi.act("abc", "reply", { text: "用方案 B" });
    expect(calls[2].url).toBe("/api/fleet/tasks/abc/reply");
    expect(calls[2].body).toEqual({ text: "用方案 B" });
    expect(calls[2].headers.Authorization).toBe("Bearer s3cret");

    await fleetApi.updateProject("app", { trust_mode: "local-only", max_concurrent: 2 });
    expect(calls[3].method).toBe("PUT");
    expect(calls[3].headers.Authorization).toBe("Bearer s3cret");

    // Without a token, none is sent; the server decides.
    setApiToken("");
    await fleetApi.act("abc", "cancel");
    expect(calls[4].headers.Authorization).toBeUndefined();
    expect(calls[4].body).toEqual({});
  });

  it("turns a refusal into an error with the server's code", async () => {
    reply = { status: 409, body: { error: "task abc cannot go from queued to done", code: "conflict" } };
    const err = await fleetApi.act("abc", "approve").catch((e) => e);
    expect(err).toBeInstanceOf(FleetApiError);
    expect(err.status).toBe(409);
    expect(err.code).toBe("conflict");
    expect(err.message).toContain("cannot go from");
    expect(err.unauthorized).toBe(false);

    reply = { status: 401, body: { error: "a valid API token is required", code: "unauthorized" } };
    expect((await fleetApi.act("abc", "cancel").catch((e) => e)).unauthorized).toBe(true);

    reply = { status: 503, body: { error: "the fleet is not enabled", code: "disabled" } };
    expect((await fleetApi.getDigest().catch((e) => e)).disabled).toBe(true);

    // Not JSON at all, e.g. a proxy's error page.
    reply = { status: 502, body: "not-json" };
    const proxy = await fleetApi.getDigest().catch((e) => e);
    expect(proxy.status).toBe(502);
    expect(proxy.message).toBe("HTTP 502");
  });

  it("reports a network failure as an error, not a crash", async () => {
    vi.stubGlobal("fetch", async () => {
      throw new Error("connection refused");
    });
    const err = await fleetApi.getDigest().catch((e) => e);
    expect(err).toBeInstanceOf(FleetApiError);
    expect(err.code).toBe("network");
  });
});
