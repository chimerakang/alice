import { beforeEach, describe, expect, it, vi } from "vitest";
import type { FleetTask } from "@/types/fleet";

const api = vi.hoisted(() => ({
  listProjects: vi.fn(),
  listTasks: vi.fn(),
  getDigest: vi.fn(),
  getTask: vi.fn(),
}));

vi.mock("@/lib/fleetApi", () => {
  class FleetApiError extends Error {
    code: string;
    constructor(code: string, message: string) {
      super(message);
      this.code = code;
    }
    get disabled() {
      return this.code === "disabled";
    }
  }
  return { fleetApi: api, FleetApiError };
});

import { FleetApiError } from "@/lib/fleetApi";
import { selectInbox, useFleetStore } from "./fleetStore";

function task(id: string, status: FleetTask["status"], updated = "2026-01-01T10:00:00Z"): FleetTask {
  return { id, status, updated_at: updated, project_id: "app" } as FleetTask;
}

const flush = () => new Promise((resolve) => setTimeout(resolve, 0));

beforeEach(() => {
  vi.clearAllMocks();
  useFleetStore.setState({
    loaded: false, loading: false, unavailable: "", error: "", digest: null, projects: [], tasks: {}, lastEventId: 0,
  });
});

describe("fleetStore", () => {
  it("loads projects, tasks and the digest", async () => {
    api.listProjects.mockResolvedValue([{ id: "app" }]);
    api.listTasks.mockResolvedValue([task("a", "queued"), task("b", "failed")]);
    api.getDigest.mockResolvedValue({ pending_wakes: 2 });

    await useFleetStore.getState().load();
    const s = useFleetStore.getState();
    expect(s.loaded).toBe(true);
    expect(s.unavailable).toBe("");
    expect(Object.keys(s.tasks)).toEqual(["a", "b"]);
    expect(s.projects).toHaveLength(1);
    expect(s.digest).toEqual({ pending_wakes: 2 });
  });

  it("tells a disabled fleet from a failure", async () => {
    api.listProjects.mockRejectedValue(new (FleetApiError as unknown as new (c: string, m: string) => Error)("disabled", "the fleet is not enabled"));
    api.listTasks.mockResolvedValue([]);
    api.getDigest.mockResolvedValue({});
    await useFleetStore.getState().load();
    expect(useFleetStore.getState().unavailable).toBe("disabled");

    api.listProjects.mockRejectedValue(new Error("boom"));
    await useFleetStore.getState().load();
    expect(useFleetStore.getState().unavailable).toBe("error");
    expect(useFleetStore.getState().error).toBe("boom");
    expect(useFleetStore.getState().loaded).toBe(true);
  });

  it("refreshes the task a fleet event is about, and remembers the event ID", async () => {
    api.getTask.mockResolvedValue(task("a", "running"));
    const { handleWsEvent } = useFleetStore.getState();

    handleWsEvent({ type: "fleet.status", timestamp: "", data: { id: 5, task_id: "a", type: "status" } });
    await flush();
    expect(api.getTask).toHaveBeenCalledWith("a");
    expect(useFleetStore.getState().tasks.a.status).toBe("running");
    expect(useFleetStore.getState().lastEventId).toBe(5);
  });

  it("ignores events it has already seen, and events that are not the fleet's", async () => {
    api.getTask.mockResolvedValue(task("a", "running"));
    const { handleWsEvent } = useFleetStore.getState();
    handleWsEvent({ type: "fleet.status", timestamp: "", data: { id: 5, task_id: "a", type: "status" } });
    await flush();

    // A replay after reconnecting, and the hub's own recent-message replay.
    handleWsEvent({ type: "fleet.status", timestamp: "", data: { id: 5, task_id: "a", type: "status" } });
    handleWsEvent({ type: "fleet.liveness", timestamp: "", data: { id: 3, task_id: "a", type: "liveness" } });
    handleWsEvent({ type: "tool_execution", timestamp: "", data: { id: 99, task_id: "a" } });
    handleWsEvent({ type: "fleet.status", timestamp: "", data: null });
    handleWsEvent({ type: "fleet.status", timestamp: "", data: { task_id: "a" } });
    await flush();

    expect(api.getTask).toHaveBeenCalledTimes(1);
    expect(useFleetStore.getState().lastEventId).toBe(5);
  });

  it("makes one request for a burst of events about one task", async () => {
    let release: (t: FleetTask) => void = () => {};
    api.getTask.mockReturnValue(new Promise<FleetTask>((resolve) => (release = resolve)));
    const { handleWsEvent } = useFleetStore.getState();
    for (let id = 1; id <= 5; id++) {
      handleWsEvent({ type: "fleet.liveness", timestamp: "", data: { id, task_id: "a", type: "liveness" } });
    }
    expect(api.getTask).toHaveBeenCalledTimes(1);
    release(task("a", "running"));
    await flush();
    expect(useFleetStore.getState().lastEventId).toBe(5);
  });

  it("keeps a task it could not refresh", async () => {
    useFleetStore.getState().putTask(task("a", "queued"));
    api.getTask.mockRejectedValue(new Error("gone"));
    await useFleetStore.getState().refreshTask("a");
    expect(useFleetStore.getState().tasks.a.status).toBe("queued");
  });

  it("selects the inbox: tasks waiting on the owner, longest-waiting first", () => {
    const inbox = selectInbox({
      a: task("a", "running"),
      b: task("b", "needs_decision", "2026-01-01T12:00:00Z"),
      c: task("c", "review_ready", "2026-01-01T11:00:00Z"),
      d: task("d", "done"),
    });
    expect(inbox.map((t) => t.id)).toEqual(["c", "b"]);
  });
});
