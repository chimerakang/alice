import { BrowserRouter, Routes, Route, NavLink } from "react-router-dom";
import { useWebSocket } from "@/hooks/useWebSocket";
import { useAppStore } from "@/stores/appStore";
import Dashboard from "@/pages/Dashboard";
import Timeline from "@/pages/Timeline";
import Checkpoints from "@/pages/Checkpoints";
import Performance from "@/pages/Performance";
import Security from "@/pages/Security";
import {
  LayoutDashboard,
  Clock,
  Camera,
  BarChart3,
  Shield,
  Wifi,
  WifiOff,
} from "lucide-react";

const navItems = [
  { to: "/", icon: LayoutDashboard, label: "Dashboard" },
  { to: "/timeline", icon: Clock, label: "Timeline" },
  { to: "/checkpoints", icon: Camera, label: "Checkpoints" },
  { to: "/performance", icon: BarChart3, label: "Performance" },
  { to: "/security", icon: Shield, label: "Security" },
];

function AppLayout() {
  const { wsConnected, handleWsEvent, setWsConnected } = useAppStore();

  useWebSocket({
    onEvent: (event) => {
      handleWsEvent(event);
      if (!wsConnected) setWsConnected(true);
    },
  });

  return (
    <div className="min-h-screen flex">
      {/* Sidebar */}
      <aside className="w-56 shrink-0 border-r border-gray-800/60 bg-black/40 backdrop-blur-sm flex flex-col">
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
        <nav className="flex-1 py-4 px-3 space-y-1">
          {navItems.map(({ to, icon: Icon, label }) => (
            <NavLink
              key={to}
              to={to}
              end={to === "/"}
              className={({ isActive }) =>
                `flex items-center gap-3 px-3 py-2.5 rounded-lg text-sm transition-all ${
                  isActive
                    ? "bg-primary/10 text-primary border border-primary/20"
                    : "text-gray-400 hover:text-gray-200 hover:bg-white/5"
                }`
              }
            >
              <Icon className="w-4 h-4" />
              {label}
            </NavLink>
          ))}
        </nav>

        {/* Connection Status */}
        <div className="p-4 border-t border-gray-800/60">
          <div className="flex items-center gap-2 text-xs">
            {wsConnected ? (
              <>
                <Wifi className="w-3.5 h-3.5 text-success" />
                <span className="text-success">Connected</span>
              </>
            ) : (
              <>
                <WifiOff className="w-3.5 h-3.5 text-error" />
                <span className="text-error">Disconnected</span>
              </>
            )}
          </div>
        </div>
      </aside>

      {/* Main Content */}
      <main className="flex-1 overflow-y-auto">
        <div className="max-w-7xl mx-auto p-6">
          <Routes>
            <Route path="/" element={<Dashboard />} />
            <Route path="/timeline" element={<Timeline />} />
            <Route path="/checkpoints" element={<Checkpoints />} />
            <Route path="/performance" element={<Performance />} />
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
