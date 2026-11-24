import { renderToStaticMarkup } from "react-dom/server";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import App from "./App";

vi.mock("react-router-dom", () => ({
  BrowserRouter: ({ children }: { children: ReactNode }) => <>{children}</>,
  Routes: ({ children }: { children: ReactNode }) => <>{children}</>,
  Route: ({ path, element }: { path: string; element: ReactNode }) => (
    <div data-testid={`route:${path}`}>{element}</div>
  ),
  NavLink: ({
    to,
    children,
  }: {
    to: string;
    children: ReactNode;
  }) => <a href={to}>{children}</a>,
  useNavigate: () => () => {},
}));

vi.mock("@/hooks/useWebSocket", () => ({
  useWebSocket: () => {},
}));

vi.mock("@/stores/appStore", () => ({
  useAppStore: () => ({
    wsConnected: false,
    handleWsEvent: () => {},
    setWsConnected: () => {},
  }),
}));

vi.mock("@/components/LanguageSwitcher", () => ({
  LanguageSwitcher: () => <div data-testid="language-switcher" />,
}));

vi.mock("@/pages/Dashboard", () => ({
  default: () => <div data-testid="dashboard-page" />,
}));

vi.mock("@/pages/Timeline", () => ({
  default: () => <div data-testid="timeline-page" />,
}));

vi.mock("@/pages/Fleet", () => ({
  default: () => <div data-testid="fleet-page" />,
}));

vi.mock("@/pages/FleetTask", () => ({
  default: () => <div data-testid="fleet-task-page" />,
}));

vi.mock("@/pages/FleetInbox", () => ({
  default: () => <div data-testid="fleet-inbox-page" />,
}));

vi.mock("@/pages/FleetProjects", () => ({
  default: () => <div data-testid="fleet-projects-page" />,
}));

vi.mock("@/stores/fleetStore", () => ({
  // Two tasks are waiting on the owner.
  useFleetStore: (selector: (s: { tasks: Record<string, unknown> }) => unknown) => selector({ tasks: {} }),
  selectInbox: () => [{ id: "a" }, { id: "b" }],
}));

vi.mock("@/pages/HermesTasks", () => ({
  default: () => <div data-testid="hermes-tasks-page" />,
}));

vi.mock("@/pages/Reviews", () => ({
  default: () => <div data-testid="reviews-page" />,
}));

vi.mock("@/pages/Quality", () => ({
  default: () => <div data-testid="quality-page" />,
}));

vi.mock("@/pages/Checkpoints", () => ({
  default: () => <div data-testid="checkpoints-page" />,
}));

vi.mock("@/pages/Performance", () => ({
  default: () => <div data-testid="performance-page" />,
}));

vi.mock("@/pages/Security", () => ({
  default: () => <div data-testid="security-page" />,
}));

vi.mock("@/pages/Memory", () => ({
  default: () => <div data-testid="memory-page" />,
}));

describe("App", () => {
  it("wires the reviews page into the sidebar and router", () => {
    const html = renderToStaticMarkup(<App />);

    expect(html).toContain('href="/issues-runs"');
    expect(html).toContain("Issues / Runs");
    expect(html).toContain('data-testid="route:/issues-runs"');
    expect(html).toContain('data-testid="timeline-page"');
    expect(html).toContain('href="/run-inspector"');
    expect(html).toContain("Run Inspector");
    expect(html).toContain('data-testid="route:/run-inspector"');
    expect(html).toContain('data-testid="hermes-tasks-page"');
    expect(html).toContain('href="/reviews"');
    expect(html).toContain("Reviews");
    expect(html).toContain('data-testid="route:/reviews"');
    expect(html).toContain('data-testid="reviews-page"');
    expect(html).toContain('href="/quality"');
    expect(html).toContain('data-testid="route:/quality"');
    expect(html).toContain('href="/memory"');
    expect(html).toContain("Memory");
    expect(html).toContain('data-testid="route:/memory"');
  });

  it("puts the fleet first in the sidebar and routes its pages", () => {
    const html = renderToStaticMarkup(<App />);

    // The fleet board is the first link; the pages that were there stay.
    const firstLink = html.indexOf('href="/');
    expect(html.slice(firstLink, firstLink + 14)).toBe('href="/fleet">');
    expect(html.indexOf('href="/fleet"')).toBeLessThan(html.indexOf('href="/issues-runs"'));
    expect(html).toContain('href="/fleet/inbox"');
    expect(html).toContain('href="/fleet/projects"');

    for (const [route, page] of [
      ["/fleet", "fleet-page"],
      ["/fleet/inbox", "fleet-inbox-page"],
      ["/fleet/projects", "fleet-projects-page"],
      ["/fleet/tasks/:id", "fleet-task-page"],
    ]) {
      expect(html).toContain(`data-testid="route:${route}"`);
      expect(html).toContain(`data-testid="${page}"`);
    }
    // The count of tasks waiting on the owner sits beside the inbox link.
    expect(html).toMatch(/data-testid="inbox-count"[^>]*>2</);
  });
});
