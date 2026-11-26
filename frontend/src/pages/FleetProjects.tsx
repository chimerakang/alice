import { useCallback, useEffect, useState } from "react";
import { clsx } from "clsx";
import { useTranslation } from "react-i18next";
import { CheckCircle2, FolderGit2, Plus, XCircle } from "lucide-react";
import FleetUnavailable from "@/components/fleet/FleetUnavailable";
import { FleetApiError, fleetApi, getApiToken, setApiToken } from "@/lib/fleetApi";
import { formatDuration } from "@/lib/fleet";
import { projectDraft, projectRequest, type ProjectDraft } from "@/lib/fleetForms";
import { useFleet } from "@/hooks/useFleet";
import { useFleetStore } from "@/stores/fleetStore";
import type { FleetProject, FleetTrustMode, FleetVerification, FleetWorktree } from "@/types/fleet";

const TRUST_MODES: FleetTrustMode[] = ["local-only", "direct-pr", "no-mistakes"];

const input =
  "w-full rounded-md border border-gray-700 bg-black/40 px-2 py-1.5 text-sm text-gray-100 placeholder:text-gray-600 focus:border-primary/50 focus:outline-none";
const button =
  "rounded-md border border-primary/30 bg-primary/15 px-3 py-1.5 text-sm text-primary hover:bg-primary/25 disabled:opacity-50";

function TokenForm({ onSaved }: { onSaved: () => void }) {
  const { t } = useTranslation();
  const [token, setToken] = useState(getApiToken());
  return (
    <form
      className="flex flex-wrap items-center gap-2"
      onSubmit={(e) => {
        e.preventDefault();
        setApiToken(token.trim());
        onSaved();
      }}
    >
      <input type="password" value={token} onChange={(e) => setToken(e.target.value)} placeholder={t("fleet.token.placeholder")} className={clsx(input, "flex-1 min-w-[12rem]")} />
      <button type="submit" className={button}>
        {t("fleet.token.save")}
      </button>
      <p className="w-full text-xs text-gray-500">{t("fleet.token.hint")}</p>
    </form>
  );
}

