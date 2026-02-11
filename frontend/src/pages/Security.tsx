import { useEffect, useState } from "react";
import { api } from "@/lib/api";
import type { SecurityStats } from "@/types/alice";
import { useAppStore } from "@/stores/appStore";
import { Shield, ShieldAlert, Eye, AlertTriangle } from "lucide-react";

export default function Security() {
  const [stats, setStats] = useState<SecurityStats | null>(null);
  const { securityEvents } = useAppStore();

  useEffect(() => {
    api.getSecurityStats().then(setStats).catch(() => {});
  }, []);

  return (
    <div className="space-y-6 animate-fade-in">
      <h2 className="text-lg font-semibold text-white">Security & Privacy</h2>

      {stats && (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
          <div className="card p-5">
            <div className="flex items-center gap-2 mb-2">
              <Shield className="w-4 h-4 text-primary" />
              <span className="text-sm text-gray-400">Total Events</span>
            </div>
            <div className="text-2xl font-bold font-mono text-white">
              {stats.total_events}
            </div>
          </div>

          <div className="card p-5">
            <div className="flex items-center gap-2 mb-2">
              <ShieldAlert className="w-4 h-4 text-error" />
              <span className="text-sm text-gray-400">Blocked</span>
            </div>
            <div className="text-2xl font-bold font-mono text-white">
              {stats.blocked_attempts}
            </div>
          </div>

          <div className="card p-5">
            <div className="flex items-center gap-2 mb-2">
              <Eye className="w-4 h-4 text-warning" />
              <span className="text-sm text-gray-400">PII Detections</span>
            </div>
            <div className="text-2xl font-bold font-mono text-white">
              {stats.pii_detections}
            </div>
          </div>

          <div className="card p-5">
            <div className="flex items-center gap-2 mb-2">
              <AlertTriangle className="w-4 h-4 text-success" />
              <span className="text-sm text-gray-400">Threat Level</span>
            </div>
            <div className="text-2xl font-bold font-mono text-white capitalize">
              {stats.threat_level}
            </div>
          </div>
        </div>
      )}

      <div className="card p-5">
        <h3 className="text-sm font-semibold text-gray-400 mb-4">
          Recent Security Events
        </h3>
        {securityEvents.length === 0 ? (
          <p className="text-gray-500 text-sm">No security events recorded.</p>
        ) : (
          <div className="space-y-2 max-h-96 overflow-y-auto">
            {securityEvents.map((e, i) => (
              <div
                key={e.event_id || i}
                className="flex items-center justify-between text-sm py-2 border-b border-gray-800"
              >
                <span className="text-gray-300">{e.event_type}</span>
                <span
                  className={
                    e.severity === "SEVERITY_HIGH" || e.severity === "SEVERITY_CRITICAL"
                      ? "text-error"
                      : "text-warning"
                  }
                >
                  {e.severity.replace("SEVERITY_", "")}
                </span>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
