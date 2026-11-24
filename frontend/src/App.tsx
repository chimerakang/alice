import { useState } from "react";
import { BrowserRouter, Routes, Route, NavLink } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { useWebSocket } from "@/hooks/useWebSocket";
import { useAppStore } from "@/stores/appStore";
import { useFleetStore } from "@/stores/fleetStore";
import { LanguageSwitcher } from "@/components/LanguageSwitcher";
import Dashboard from "@/pages/Dashboard";
import Timeline from "@/pages/Timeline";
import Reviews from "@/pages/Reviews";
import Quality from "@/pages/Quality";
import Checkpoints from "@/pages/Checkpoints";
import Performance from "@/pages/Performance";
import Security from "@/pages/Security";
import Memory from "@/pages/Memory";
import Runtime from "@/pages/Runtime";
import HermesTasks from "@/pages/HermesTasks";
import Fleet from "@/pages/Fleet";
import FleetTask from "@/pages/FleetTask";
import FleetInbox from "@/pages/FleetInbox";
import FleetProjects from "@/pages/FleetProjects";
import { selectInbox } from "@/stores/fleetStore";
import {
  LayoutDashboard,
  Clock,
  MessageSquareText,
  Gauge,
  Camera,
  BarChart3,
  Shield,
  BrainCircuit,
  Activity,
  Pickaxe,
  Wifi,
  WifiOff,
  Ship,
  Inbox,
  FolderGit2,
  Menu,
} from "lucide-react";

const navSections = [
  {
    // The fleet comes first: it is where work is given and decided.
    labelKey: "nav.section_fleet",
    items: [
      { to: "/fleet", icon: Ship, labelKey: "nav.fleet" },
      { to: "/fleet/inbox", icon: Inbox, labelKey: "nav.fleet_inbox" },
      { to: "/fleet/projects", icon: FolderGit2, labelKey: "nav.fleet_projects" },
    ],
  },
  {
    labelKey: "nav.section_overview",
    items: [
      { to: "/", icon: LayoutDashboard, labelKey: "nav.dashboard" },
    ],
  },
  {
    labelKey: "nav.section_development_tracking",
    items: [
      { to: "/issues-runs", icon: Clock, labelKey: "nav.issues_runs" },
      { to: "/run-inspector", icon: Pickaxe, labelKey: "nav.run_inspector" },
      { to: "/reviews", icon: MessageSquareText, labelKey: "nav.reviews" },
    ],
  },
  {
    labelKey: "nav.section_secondary",
    items: [
      { to: "/quality", icon: Gauge, labelKey: "nav.analytics" },
      { to: "/checkpoints", icon: Camera, labelKey: "nav.checkpoints" },
      { to: "/performance", icon: BarChart3, labelKey: "nav.performance" },
      { to: "/runtime", icon: Activity, labelKey: "nav.runtime" },
      { to: "/memory", icon: BrainCircuit, labelKey: "nav.memory" },
      { to: "/security", icon: Shield, labelKey: "nav.security" },
    ],
  },
];