function ProjectPanel({ project }: { project: FleetProject }) {
  const { t } = useTranslation();
  const load = useFleetStore((s) => s.load);
  const [draft, setDraft] = useState<ProjectDraft>(() => projectDraft(project));
  const [saving, setSaving] = useState(false);
  const [message, setMessage] = useState<{ ok: boolean; text: string } | null>(null);
  const [needToken, setNeedToken] = useState(false);
  const [trying, setTrying] = useState(false);
  const [results, setResults] = useState<FleetVerification[] | null>(null);
  const [trees, setTrees] = useState<FleetWorktree[] | null>(null);

  const saved = JSON.stringify(projectDraft(project));
  const dirty = JSON.stringify(draft) !== saved;

  useEffect(() => {
    let stale = false;
    fleetApi
      .listProjectWorktrees(project.id)
      .then((list) => !stale && setTrees(list))
      .catch(() => !stale && setTrees([]));
    return () => {
      stale = true;
    };
  }, [project.id, project.updated_at]);

  const fail = useCallback((err: unknown) => {
    if (err instanceof FleetApiError && err.unauthorized) setNeedToken(true);
    setMessage({ ok: false, text: err instanceof Error ? err.message : String(err) });
  }, []);

  async function save() {
    const built = projectRequest(draft);
    if (!built.req) {
      setMessage({ ok: false, text: t(`fleet.projects.error_${built.error}`) });
      return;
    }
    setSaving(true);
    setMessage(null);
    try {
      await fleetApi.updateProject(project.id, built.req);
      await load();
      setMessage({ ok: true, text: t("fleet.projects.saved") });
    } catch (err) {
      fail(err);
    } finally {
      setSaving(false);
    }
  }

  async function tryVerify() {
    setTrying(true);
    setMessage(null);
    setResults(null);
    try {
      setResults(await fleetApi.tryVerify(project.id));
    } catch (err) {
      fail(err);
    } finally {
      setTrying(false);
    }
  }

  const set = <K extends keyof ProjectDraft>(key: K, value: ProjectDraft[K]) => setDraft((d) => ({ ...d, [key]: value }));

  return (
    <section className="rounded-lg border border-gray-800 bg-surface/40 p-4 space-y-4" data-project={project.id}>
      <header className="space-y-1">
        <h2 className="flex items-center gap-2 text-base font-semibold text-white">
          <FolderGit2 className="w-4 h-4 text-primary" />
          {project.name || project.id}
          <code className="text-xs font-normal text-gray-600">{project.id}</code>
        </h2>
        <p className="text-xs font-mono text-gray-500 break-all">{project.repo_path}</p>
        <p className="text-xs text-gray-500">
          {t("fleet.projects.branch")}: <span className="text-gray-300">{project.default_branch}</span>
          {" · "}
          {t("fleet.projects.remote")}: <span className="text-gray-300">{project.remote || t("fleet.projects.no_remote")}</span>
          {" · "}
          {t("fleet.projects.topics")}:{" "}
          <span className="text-gray-300">
            {(project.topics ?? []).length === 0
              ? t("fleet.projects.no_topics")
              : project.topics.map((tp) => `${tp.chat_id}${tp.thread_id ? `/${tp.thread_id}` : ""}`).join(", ")}
          </span>
        </p>
      </header>

      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-6">
        <label className="space-y-1 text-xs text-gray-400">
          {t("fleet.projects.name")}
          <input value={draft.name} onChange={(e) => set("name", e.target.value)} className={input} />
        </label>
        <label className="space-y-1 text-xs text-gray-400">
          {t("fleet.projects.default_branch")}
          <input value={draft.default_branch} onChange={(e) => set("default_branch", e.target.value)} className={clsx(input, "font-mono")} />
        </label>
        <label className="space-y-1 text-xs text-gray-400">
          {t("fleet.projects.trust_mode")}
          <select value={draft.trust_mode} onChange={(e) => set("trust_mode", e.target.value as FleetTrustMode)} className={input}>
            {TRUST_MODES.map((mode) => (
              <option key={mode} value={mode}>
                {t(`fleet.trust.${mode}`)}
              </option>
            ))}
          </select>
        </label>
        <label className="space-y-1 text-xs text-gray-400">
          {t("fleet.projects.max_concurrent")}
          <input value={draft.max_concurrent} onChange={(e) => set("max_concurrent", e.target.value)} inputMode="numeric" className={input} />
        </label>
        <label className="space-y-1 text-xs text-gray-400">
          {t("fleet.projects.task_budget")}
          <input value={draft.task_budget} onChange={(e) => set("task_budget", e.target.value)} inputMode="decimal" placeholder={t("fleet.projects.budget_default")} className={input} />
        </label>
        <label className="space-y-1 text-xs text-gray-400">
          {t("fleet.projects.daily_budget")}
          <input value={draft.daily_budget} onChange={(e) => set("daily_budget", e.target.value)} inputMode="decimal" placeholder={t("fleet.projects.budget_default")} className={input} />
        </label>
      </div>
      <p className="text-xs text-gray-500">{t("fleet.projects.default_branch_help")} {t(`fleet.trust_help.${draft.trust_mode}`)}</p>

      <label className="block space-y-1 text-xs text-gray-400">
        {t("fleet.projects.verify_commands")}
        <textarea value={draft.verify} onChange={(e) => set("verify", e.target.value)} rows={3} placeholder={"go build ./...\ngo test ./..."} className={clsx(input, "font-mono")} />
      </label>

      <div className="flex flex-wrap items-center gap-2">
        <button type="button" onClick={() => void save()} disabled={saving || !dirty} className={button}>
          {saving ? t("fleet.actions.working") : t("common.save")}
        </button>
        <button
          type="button"
          onClick={() => void tryVerify()}
          disabled={trying || dirty || (project.verify_commands ?? []).length === 0}
          title={dirty ? t("fleet.projects.try_save_first") : ""}
          className="rounded-md border border-gray-700 px-3 py-1.5 text-sm text-gray-300 hover:bg-white/5 disabled:opacity-50"
        >
          {trying ? t("fleet.projects.trying") : t("fleet.projects.try_verify")}
        </button>
        {message && <span className={clsx("text-xs break-words", message.ok ? "text-success" : "text-error")}>{message.text}</span>}
      </div>
      {needToken && <TokenForm onSaved={() => { setNeedToken(false); setMessage(null); }} />}

      {results && (
        <div className="space-y-2" data-testid="verify-results">
          {results.map((r) => (
            <div key={r.step_index} className="rounded-md border border-gray-800">
              <div className="flex flex-wrap items-center gap-2 px-3 py-1.5 text-sm">
                {r.passed ? <CheckCircle2 className="w-4 h-4 text-success" /> : <XCircle className="w-4 h-4 text-error" />}
                <code className="min-w-0 flex-1 break-all text-gray-200">{r.command}</code>
                <span className="text-xs text-gray-500 tabular-nums">
                  {t("fleet.task.exit_code", { code: r.exit_code })} · {formatDuration(r.duration_ms)}
                  {r.timed_out && ` · ${t("fleet.task.timed_out")}`}
                </span>
              </div>
              {(r.output_tail || r.error) && (
                <pre className="max-h-56 overflow-auto border-t border-gray-800 bg-black/40 p-3 text-xs text-gray-300 whitespace-pre-wrap break-words">
                  {r.error ? `${r.error}\n` : ""}
                  {r.output_tail}
                </pre>
              )}
            </div>
          ))}
        </div>
      )}

      <div>
        <h3 className="mb-1.5 text-xs font-semibold uppercase tracking-wider text-gray-400">
          {t("fleet.projects.worktrees")} {trees ? `· ${trees.length}` : ""}
        </h3>
        {trees === null && <p className="text-xs text-gray-600">{t("common.loading")}</p>}
        {trees?.length === 0 && <p className="text-xs text-gray-600">{t("fleet.projects.no_worktrees")}</p>}
        <ul className="space-y-1">
          {trees?.map((tree) => (
            <li key={tree.path} className="flex flex-wrap items-center gap-2 text-xs" data-worktree={tree.path}>
              <code className="min-w-0 flex-1 break-all text-gray-400">{tree.path}</code>
              <span className="font-mono text-gray-500">{tree.branch}</span>
              {tree.task_id && <span className="text-gray-500">{tree.task_id} · {tree.task_status}</span>}
              {tree.dirty && <span className="rounded border border-warning/40 bg-warning/10 px-1.5 text-warning">{t("fleet.projects.dirty")}</span>}
              {tree.orphan && <span className="rounded border border-error/40 bg-error/10 px-1.5 text-error">{t("fleet.projects.orphan")}</span>}
              {tree.missing && <span className="rounded border border-gray-600 px-1.5 text-gray-400">{t("fleet.projects.missing")}</span>}
            </li>
          ))}
        </ul>
      </div>
    </section>
  );
}

