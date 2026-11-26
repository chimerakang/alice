import { renderToStaticMarkup } from "react-dom/server";
import type { ReactNode } from "react";
import { beforeAll, describe, expect, it, vi } from "vitest";
import i18n from "@/i18n";
import type { FleetTask } from "@/types/fleet";

vi.mock("react-router-dom", () => ({
  Link: ({ to, children, ...rest }: { to: string; children: ReactNode }) => (
    <a href={to} {...rest}>
      {children}
    </a>
  ),
  useParams: () => ({ id: "aaaaaaaaaaaa" }),
}));

function task(over: Partial<FleetTask>): FleetTask {
  return {
    id: "aaaaaaaaaaaa", project_id: "app", project_name: "My App", shape: "ship", goal: "修好登入測試\n第二行",
    source_kind: "manual", issue_number: 0, trust_mode: "local-only", model: "", priority: 0, status: "queued",
    status_reason: "", liveness: "unknown", branch: "", worktree_path: "", session_id: "", pr_url: "", pr_number: 0,
    input_tokens: 0, output_tokens: 0, cost_usd: 0, budget_cost_usd: 0, created_at: "2026-01-01T10:00:00Z",
    updated_at: "2026-01-01T10:00:00Z", started_at: null, finished_at: null, last_activity_at: null, ...over,
  };
}

const state = vi.hoisted(() => ({
  current: {} as Record<string, unknown>,
}));

vi.mock("@/hooks/useFleet", () => ({ useFleet: () => state.current }));
vi.mock("@/stores/fleetStore", () => ({
  useFleetStore: (selector: (s: unknown) => unknown) => selector({ putTask: () => {}, load: async () => {} }),
  selectInbox: (tasks: Record<string, FleetTask>) =>
    Object.values(tasks).filter((t) => t.status === "review_ready" || t.status === "needs_decision"),
}));

import Fleet from "./Fleet";
import FleetInbox from "./FleetInbox";
import FleetProjects from "./FleetProjects";
import { projectRequest } from "@/lib/fleetForms";

const loadedState = (tasks: FleetTask[]) => ({
  loaded: true,
  unavailable: "",
  error: "",
  projects: [
    { id: "app", name: "My App", repo_path: "/repos/app", remote: "origin", default_branch: "main", trust_mode: "local-only",
      verify_commands: ["go test ./..."], max_concurrent: 2, branch_prefix: "alice/", budget: { task_cost_usd: 0, daily_cost_usd: 5 },
      topics: [{ chat_id: -100, thread_id: 7 }], created_at: null, updated_at: "2026-01-01T10:00:00Z" },
    { id: "web", name: "Web", repo_path: "/repos/web", remote: "", default_branch: "main", trust_mode: "local-only",
      verify_commands: [], max_concurrent: 1, branch_prefix: "alice/", budget: null, topics: [], created_at: null, updated_at: null },
  ],
  tasks: Object.fromEntries(tasks.map((t) => [t.id, t])),
  digest: {
    totals: { queued: 1, running: 2, review_ready: 1, needs_decision: 1, failed_today: 3, done_today: 4 },
    today: { input_tokens: 0, output_tokens: 0, cost_usd: 12.5 },
    projects: [], pending_wakes: 0, notify_mode: "normal", generated_at: null, day_start: null,
  },
  load: async () => {},
  refreshTask: async () => {},
});

beforeAll(async () => {
  await i18n.changeLanguage("zh-TW");
});

