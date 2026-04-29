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
});