/** Register repositories and set how the fleet treats each. */
export default function FleetProjects() {
  const { t } = useTranslation();
  const { loaded, unavailable, error, projects, load } = useFleet();
  const [path, setPath] = useState("");
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  const [needToken, setNeedToken] = useState(false);

  if (!loaded) return <p className="text-sm text-gray-500">{t("common.loading")}</p>;
  if (unavailable) return <FleetUnavailable reason={unavailable} error={error} />;

  async function register() {
    setBusy(true);
    setMessage("");
    try {
      await fleetApi.registerProject({ repo_path: path.trim() });
      setPath("");
      await load();
    } catch (err) {
      if (err instanceof FleetApiError && err.unauthorized) setNeedToken(true);
      setMessage(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="space-y-5" data-testid="fleet-projects">
      <div>
        <h1 className="text-2xl font-bold text-white">{t("fleet.projects.title")}</h1>
        <p className="text-sm text-gray-500">{t("fleet.projects.subtitle")}</p>
      </div>

      <div className="rounded-lg border border-gray-800 p-4 space-y-2">
        <div className="flex flex-wrap gap-2">
          <input value={path} onChange={(e) => setPath(e.target.value)} placeholder={t("fleet.projects.path_placeholder")} className={clsx(input, "flex-1 min-w-[14rem] font-mono")} />
          <button type="button" onClick={() => void register()} disabled={busy || path.trim() === ""} className={clsx(button, "inline-flex items-center gap-1.5")}>
            <Plus className="w-4 h-4" />
            {t("fleet.projects.register")}
          </button>
        </div>
        <p className="text-xs text-gray-500">{t("fleet.projects.register_hint")}</p>
        {message && !needToken && <p className="text-xs text-error break-words">{message}</p>}
        {needToken && <TokenForm onSaved={() => { setNeedToken(false); setMessage(""); }} />}
      </div>

      {projects.length === 0 && <p className="text-sm text-gray-500">{t("fleet.projects.empty")}</p>}
      {projects.map((project) => (
        <ProjectPanel key={`${project.id}:${project.updated_at}`} project={project} />
      ))}
    </div>
  );
}
