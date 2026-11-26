import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";
import type { FleetDelivery, FleetTask } from "@/types/fleet";
import FleetDecisionContext from "./FleetDecisionContext";

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

const interruptedTask = {
  status_reason: "runner stopped: context canceled",
  session_id: "saved-session",
} as FleetTask;

describe("FleetDecisionContext", () => {
  it("explains the interruption and distinguishes unrun checks from a failed check", () => {
    const delivery = { verification: "not_run", diff_known: true, files: 1, untracked: 0 } as FleetDelivery;
    const html = renderToStaticMarkup(<FleetDecisionContext task={interruptedTask} delivery={delivery} />);
    expect(html).toContain("fleet.decision_context.interrupted");
    expect(html).toContain("fleet.delivery.verify_not_run");
    expect(html).toContain("fleet.delivery.note_missing");
    expect(html).toContain("fleet.decision_context.reply");
    expect(html).toContain("fleet.decision_context.pass");
  });

  it("still explains the actions when the delivery API is unavailable", () => {
    const html = renderToStaticMarkup(<FleetDecisionContext task={{ ...interruptedTask, session_id: "" }} delivery={null} />);
    expect(html).toContain("fleet.decision_context.checks_unavailable");
    expect(html).toContain("fleet.decision_context.no_session");
    expect(html).toContain("fleet.decision_context.discard");
  });
});
