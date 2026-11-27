import { renderToStaticMarkup } from "react-dom/server";
import { beforeAll, describe, expect, it } from "vitest";
import i18n from "@/i18n";
import type { FleetDelivery } from "@/types/fleet";
import FleetAcceptancePanel from "./FleetAcceptancePanel";

beforeAll(async () => {
  await i18n.changeLanguage("zh-TW");
});

const base = {
  task_id: "t1",
  status: "review_ready",
  verification: "not_run",
  note: "",
  acceptance_items: [],
  acceptance: {
    structured: false, version: 0, approved: 0, total: 0, rounds: 0, external_wait: false, gate_ok: true,
    gate_code: "", gate_open: [], evidence_only: false, proposal: null, rejected_reports: [], verification_skipped: false,
  },
} as unknown as FleetDelivery;

describe("FleetAcceptancePanel", () => {
  it("says there is no item-by-item record for an older task and offers to fill it in", () => {
    const html = renderToStaticMarkup(<FleetAcceptancePanel delivery={base} onChange={() => {}} />);
    expect(html).toContain("尚無逐項紀錄");
    expect(html).toContain('data-testid="fleet-acceptance-empty"');
  });

  it("shows evidence, blockers, unreported items, progress and the closing gate", () => {
    const d = {
      ...base,
      verification: "passed",
      acceptance: { ...base.acceptance, structured: true, version: 2, approved: 1, total: 3, external_wait: true, gate_ok: false, gate_code: "items_not_approved", gate_open: ["item-2", "item-3"], verification_skipped: true },
      acceptance_items: [
        { id: "item-1", text: "a", version: 1, report: "evidence_submitted", evidence: [{ type: "pr", ref: "https://x/pull/1", paths: [], note: "" }], blocker: null, round: 1, decision: "approved", note: "", decided_by: "me", reopened: "", history: [] },
        { id: "item-2", text: "b", version: 1, report: "awaiting_external", evidence: [], blocker: { missing: "人工評分", owner: "老師" }, round: 1, decision: "awaiting_external", note: "等老師", decided_by: "me", reopened: "", history: [] },
        { id: "item-3", text: "c", version: 2, report: "not_reported", evidence: [], blocker: null, round: 0, decision: "pending", note: "", decided_by: "", reopened: "criteria_changed", history: [] },
      ],
    } as unknown as FleetDelivery;
    const html = renderToStaticMarkup(<FleetAcceptancePanel delivery={d} onChange={() => {}} />);
    for (const s of ["已核准 1 / 3", "尚未回報", "人工評分", "老師", "item-2, item-3", "略過自動驗證送審", "條件已變更", "https://x/pull/1"]) {
      expect(html).toContain(s);
    }
    expect(html).toContain('data-testid="fleet-acceptance-auto"');
  });
});
