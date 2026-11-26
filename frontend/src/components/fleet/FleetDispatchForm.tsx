import { useEffect, useState } from "react";
import { clsx } from "clsx";
import { useTranslation } from "react-i18next";
import { X } from "lucide-react";
import { FleetApiError, fleetApi, getApiToken, setApiToken } from "@/lib/fleetApi";
import { parseBatchGoals } from "@/lib/fleet";
import { DEFAULT_CREATE_ISSUE, MAX_BATCH, buildDispatchRequests, type DispatchMode, type GitHubDispatchOptions } from "@/lib/fleetForms";
import { useFleetStore } from "@/stores/fleetStore";
import type { FleetIssue, FleetMilestone, FleetProject, FleetShape } from "@/types/fleet";

type Mode = DispatchMode;

interface Props {
  projects: FleetProject[];
  defaultProject?: string;
  onClose: () => void;
}

/** The open issues of one project, or why they could not be read. */
interface IssueList {
  project: string;
  issues: FleetIssue[];
  error: string;
}

/**
 * Dispatches tasks: one described in words, several at once (one per line),
 * or picked from the project's open GitHub issues.
 */
export default function FleetDispatchForm({ projects, defaultProject = "", onClose }: Props) {
  const { t } = useTranslation();
  const putTask = useFleetStore((s) => s.putTask);
  const [mode, setMode] = useState<Mode>("single");
  const [project, setProject] = useState(defaultProject || projects[0]?.id || "");
  const [shape, setShape] = useState<FleetShape>("ship");
  const [model, setModel] = useState("");
  const [budget, setBudget] = useState("");
  const [goal, setGoal] = useState("");
  const [batch, setBatch] = useState("");
  const [picked, setPicked] = useState<number[]>([]);
  const [loadedIssues, setLoadedIssues] = useState<IssueList | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [results, setResults] = useState<{ ok: number; failed: string[]; issueNumber?: number; warning?: string } | null>(null);
  const [needToken, setNeedToken] = useState(false);
  const [token, setToken] = useState(getApiToken());

  // GitHub section state (single mode only)
  const currentProject = projects.find((p) => p.id === project);
  const githubAvailable = currentProject?.github_available ?? false;
  const [createIssue, setCreateIssue] = useState(DEFAULT_CREATE_ISSUE);
  const [ghMilestone, setGhMilestone] = useState("");
  const [ghEpicIssue, setGhEpicIssue] = useState<number | null>(null);
  const [loadedMilestones, setLoadedMilestones] = useState<FleetMilestone[] | null>(null);
  const [loadedEpics, setLoadedEpics] = useState<FleetIssue[] | null>(null);

  // The issue list belongs to a project: what was loaded for another project
  // counts as not loaded.
  const current = loadedIssues?.project === project ? loadedIssues : null;
  const issues = current && !current.error ? current.issues : null;
  const issuesError = current?.error ?? "";

  useEffect(() => {
    if (mode !== "issues" || !project) return;
    let stale = false;
    fleetApi
      .listProjectIssues(project)
      .then((list) => !stale && setLoadedIssues({ project, issues: list, error: "" }))
      .catch(
        (err: unknown) =>
          !stale && setLoadedIssues({ project, issues: [], error: err instanceof Error ? err.message : String(err) }),
      );
    return () => {
      stale = true;
    };
  }, [mode, project]);

  // Load milestones and epics for the GitHub section (single mode + github available).
  useEffect(() => {
    if (mode !== "single" || !project || !githubAvailable) return;
    let stale = false;
    fleetApi.listProjectMilestones(project)
      .then((list) => { if (!stale) setLoadedMilestones(list); })
      .catch(() => { if (!stale) setLoadedMilestones([]); });
    fleetApi.listProjectIssues(project)
      .then((list) => { if (!stale) setLoadedEpics(list.filter((i) => i.is_epic)); })
      .catch(() => { if (!stale) setLoadedEpics([]); });
    return () => { stale = true; };
  }, [mode, project, githubAvailable]);

  async function submit() {
    const github: GitHubDispatchOptions | undefined =
      mode === "single" && githubAvailable
        ? { createIssue, milestone: ghMilestone, epicIssue: ghEpicIssue }
        : undefined;
    const built = buildDispatchRequests({ mode, project, shape, model, budget, goal, batch, issues: picked, github });
    if (built.error) {
      setError(t(`fleet.dispatch.error_${built.error}`, { max: MAX_BATCH }));
      return;
    }
    setBusy(true);
    setError("");
    setResults(null);
    let ok = 0;
    const failed: string[] = [];
    let issueNumber: number | undefined;
    let warning: string | undefined;
    // One at a time: a refusal for one task must not hide the others, and
    // the order of the list is the order they are queued in.
    for (const req of built.requests) {
      try {
        const task = await fleetApi.dispatch(req);
        putTask(task);
        ok++;
        if (task.issue_url && task.issue_number) issueNumber = task.issue_number;
        if (task.warning) warning = task.warning;
      } catch (err) {
        if (err instanceof FleetApiError && err.unauthorized) {
          setNeedToken(true);
          setBusy(false);
          setError(err.message);
          return;
        }
        const label = req.issue ? `#${req.issue}` : (req.goal ?? "").slice(0, 60);
        failed.push(`${label}: ${err instanceof Error ? err.message : String(err)}`);
      }
    }
    setBusy(false);
    setResults({ ok, failed, issueNumber, warning });
    if (failed.length === 0) {
      setGoal("");
      setBatch("");
      setPicked([]);
      if (mode !== "issues") onClose();
    }
  }

  const input =
    "w-full rounded-md border border-gray-700 bg-black/40 px-2 py-1.5 text-sm text-gray-100 placeholder:text-gray-600 focus:border-primary/50 focus:outline-none";
  const count = mode === "batch" ? parseBatchGoals(batch).length : mode === "issues" ? picked.length : goal.trim() ? 1 : 0;

  return (
    <div className="rounded-lg border border-primary/30 bg-black/40 p-4 space-y-3" data-testid="fleet-dispatch">
      <div className="flex items-center justify-between">
        <h2 className="text-sm font-semibold text-white">{t("fleet.dispatch.title")}</h2>
        <button type="button" onClick={onClose} aria-label={t("common.cancel")} className="text-gray-500 hover:text-gray-200">
          <X className="w-4 h-4" />
        </button>
      </div>

      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <label className="space-y-1 text-xs text-gray-400">
          {t("fleet.dispatch.project")}
          <select
            value={project}
            onChange={(e) => {
              setProject(e.target.value);
              setPicked([]); // issue numbers mean nothing in another project
            }}
            className={input}
          >
            {projects.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name || p.id}
              </option>
            ))}
          </select>
        </label>
        <label className="space-y-1 text-xs text-gray-400">
          {t("fleet.dispatch.shape")}
          <select value={shape} onChange={(e) => setShape(e.target.value as FleetShape)} className={input}>
            <option value="ship">{t("fleet.shape.ship")}</option>
            <option value="scout">{t("fleet.shape.scout")}</option>
          </select>
        </label>
        <label className="space-y-1 text-xs text-gray-400">
          {t("fleet.dispatch.model")}
          <input value={model} onChange={(e) => setModel(e.target.value)} placeholder={t("fleet.dispatch.model_default")} className={input} />
        </label>
        <label className="space-y-1 text-xs text-gray-400">
          {t("fleet.dispatch.budget")}
          <input value={budget} onChange={(e) => setBudget(e.target.value)} inputMode="decimal" placeholder={t("fleet.dispatch.budget_default")} className={input} />
        </label>
      </div>

      <div className="flex gap-1 border-b border-gray-800">
        {(["single", "batch", "issues"] as Mode[]).map((m) => (
          <button
            key={m}
            type="button"
            onClick={() => setMode(m)}
            className={clsx(
              "px-3 py-1.5 text-sm border-b-2 -mb-px",
              mode === m ? "border-primary text-primary" : "border-transparent text-gray-400 hover:text-gray-200",
            )}
          >
            {t(`fleet.dispatch.mode_${m}`)}
          </button>
        ))}
      </div>

      {mode === "single" && (
        <div className="space-y-3">
          <textarea value={goal} onChange={(e) => setGoal(e.target.value)} rows={4} placeholder={t(`fleet.dispatch.goal_placeholder_${shape}`)} className={input} />
          {/* GitHub section */}
          <div className="rounded-md border border-gray-800 p-3 space-y-2">
            <p className="text-xs font-medium text-gray-400">{t("fleet.dispatch.github_section")}</p>
            {!githubAvailable ? (
              <p className="text-xs text-gray-500">{t("fleet.dispatch.github_no_remote")}</p>
            ) : (
              <>
                <label className="flex items-center gap-2 text-sm text-gray-200 cursor-pointer">
                  <input
                    type="checkbox"
                    checked={createIssue}
                    onChange={(e) => setCreateIssue(e.target.checked)}
                    className="accent-primary"
                  />
                  {t("fleet.dispatch.github_create_issue")}
                </label>
                {createIssue && (
                  <div className="grid grid-cols-1 gap-2 sm:grid-cols-2 pl-5">
                    <label className="space-y-1 text-xs text-gray-400">
                      {t("fleet.dispatch.github_milestone_label")}
                      {loadedMilestones && loadedMilestones.length > 0 ? (
                        <select
                          value={ghMilestone}
                          onChange={(e) => setGhMilestone(e.target.value)}
                          className={input}
                        >
                          <option value="">{t("fleet.dispatch.github_milestone_placeholder")}</option>
                          {loadedMilestones.map((m) => (
                            <option key={m.number} value={m.title}>{m.title}</option>
                          ))}
                        </select>
                      ) : (
                        <input
                          value={ghMilestone}
                          onChange={(e) => setGhMilestone(e.target.value)}
                          placeholder={t("fleet.dispatch.github_milestone_placeholder")}
                          className={input}
                        />
                      )}
                    </label>
                    <label className="space-y-1 text-xs text-gray-400">
                      {t("fleet.dispatch.github_epic_label")}
                      <select
                        value={ghEpicIssue ?? ""}
                        onChange={(e) => setGhEpicIssue(e.target.value ? Number(e.target.value) : null)}
                        className={input}
                      >
                        <option value="">{t("fleet.dispatch.github_epic_placeholder")}</option>
                        {(loadedEpics ?? []).map((epic) => (
                          <option key={epic.number} value={epic.number}>#{epic.number} {epic.title}</option>
                        ))}
                      </select>
                    </label>
                  </div>
                )}
              </>
            )}
          </div>
        </div>
      )}
      {mode === "batch" && (
        <div className="space-y-1">
          <textarea value={batch} onChange={(e) => setBatch(e.target.value)} rows={6} placeholder={t("fleet.dispatch.batch_placeholder")} className={input} />
          <p className="text-xs text-gray-500">{t("fleet.dispatch.batch_hint", { max: MAX_BATCH })}</p>
        </div>
      )}
      {mode === "issues" && (
        <div className="max-h-72 overflow-y-auto rounded-md border border-gray-800">
          {issues === null && !issuesError && <p className="p-3 text-sm text-gray-500">{t("common.loading")}</p>}
          {issuesError && <p className="p-3 text-sm text-error break-words">{issuesError}</p>}
          {issues !== null && issues.length === 0 && <p className="p-3 text-sm text-gray-500">{t("fleet.dispatch.no_issues")}</p>}
          {(issues ?? []).map((issue) => {
            const taken = issue.task_id !== "";
            return (
              <label
                key={issue.number}
                className={clsx("flex items-start gap-2 border-b border-gray-800/60 px-3 py-2 text-sm last:border-b-0", taken ? "opacity-50" : "cursor-pointer hover:bg-white/5")}
              >
                <input
                  type="checkbox"
                  className="mt-1"
                  disabled={taken}
                  checked={picked.includes(issue.number)}
                  onChange={(e) =>
                    setPicked((cur) => (e.target.checked ? [...cur, issue.number] : cur.filter((n) => n !== issue.number)))
                  }
                />
                <span className="min-w-0 flex-1">
                  <span className="text-primary mr-1.5">#{issue.number}</span>
                  <span className="text-gray-100 break-words">{issue.title}</span>
                  <span className="block text-xs text-gray-500">
                    {[issue.milestone, ...issue.labels].filter(Boolean).join(" · ")}
                    {taken && ` · ${t("fleet.dispatch.issue_taken", { id: issue.task_id })}`}
                  </span>
                </span>
              </label>
            );
          })}
        </div>
      )}

      {needToken && (
        <form
          className="flex flex-wrap items-center gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            setApiToken(token.trim());
            setNeedToken(false);
            setError("");
          }}
        >
          <input type="password" value={token} onChange={(e) => setToken(e.target.value)} placeholder={t("fleet.token.placeholder")} className={clsx(input, "flex-1 min-w-[12rem]")} />
          <button type="submit" className="rounded-md border border-primary/30 bg-primary/15 px-3 py-1.5 text-sm text-primary">
            {t("fleet.token.save")}
          </button>
          <p className="w-full text-xs text-gray-500">{t("fleet.token.hint")}</p>
        </form>
      )}
      {error && !needToken && <p className="text-xs text-error break-words">{error}</p>}
      {results && (
        <div className="text-xs space-y-1">
          {results.ok > 0 && <p className="text-success">{t("fleet.dispatch.done", { count: results.ok })}</p>}
          {results.issueNumber != null && (
            <p className="text-success">{t("fleet.dispatch.github_issue_created", { number: results.issueNumber })}</p>
          )}
          {results.warning && (
            <p className="text-warning break-words">{t("fleet.dispatch.github_epic_warning", { message: results.warning })}</p>
          )}
          {results.failed.map((line) => (
            <p key={line} className="text-error break-words">
              {line}
            </p>
          ))}
        </div>
      )}

      <div className="flex justify-end">
        <button
          type="button"
          disabled={busy || count === 0}
          onClick={() => void submit()}
          className="rounded-md border border-primary/30 bg-primary/15 px-4 py-1.5 text-sm font-medium text-primary hover:bg-primary/25 disabled:opacity-50"
        >
          {busy ? t("fleet.actions.working") : t("fleet.dispatch.submit", { count })}
        </button>
      </div>
    </div>
  );
}
