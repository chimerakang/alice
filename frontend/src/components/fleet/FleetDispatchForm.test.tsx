// @vitest-environment jsdom

import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import i18n from "@/i18n";
import type { FleetProject } from "@/types/fleet";

(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const api = vi.hoisted(() => ({
  getProjectGitHub: vi.fn(),
  listProjectMilestones: vi.fn(),
  listProjectIssues: vi.fn(),
  dispatch: vi.fn(),
}));

vi.mock("@/lib/fleetApi", () => ({
  fleetApi: api,
  FleetApiError: class extends Error {},
  getApiToken: () => "",
  setApiToken: () => {},
}));
vi.mock("@/stores/fleetStore", () => ({ useFleetStore: () => () => {} }));

import FleetDispatchForm from "./FleetDispatchForm";

let container: HTMLDivElement;
let root: Root;

beforeAll(async () => {
  await i18n.changeLanguage("zh-TW");
});

beforeEach(() => {
  vi.resetAllMocks();
  api.listProjectMilestones.mockResolvedValue([{ number: 1, title: "v1" }]);
  api.listProjectIssues.mockResolvedValue([]);
  api.dispatch.mockResolvedValue({ id: "t1" });
  container = document.createElement("div");
  document.body.appendChild(container);
  root = createRoot(container);
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

async function render(githubAvailable = true) {
  const project = { id: "app", name: "App", github_available: githubAvailable } as FleetProject;
  await act(async () => root.render(<FleetDispatchForm projects={[project]} onClose={() => {}} />));
}

/** Sets a controlled field the way typing would, so React sees the change. */
async function type(el: HTMLInputElement | HTMLTextAreaElement, value: string) {
  const set = Object.getOwnPropertyDescriptor(Object.getPrototypeOf(el), "value")!.set!;
  await act(async () => {
    set.call(el, value);
    el.dispatchEvent(new Event("input", { bubbles: true }));
  });
}

function expectCreateDisabled(reasonKey: string) {
  const box = container.querySelector<HTMLInputElement>('[data-testid="github-create-disabled"]')!;
  expect(box.disabled).toBe(true);
  expect(box.checked).toBe(false);
  expect(container.textContent).toContain(i18n.t(`fleet.dispatch.${reasonKey}`));
}

async function submitGoal() {
  await type(container.querySelector("textarea")!, "fix it");
  const buttons = container.querySelectorAll("button");
  await act(async () => buttons[buttons.length - 1].click());
}

describe("FleetDispatchForm GitHub section", () => {
  it.each([
    ["no remote", false, null, "github_reason_no_remote"],
    ["gh not logged in", true, { available: false, reason: "auth_required" }, "github_reason_auth_required"],
    ["check failed", true, null, "github_reason_check_failed"],
  ])("disables issue creation and dispatches as text: %s", async (_name, remote, status, reason) => {
    if (status) api.getProjectGitHub.mockResolvedValue(status);
    else api.getProjectGitHub.mockRejectedValue(new Error("boom"));
    await render(remote);
    expectCreateDisabled(reason);
    await submitGoal();
    expect(api.dispatch).toHaveBeenCalledWith({ project_id: "app", shape: "ship", goal: "fix it" });
  });

  it("offers open milestones and accepts a new name", async () => {
    api.getProjectGitHub.mockResolvedValue({ available: true, reason: "" });
    await render();
    const milestone = container.querySelector<HTMLInputElement>('input[list="fleet-milestones"]')!;
    const offered = [...container.querySelectorAll<HTMLOptionElement>("#fleet-milestones option")].map((o) => o.value);
    expect(offered).toEqual(["v1"]);
    await type(milestone, "v2 new");
    await submitGoal();
    expect(api.dispatch).toHaveBeenCalledWith({
      project_id: "app", shape: "ship", goal: "fix it", create_issue: true, milestone: "v2 new",
    });
  });
});