function AppLayout() {
  const { t } = useTranslation();
  const { wsConnected, handleWsEvent, setWsConnected } = useAppStore();
  // How many tasks are waiting on the owner, shown beside the inbox link.
  const waiting = useFleetStore((s) => selectInbox(s.tasks).length);
  // On a phone the sidebar is a drawer, closed until asked for.
  const [menuOpen, setMenuOpen] = useState(false);

  useWebSocket({
    onEvent: (event) => {
      handleWsEvent(event);
      useFleetStore.getState().handleWsEvent(event);
      if (!wsConnected) setWsConnected(true);
    },
    // After a drop, the server replays the fleet events missed since this.
    getFleetAfterId: () => useFleetStore.getState().lastEventId,
  });

  return (
    <div className="min-h-screen flex flex-col md:flex-row">
      {/* Top bar, phones only */}
      <div className="md:hidden flex items-center justify-between border-b border-gray-800/60 bg-black/60 px-4 py-3">
        <span className="text-base font-bold text-white">
          Alice<span className="text-primary ml-1.5 text-xs font-normal">Monitor</span>
        </span>
        <button
          type="button"
          onClick={() => setMenuOpen((v) => !v)}
          aria-label={t("nav.menu")}
          aria-expanded={menuOpen}
          className="rounded-md border border-gray-700 p-1.5 text-gray-300"
        >
          <Menu className="w-5 h-5" />
        </button>
      </div>

      {/* Sidebar */}
      <aside
        className={`${menuOpen ? "flex" : "hidden"} md:flex w-full md:w-56 shrink-0 border-b md:border-b-0 md:border-r border-gray-800/60 bg-black/40 backdrop-blur-sm flex-col`}
      >
        {/* Logo */}
        <div className="p-5 border-b border-gray-800/60">
          <h1 className="text-lg font-bold tracking-tight text-white">
            Alice
            <span className="text-primary ml-1.5 text-sm font-normal">
              Monitor
            </span>
          </h1>
        </div>

        {/* Nav */}
        <nav className="flex-1 py-4 px-3 space-y-4 overflow-y-auto">
          {navSections.map((section) => (
            <div key={section.labelKey} className="space-y-1">
              <div className="px-3 pb-1 text-[10px] font-semibold tracking-wider uppercase text-gray-600">
                {t(section.labelKey)}
              </div>
              {section.items.map(({ to, icon: Icon, labelKey }) => (
                <NavLink
                  key={to}
                  to={to}
                  end={to === "/" || to === "/fleet"}
                  onClick={() => setMenuOpen(false)}
                  className={({ isActive }) =>
                    `flex items-center gap-3 px-3 py-2.5 rounded-lg text-sm transition-all ${
                      isActive
                        ? "bg-primary/10 text-primary border border-primary/20"
                        : "text-gray-400 hover:text-gray-200 hover:bg-white/5"
                    }`
                  }
                >
                  <Icon className="w-4 h-4" />
                  {t(labelKey)}
                  {to === "/fleet/inbox" && waiting > 0 && (
                    <span
                      className="ml-auto rounded-full bg-warning/20 px-1.5 text-[11px] font-semibold tabular-nums text-warning"
                      data-testid="inbox-count"
                    >
                      {waiting}
                    </span>
                  )}
                </NavLink>
              ))}
            </div>
          ))}
        </nav>

        {/* Language Switcher & Connection Status */}
        <div className="space-y-2 p-3 border-t border-gray-800/60">
          <LanguageSwitcher />
          <div className="flex items-center gap-2 text-xs px-3 py-2">
            {wsConnected ? (
              <>
                <Wifi className="w-3.5 h-3.5 text-success" />
                <span className="text-success">{t("common.connected")}</span>
              </>
            ) : (
              <>
                <WifiOff className="w-3.5 h-3.5 text-error" />
                <span className="text-error">{t("common.disconnected")}</span>
              </>
            )}
          </div>
        </div>
      </aside>

      {/* Main Content */}
      <main className="flex-1 min-w-0 overflow-y-auto">
        <div className="max-w-7xl mx-auto p-4 md:p-6">
          <Routes>
            <Route path="/" element={<Dashboard />} />
            <Route path="/fleet" element={<Fleet />} />
            <Route path="/fleet/inbox" element={<FleetInbox />} />
            <Route path="/fleet/projects" element={<FleetProjects />} />
            <Route path="/fleet/tasks/:id" element={<FleetTask />} />
            <Route path="/issues-runs" element={<Timeline />} />
            <Route path="/timeline" element={<Timeline />} />
            <Route path="/reviews" element={<Reviews />} />
            <Route path="/quality" element={<Quality />} />
            <Route path="/checkpoints" element={<Checkpoints />} />
            <Route path="/performance" element={<Performance />} />
            <Route path="/runtime" element={<Runtime />} />
            <Route path="/run-inspector" element={<HermesTasks />} />
            <Route path="/hermes-tasks" element={<HermesTasks />} />
            <Route path="/memory" element={<Memory />} />
            <Route path="/security" element={<Security />} />
          </Routes>
        </div>
      </main>
    </div>
  );
}

export default function App() {
  return (
    <BrowserRouter>
      <AppLayout />
    </BrowserRouter>
  );
}
