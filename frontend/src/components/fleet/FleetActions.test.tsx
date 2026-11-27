import { renderToStaticMarkup } from "react-dom/server";
import { beforeAll, describe, expect, it } from "vitest";
import i18n from "@/i18n";
import type { FleetTask } from "@/types/fleet";
import FleetActions from "./FleetActions";

beforeAll(async () => {
  await i18n.changeLanguage("zh-TW");
});

const task = (over: Partial<FleetTask>) => ({ id: "t1", status: "needs_decision", session_id: "", actions: ["retry", "reply"], ...over }) as FleetTask;

describe("FleetActions retry vs reply hint", () => {
  it("explains the difference only for a blocked task with a session", () => {
    const withSession = renderToStaticMarkup(<FleetActions task={task({ session_id: "s1" })} />);
    expect(withSession).toContain('data-testid="fleet-retry-vs-reply"');
    expect(withSession).toContain("原 session");
    expect(renderToStaticMarkup(<FleetActions task={task({})} />)).not.toContain("fleet-retry-vs-reply");
    expect(renderToStaticMarkup(<FleetActions task={task({ status: "failed", session_id: "s1" })} />)).not.toContain("fleet-retry-vs-reply");
  });
});
