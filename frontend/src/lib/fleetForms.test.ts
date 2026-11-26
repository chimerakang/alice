import { describe, expect, it } from "vitest";
import { DEFAULT_CREATE_ISSUE, DEFAULT_GITHUB_KIND, MAX_BATCH, buildDispatchRequests, buildEpicRequest, projectDraft, projectRequest, taskFieldsDisabled, withCreatedEpic } from "@/lib/fleetForms";
import type { FleetProject } from "@/types/fleet";

const base = { mode: "single" as const, project: "app", shape: "ship" as const, model: "", budget: "", goal: "", batch: "", issues: [] as number[] };

describe("buildDispatchRequests", () => {
  it("builds one request for a single task, with its options", () => {
    expect(buildDispatchRequests({ ...base, goal: "  fix it  ", model: " opus ", budget: "$2.50", shape: "scout" })).toEqual({
      requests: [{ project_id: "app", shape: "scout", model: "opus", budget_usd: 2.5, goal: "fix it" }],
      error: "",
    });
    // Options left empty are not sent, so the server's defaults apply.
    expect(buildDispatchRequests({ ...base, goal: "fix it" }).requests).toEqual([{ project_id: "app", shape: "ship", goal: "fix it" }]);
  });

  it("builds one request per line of a batch", () => {
    const built = buildDispatchRequests({ ...base, mode: "batch", batch: "1. 更新 README\n\n- 補上測試\nfix lint", budget: "3" });
    expect(built.error).toBe("");
    expect(built.requests.map((r) => r.goal)).toEqual(["更新 README", "補上測試", "fix lint"]);
    expect(built.requests.every((r) => r.budget_usd === 3 && r.project_id === "app")).toBe(true);
  });

  it("builds one request per picked issue", () => {
    const built = buildDispatchRequests({ ...base, mode: "issues", issues: [12, 15] });
    expect(built.requests).toEqual([
      { project_id: "app", shape: "ship", issue: 12 },
      { project_id: "app", shape: "ship", issue: 15 },
    ]);
  });

  it("refuses what cannot be dispatched", () => {
    expect(buildDispatchRequests({ ...base, project: "", goal: "x" }).error).toBe("project");
    expect(buildDispatchRequests({ ...base, goal: "   " }).error).toBe("empty");
    expect(buildDispatchRequests({ ...base, mode: "batch", batch: "\n \n" }).error).toBe("empty");
    expect(buildDispatchRequests({ ...base, mode: "issues" }).error).toBe("empty");
    for (const budget of ["0", "-2", "lots"]) {
      expect(buildDispatchRequests({ ...base, goal: "x", budget }).error).toBe("budget");
    }
    const many = Array.from({ length: MAX_BATCH + 1 }, (_, i) => `task ${i}`).join("\n");
    expect(buildDispatchRequests({ ...base, mode: "batch", batch: many }).error).toBe("too_many");
    expect(buildDispatchRequests({ ...base, mode: "batch", batch: many.split("\n").slice(1).join("\n") }).error).toBe("");
  });
});

describe("project form", () => {
  const project = { id: "app", name: "App", default_branch: "main", trust_mode: "local-only", verify_commands: [], max_concurrent: 2 } as unknown as FleetProject;

  it("shows the current target branch and sends the edited one", () => {
    const draft = projectDraft(project);
    expect(draft.default_branch).toBe("main");
    const built = projectRequest({ ...draft, default_branch: "  develop " });
    expect(built.error).toBe("");
    expect(built.req?.default_branch).toBe("develop");
  });

  it("sends an empty target branch when it is cleared, which leaves it unchanged", () => {
    expect(projectRequest({ ...projectDraft(project), default_branch: "" }).req?.default_branch).toBe("");
  });
});

describe("buildDispatchRequests with GitHub options", () => {
  it("starts with create issue checked", () => {
    expect(DEFAULT_CREATE_ISSUE).toBe(true);
  });

  const github = { createIssue: true, milestone: " v2 ", epicIssue: 181 };

  it("sends create_issue, milestone and epic_issue for a single task", () => {
    expect(buildDispatchRequests({ ...base, goal: "fix it", github }).requests).toEqual([
      { project_id: "app", shape: "ship", goal: "fix it", create_issue: true, milestone: "v2", epic_issue: 181 },
    ]);
    // Milestone and epic are optional.
    expect(buildDispatchRequests({ ...base, goal: "fix it", github: { createIssue: true, milestone: "", epicIssue: null } }).requests).toEqual([
      { project_id: "app", shape: "ship", goal: "fix it", create_issue: true },
    ]);
  });

  it("sends none of them when unchecked, exactly as before", () => {
    const off = { createIssue: false, milestone: "v2", epicIssue: 181 };
    expect(buildDispatchRequests({ ...base, goal: "fix it", github: off }).requests).toEqual(
      buildDispatchRequests({ ...base, goal: "fix it" }).requests,
    );
    expect(buildDispatchRequests({ ...base, goal: "fix it", github: off }).requests[0]).not.toHaveProperty("create_issue");
  });

  it("leaves batch and issues modes alone", () => {
    const batch = buildDispatchRequests({ ...base, mode: "batch", batch: "a\nb", github }).requests;
    expect(batch).toEqual([
      { project_id: "app", shape: "ship", goal: "a" },
      { project_id: "app", shape: "ship", goal: "b" },
    ]);
    expect(buildDispatchRequests({ ...base, mode: "issues", issues: [3], github }).requests).toEqual([
      { project_id: "app", shape: "ship", issue: 3 },
    ]);
  });
});

describe("epic creation", () => {
  it("assembles the request from the description and milestone", () => {
    expect(buildEpicRequest({ project: "app", goal: " Plan\nmore ", milestone: " v2 " })).toEqual({
      request: { project_id: "app", description: "Plan\nmore", milestone: "v2" },
      error: "",
    });
    expect(buildEpicRequest({ project: "app", goal: "Plan", milestone: "" }).request).toEqual({ project_id: "app", description: "Plan" });
  });

  it("refuses an empty description or no project", () => {
    expect(buildEpicRequest({ project: "app", goal: "  ", milestone: "" }).error).toBe("empty");
    expect(buildEpicRequest({ project: "", goal: "x", milestone: "" }).error).toBe("project");
  });

  it("disables the task-only fields for an epic, in single mode only", () => {
    expect(taskFieldsDisabled("epic", "single")).toBe(true);
    expect(taskFieldsDisabled("issue", "single")).toBe(false);
    expect(taskFieldsDisabled("epic", "batch")).toBe(false);
    expect(DEFAULT_GITHUB_KIND).toBe("issue");
  });

  it("puts the new epic in the list so it can be preselected", () => {
    const existing = [{ number: 1, title: "old", labels: [], milestone: "", url: "", task_id: "", is_epic: true }];
    const created = { number: 7, title: "new", url: "u" };
    expect(withCreatedEpic(existing, created).map((e) => e.number)).toEqual([1, 7]);
    // Already reloaded: no duplicate.
    expect(withCreatedEpic(withCreatedEpic(existing, created), created)).toHaveLength(2);
  });
});
