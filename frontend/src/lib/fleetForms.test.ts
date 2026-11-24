import { describe, expect, it } from "vitest";
import { MAX_BATCH, buildDispatchRequests } from "@/lib/fleetForms";

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
