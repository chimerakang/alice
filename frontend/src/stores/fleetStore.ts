import { create } from "zustand";
import { fleetApi, FleetApiError } from "@/lib/fleetApi";
import { needsOwner } from "@/lib/fleet";
import type { WebSocketEvent } from "@/types/alice";
import type { FleetDigest, FleetEvent, FleetProject, FleetTask } from "@/types/fleet";

/** Why the fleet's data could not be loaded, if it could not. */
export type FleetUnavailable = "" | "disabled" | "error";

interface FleetState {
  /** True once the first load has finished, whatever its outcome. */
  loaded: boolean;
  loading: boolean;
  unavailable: FleetUnavailable;
  error: string;

  digest: FleetDigest | null;
  projects: FleetProject[];
  /** Every known task, by ID. */
  tasks: Record<string, FleetTask>;
  /** The highest event ID seen, to resume from after a reconnect. */
  lastEventId: number;

  /** Loads projects, tasks and the digest. */
  load: () => Promise<void>;
  /** Reloads one task after something happened to it. */
  refreshTask: (id: string) => Promise<void>;
  refreshDigest: () => Promise<void>;
  /** Puts a task returned by a command straight into the store. */
  putTask: (task: FleetTask) => void;
  /** Handles a fleet.* WebSocket message. Others are ignored. */
  handleWsEvent: (event: WebSocketEvent) => void;
}

/** Tasks waiting on the owner: the inbox. */
export function selectInbox(tasks: Record<string, FleetTask>): FleetTask[] {
  return Object.values(tasks)
    .filter((t) => needsOwner(t.status))
    .sort((a, b) => (a.updated_at ?? "").localeCompare(b.updated_at ?? ""));
}

// One refresh per task at a time: a burst of events for a task is one fetch.
const pendingRefresh = new Set<string>();
let digestTimer: ReturnType<typeof setTimeout> | null = null;

export const useFleetStore = create<FleetState>()((set, get) => ({
  loaded: false,
  loading: false,
  unavailable: "",
  error: "",
  digest: null,
  projects: [],
  tasks: {},
  lastEventId: 0,

  load: async () => {
    if (get().loading) return;
    set({ loading: true });
    try {
      const [projects, tasks, digest] = await Promise.all([
        fleetApi.listProjects(),
        fleetApi.listTasks({ limit: 500 }),
        fleetApi.getDigest(),
      ]);
      const byId: Record<string, FleetTask> = {};
      for (const task of tasks) byId[task.id] = task;
      set({ projects, tasks: byId, digest, loaded: true, loading: false, unavailable: "", error: "" });
    } catch (err) {
      const disabled = err instanceof FleetApiError && err.disabled;
      set({
        loaded: true,
        loading: false,
        unavailable: disabled ? "disabled" : "error",
        error: err instanceof Error ? err.message : String(err),
      });
    }
  },

  refreshTask: async (id) => {
    if (!id || pendingRefresh.has(id)) return;
    pendingRefresh.add(id);
    try {
      const task = await fleetApi.getTask(id);
      set((s) => ({ tasks: { ...s.tasks, [task.id]: task } }));
    } catch {
      // The task may be gone, or the server briefly away; the next event or
      // a reload brings it back.
    } finally {
      pendingRefresh.delete(id);
    }
  },

  refreshDigest: async () => {
    try {
      set({ digest: await fleetApi.getDigest() });
    } catch {
      // Keep the last digest.
    }
  },

  putTask: (task) => set((s) => ({ tasks: { ...s.tasks, [task.id]: task } })),

  handleWsEvent: (event) => {
    if (typeof event.type !== "string" || !event.type.startsWith("fleet.")) return;
    const data = event.data as Partial<FleetEvent> | null;
    if (!data || typeof data.id !== "number") return;
    // The hub replays its recent messages to a new connection, and a
    // reconnect replays what was missed: the same event can arrive twice.
    if (data.id <= get().lastEventId) return;
    set({ lastEventId: data.id });
    if (data.task_id) void get().refreshTask(data.task_id);
    // Counts change with statuses; one digest fetch covers a burst.
    if (data.type === "status" && digestTimer === null) {
      digestTimer = setTimeout(() => {
        digestTimer = null;
        void get().refreshDigest();
      }, 500);
    }
  },
}));
