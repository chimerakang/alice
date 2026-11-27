import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";
import type { FleetDelivery } from "@/types/fleet";
import FleetDeliverySummary from "./FleetDeliverySummary";

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

const base: FleetDelivery = {
  task_id: "t1",
  shape: "ship",
  status: "review_ready",
  approvable: true,
  verification: "passed",
  checks_run: 1,
  checks_attempt: 0,
  checks_previous: false,
  checks: [{ name: "build", command: "go build ./...", passed: true, duration_ms: 1200 }],
  diff_known: true,
  files: 2,
  insertions: 10,
  deletions: 3,
  untracked: 0,
  note: "added the endpoint",
  note_missing: false,
  stop_reason: "",
  runs: [{ number: 1, started_at: null, outcome: "passed", attempt: 0, stop_reason: "" }],
  completed: [],
  pending: [],
  limits: [],
  next: "approve",
} as unknown as FleetDelivery;

const render = (over: Partial<FleetDelivery>) =>
  renderToStaticMarkup(<FleetDeliverySummary delivery={{ ...base, ...over }} />);

describe("FleetDeliverySummary", () => {
  it("shows the delivered work and each check before approval", () => {
    const html = render({});
    expect(html).toContain("fleet.delivery.verify_passed");
    expect(html).toContain("fleet.delivery.checks_of_run");
    expect(html).toContain("go build ./...");
    expect(html).toContain("added the endpoint");
    expect(html).toContain("fleet.delivery.next_approve");
  });

  it("says when no checks are configured and when the note is missing", () => {
    const html = render({ verification: "not_configured", checks: [], note: "", note_missing: true });
    expect(html).toContain("fleet.delivery.verify_not_configured");
    expect(html).toContain("fleet.delivery.note_missing");
    expect(html).not.toContain("fleet-delivery-checks");
  });

  it("marks checks carried over from a previous run", () => {
    const html = render({
      verification: "not_run",
      checks_previous: true,
      stop_reason: "runner stopped: context canceled",
      runs: [
        { number: 1, started_at: null, outcome: "passed", attempt: 0, stop_reason: "" },
        { number: 2, started_at: null, outcome: "not_verified", attempt: 0, stop_reason: "context canceled" },
      ],
    });
    expect(html).toContain("fleet.delivery.verify_not_run");
    expect(html).toContain("fleet.delivery.checks_previous");
    expect(html).not.toContain("fleet.delivery.checks_of_run");
    expect(html).toContain("fleet.delivery.outcome_not_verified");
    expect(html).toContain("runner stopped: context canceled");
  });
});
