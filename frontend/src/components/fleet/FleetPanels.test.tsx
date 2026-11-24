import { renderToStaticMarkup } from "react-dom/server";
import type { ReactNode } from "react";
import { beforeAll, describe, expect, it, vi } from "vitest";
import i18n from "@/i18n";
import type { FleetAuditEntry, FleetCosts, FleetTask } from "@/types/fleet";

vi.mock("react-router-dom", () => ({
  Link: ({ to, children, ...rest }: { to: string; children: ReactNode }) => (
    <a href={to} {...rest}>
      {children}
    </a>
  ),
}));

import { FleetCostView } from "./FleetCostPanel";
import { FleetAuditTable } from "./FleetAuditPanel";

beforeAll(async () => {
  await i18n.changeLanguage("zh-TW");
});

const task = (over: Partial<FleetTask>) =>
  ({ id: "aaaaaaaaaaaa", project_id: "app", project_name: "My App", goal: "修好登入測試\n細節", cost_usd: 0, status_reason: "", ...over }) as FleetTask;

describe("FleetCostView", () => {
  const costs: FleetCosts = {
    day_start: null,
    period_start: null,
    today: { input_tokens: 0, output_tokens: 0, cost_usd: 6.25 },
    period: { input_tokens: 0, output_tokens: 0, cost_usd: 41.5 },
    daily_limit_usd: 20,
    projects: [
      { project_id: "app", name: "My App", today: { input_tokens: 0, output_tokens: 0, cost_usd: 6 }, period: { input_tokens: 0, output_tokens: 0, cost_usd: 30 }, daily_limit_usd: 5, task_limit_usd: 2 },
      { project_id: "web", name: "", today: { input_tokens: 0, output_tokens: 0, cost_usd: 0.25 }, period: null, daily_limit_usd: 0, task_limit_usd: 0 },
    ],
    tasks: [task({ id: "dear", cost_usd: 6 }), task({ id: "cheap", cost_usd: 0.25, project_name: "", project_id: "web", goal: "small one" })],
    overruns: [task({ id: "dear", status_reason: "over the task budget: $6.00 spent of a $2.00 limit" })],
  };

  it("groups cost by project and by task", () => {
    const html = renderToStaticMarkup(<FleetCostView costs={costs} days={7} />);
    expect(html).toContain("$6.25");
    expect(html).toContain("$41.50");
    expect(html).toContain("近 7 天");

    const projects = html.slice(html.indexOf('data-testid="fleet-cost-projects"'), html.indexOf('data-testid="fleet-cost-tasks"'));
    expect(projects).toContain("My App");
    expect(projects).toContain("$30.00");
    expect(projects).toContain(">web<"); // falls back to the ID when unnamed

    const tasks = html.slice(html.indexOf('data-testid="fleet-cost-tasks"'), html.indexOf('data-testid="fleet-cost-overruns"'));
    expect(tasks.indexOf("$6.00")).toBeLessThan(tasks.indexOf("$0.25"));
    expect(tasks).toContain('href="/fleet/tasks/dear"');
    expect(tasks).toContain("修好登入測試");
    expect(tasks).not.toContain("細節");
  });

  it("shows how much of each budget is used, and when one is exceeded", () => {
    const html = renderToStaticMarkup(<FleetCostView costs={costs} days={7} />);
    // Fleet: 6.25 of 20 is 31%, within the limit.
    expect(html).toContain("31% · $6.25 / $20.00");
    // The project: 6 of 5 is over.
    expect(html).toContain("120% · $6.00 / $5.00");
    expect(html).toMatch(/data-over="true"[^]*120%/);
    // No limit set: said so, not shown as 0%.
    expect(html).toContain("未設上限");
    expect(html).not.toContain("NaN");
    expect(html).not.toContain("Infinity");
  });

  it("lists the tasks a budget stopped, with the reason", () => {
    const html = renderToStaticMarkup(<FleetCostView costs={costs} days={7} />);
    const overruns = html.slice(html.indexOf('data-testid="fleet-cost-overruns"'));
    expect(overruns).toContain("超限紀錄");
    expect(overruns).toContain("over the task budget: $6.00 spent of a $2.00 limit");

    const none = renderToStaticMarkup(<FleetCostView costs={{ ...costs, tasks: [], overruns: [] }} days={1} />);
    expect(none).toContain("沒有因預算而停下的任務");
    expect(none).toContain("這段期間沒有產生成本的任務");
  });
});

describe("FleetAuditTable", () => {
  const entries: FleetAuditEntry[] = [
    { id: 3, actor: "42", via: "web", command: "approve", project_id: "app", task_id: "aaaaaaaaaaaa", detail: "", ok: false, error: "task has nothing awaiting approval", created_at: "2026-01-01T10:02:00Z" },
    { id: 2, actor: "42", via: "telegram", command: "dispatch", project_id: "app", task_id: "aaaaaaaaaaaa", detail: "修好登入測試", ok: true, error: "", created_at: "2026-01-01T10:00:00Z" },
    { id: 1, actor: "alice", via: "startup", command: "register_project", project_id: "app", task_id: "", detail: "/repos/app", ok: true, error: "", created_at: "2026-01-01T09:00:00Z" },
  ];

  it("shows who did what, through what, and how it went", () => {
    const html = renderToStaticMarkup(<FleetAuditTable entries={entries} />);
    for (const label of ["操作者", "來源", "指令", "結果", "時間"]) expect(html).toContain(label);
    expect(html).toContain("網頁");
    expect(html).toContain("Telegram");
    expect(html).toContain("啟動程序");
    expect(html).toContain("dispatch");
    expect(html).toContain("修好登入測試");
    expect(html).toContain('href="/fleet/tasks/aaaaaaaaaaaa"');
    // A refusal is shown with its reason.
    expect(html).toContain('data-audit-ok="false"');
    expect(html).toContain("task has nothing awaiting approval");
    expect(html.match(/data-audit-ok="true"/g)).toHaveLength(2);
  });

  it("names an unfamiliar source as it is, and says when there is nothing", () => {
    const odd = renderToStaticMarkup(<FleetAuditTable entries={[{ ...entries[1], via: "cron" }]} />);
    expect(odd).toContain(">cron<");
    expect(renderToStaticMarkup(<FleetAuditTable entries={[]} />)).toContain("沒有符合的紀錄");
  });
});
