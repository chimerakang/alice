import { useAppStore } from "@/stores/appStore";
import { Clock, Wrench, Brain, ShieldAlert, BarChart3 } from "lucide-react";

const eventConfig: Record<
  string,
  { icon: React.ComponentType<{ className?: string }>; color: string; label: string }
> = {
  tool_execution_start: { icon: Wrench, color: "text-primary", label: "Tool Start" },
  tool_execution: { icon: Wrench, color: "text-primary-light", label: "Tool Complete" },
  decision_complete: { icon: Brain, color: "text-cyan-400", label: "Decision" },
  performance_metric: { icon: BarChart3, color: "text-accent", label: "Performance" },
  security_alert: { icon: ShieldAlert, color: "text-error", label: "Security" },
  agent_status: { icon: Clock, color: "text-success", label: "Agent Status" },
};

export default function Timeline() {
  const { wsEvents, wsConnected } = useAppStore();

  return (
    <div className="space-y-4 animate-fade-in">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold text-white">AI Decision Timeline</h2>
        <div className="flex items-center gap-2 text-sm text-gray-400">
          <span
            className={`status-dot ${wsConnected ? "status-dot-success" : "status-dot-error"}`}
          />
          {wsConnected ? "Live" : "Disconnected"}
          <span className="text-gray-600">|</span>
          <span>{wsEvents.length} events</span>
        </div>
      </div>

      {wsEvents.length === 0 ? (
        <div className="card p-12 text-center">
          <Clock className="w-12 h-12 text-gray-600 mx-auto mb-4" />
          <p className="text-gray-400">Waiting for events...</p>
          <p className="text-gray-600 text-sm mt-1">
            Events will appear here in real-time as the AI agent works.
          </p>
        </div>
      ) : (
        <div className="relative pl-8">
          {/* Vertical line */}
          <div className="absolute left-3 top-0 bottom-0 w-0.5 bg-gradient-to-b from-primary via-primary-light to-transparent" />

          {wsEvents.slice(0, 50).map((event, i) => {
            const cfg = eventConfig[event.type] ?? eventConfig.agent_status;
            const Icon = cfg.icon;
            const ts = new Date(event.timestamp).toLocaleTimeString();

            return (
              <div
                key={i}
                className="relative mb-4 animate-slide-up"
                style={{ animationDelay: `${i * 30}ms` }}
              >
                {/* Node dot */}
                <div className="absolute -left-5 top-2 w-3 h-3 rounded-full bg-dark-grey border-2 border-primary z-10" />

                <div className="card p-4 ml-2">
                  <div className="flex items-center gap-2 mb-1">
                    <Icon className={`w-4 h-4 ${cfg.color}`} />
                    <span className={`text-sm font-medium ${cfg.color}`}>
                      {cfg.label}
                    </span>
                    <span className="text-xs text-gray-500 ml-auto font-mono">
                      {ts}
                    </span>
                  </div>
                  <pre className="text-xs text-gray-400 font-mono overflow-hidden max-h-20 mt-1">
                    {JSON.stringify(event.data, null, 2)}
                  </pre>
                </div>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