describe("Fleet board", () => {
  it("shows a loading line, then why the fleet is unavailable", () => {
    state.current = { loaded: false, tasks: {}, projects: [] };
    expect(renderToStaticMarkup(<Fleet />)).toContain("載入中");

    state.current = { loaded: true, unavailable: "disabled", error: "", tasks: {}, projects: [] };
    const html = renderToStaticMarkup(<Fleet />);
    expect(html).toContain("Fleet 尚未啟用");
    expect(html).not.toContain('data-testid="fleet-board"');

    state.current = { loaded: true, unavailable: "error", error: "connection refused", tasks: {}, projects: [] };
    expect(renderToStaticMarkup(<Fleet />)).toContain("connection refused");
  });

  it("puts every task in its status column, across projects", () => {
    state.current = loadedState([
      task({ id: "q1", status: "queued" }),
      task({ id: "r1", status: "running", liveness: "busy", project_id: "web", project_name: "Web" }),
      task({ id: "v1", status: "verifying" }),
      task({ id: "rr", status: "review_ready", pr_number: 9 }),
      task({ id: "nd", status: "needs_decision", status_reason: "verification failed 3 times" }),
      task({ id: "d1", status: "done" }),
      task({ id: "f1", status: "failed", liveness: "dead" }),
    ]);
    const html = renderToStaticMarkup(<Fleet />);

    const column = (key: string) => {
      const start = html.indexOf(`data-column="${key}"`);
      const next = html.indexOf("data-column=", start + 1);
      return html.slice(start, next === -1 ? undefined : next);
    };
    expect(column("queued")).toContain('data-task-id="q1"');
    expect(column("active")).toContain('data-task-id="r1"');
    expect(column("active")).toContain('data-task-id="v1"');
    expect(column("review_ready")).toContain('data-task-id="rr"');
    expect(column("needs_decision")).toContain('data-task-id="nd"');
    expect(column("finished")).toContain('data-task-id="d1"');
    expect(column("finished")).toContain('data-task-id="f1"');
    expect(column("queued")).not.toContain('data-task-id="r1"');

    // A card links to its task, shows the goal's first line only, its
    // project, and why it stopped.
    expect(html).toContain('href="/fleet/tasks/nd"');
    expect(html).toContain("修好登入測試");
    expect(html).not.toContain("第二行");
    expect(html).toContain("My App");
    expect(html).toContain("verification failed 3 times");
    expect(html).toContain("#9");
  });

  it("shows the global counts, today's cost and the project filter", () => {
    state.current = loadedState([task({})]);
    const html = renderToStaticMarkup(<Fleet />);
    const counts = html.slice(html.indexOf('data-testid="fleet-counts"'), html.indexOf('data-testid="fleet-board"'));
    for (const text of ["進行中", "排隊", "待審", "待決策", "今日完成", "今日失敗", "今日成本", "$12.50"]) {
      expect(counts).toContain(text);
    }
    expect(counts).toMatch(/>2<\/div>/); // running
    expect(counts).toMatch(/>4<\/div>/); // done today
    expect(html).toContain("全部專案");
    expect(html).toContain('<option value="web">Web</option>');
    // Two tasks wait on the owner: review_ready 1 + needs_decision 1.
    expect(html).toContain("待處理 2");
  });

  it("draws unknown liveness unlike idle", () => {
    state.current = loadedState([
      task({ id: "u1", status: "running", liveness: "unknown" }),
      task({ id: "i1", status: "review_ready", liveness: "idle" }),
      task({ id: "b1", status: "running", liveness: "busy" }),
      task({ id: "x1", status: "running", liveness: "dead" }),
    ]);
    const html = renderToStaticMarkup(<Fleet />);
    const dot = (liveness: string) => {
      // The whole element: its title comes before the attribute.
      const at = html.indexOf(`data-liveness="${liveness}"`);
      return html.slice(html.lastIndexOf("<span", at), at + 260);
    };
    expect(dot("unknown")).toContain("border-dashed");
    expect(dot("idle")).not.toContain("border-dashed");
    expect(dot("idle")).toContain("bg-gray-500");
    expect(dot("busy")).toContain("bg-success");
    expect(dot("dead")).toContain("bg-error");
    // And says so in words, for whoever cannot tell the colours apart.
    expect(dot("unknown")).toContain("不代表閒置");
  });

  it("stacks its columns on a phone and spreads them on wider screens", () => {
    state.current = loadedState([task({})]);
    const html = renderToStaticMarkup(<Fleet />);
    expect(html).toMatch(/data-testid="fleet-board"|class="grid grid-cols-1[^"]*md:grid-cols-3[^"]*xl:grid-cols-5/);
    expect(html).toContain("grid-cols-1");
    expect(html).toContain("xl:grid-cols-5");
  });

  it("points to the projects page when nothing is registered", () => {
    state.current = { ...loadedState([]), projects: [] };
    const html = renderToStaticMarkup(<Fleet />);
    expect(html).toContain("還沒有登記任何專案");
    expect(html).toContain('href="/fleet/projects"');
  });
});

describe("Fleet inbox", () => {
  it("lists what waits on the owner, decisions first, with the actions to take", () => {
    state.current = loadedState([
      task({ id: "rr", status: "review_ready" }),
      task({ id: "nd", status: "needs_decision", status_reason: "agent is waiting for an answer", issue_number: 12 }),
      task({ id: "r1", status: "running" }),
      task({ id: "d1", status: "done" }),
    ]);
    const html = renderToStaticMarkup(<FleetInbox />);
    expect(html).toContain('data-inbox-task="nd"');
    expect(html).toContain('data-inbox-task="rr"');
    expect(html).not.toContain('data-inbox-task="r1"');
    expect(html).not.toContain('data-inbox-task="d1"');
    expect(html.indexOf('data-inbox-task="nd"')).toBeLessThan(html.indexOf('data-inbox-task="rr"'));
    expect(html).toContain("agent is waiting for an answer");
    expect(html).toContain("#12");

    // Each item can be dealt with here.
    const item = (id: string) => {
      const start = html.indexOf(`data-inbox-task="${id}"`);
      const next = html.indexOf("data-inbox-task=", start + 1);
      return html.slice(start, next === -1 ? undefined : next);
    };
    for (const action of ["reply", "retry", "pass", "discard"]) expect(item("nd")).toContain(`data-action="${action}"`);
    for (const action of ["approve", "reply", "discard"]) expect(item("rr")).toContain(`data-action="${action}"`);
    expect(item("rr")).not.toContain('data-action="retry"');
    expect(item("nd")).toContain('href="/fleet/tasks/nd"');
  });

  it("says so when nothing is waiting", () => {
    state.current = loadedState([task({ status: "running" })]);
    expect(renderToStaticMarkup(<FleetInbox />)).toContain("沒有等你處理的任務");
  });
});

describe("Fleet projects", () => {
  it("shows each project's settings and where it is bound", () => {
    state.current = loadedState([]);
    const html = renderToStaticMarkup(<FleetProjects />);
    expect(html).toContain('data-project="app"');
    expect(html).toContain("/repos/app");
    expect(html).toContain("-100/7"); // the bound Telegram topic
    expect(html).toContain("go test ./...");
    for (const label of ["信任模式", "並行上限", "驗收指令", "試跑驗收指令", "工作目錄", "登記"]) expect(html).toContain(label);
    for (const mode of ["local-only", "direct-pr", "no-mistakes"]) expect(html).toContain(`<option value="${mode}"`);
    // The second project has no remote and no topic.
    const web = html.slice(html.indexOf('data-project="web"'));
    expect(web).toContain("無");
  });

  it("validates the form before sending it", () => {
    const base = { name: " App ", default_branch: " develop ", trust_mode: "direct-pr" as const, verify: "go build ./...\n\n  go test ./...  ", max_concurrent: "2", task_budget: "", daily_budget: "$7.5" };
    expect(projectRequest(base)).toEqual({
      req: {
        name: "App", default_branch: "develop", trust_mode: "direct-pr", verify_commands: ["go build ./...", "go test ./..."],
        max_concurrent: 2, budget: { task_cost_usd: 0, daily_cost_usd: 7.5 },
      },
      error: "",
    });
    for (const bad of ["0", "-1", "1.5", "two", ""]) {
      expect(projectRequest({ ...base, max_concurrent: bad }).error).toBe("concurrent");
    }
    for (const bad of ["-1", "lots"]) {
      expect(projectRequest({ ...base, task_budget: bad }).error).toBe("budget");
    }
  });
});
