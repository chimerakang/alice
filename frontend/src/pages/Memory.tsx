import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { BrainCircuit, Database, FileText, Layers3, RefreshCw, Search } from "lucide-react";
import { api } from "@/lib/api";
import type { MemoryPreviewResponse, MemoryPreviewSection } from "@/types/alice";

interface MemoryFormState {
  chatId: string;
  threadId: string;
  projectDir: string;
  issue: string;
  message: string;
  mode: string;
  budget: string;
}

const defaultForm: MemoryFormState = {
  chatId: "",
  threadId: "0",
  projectDir: "",
  issue: "",
  message: "",
  mode: "preview",
  budget: "6000",
};

function sourceTone(source: string): string {
  switch (source) {
    case "issue_task":
      return "border-cyan-500/30 bg-cyan-500/10 text-cyan-200";
    case "hermes_task":
      return "border-blue-500/30 bg-blue-500/10 text-blue-200";
    case "general_task":
      return "border-emerald-500/30 bg-emerald-500/10 text-emerald-200";
    case "recent_messages":
      return "border-amber-500/30 bg-amber-500/10 text-amber-200";
    default:
      return "border-gray-600 bg-gray-800 text-gray-300";
  }
}

function formatNumber(value: number): string {
  return new Intl.NumberFormat("en-US").format(value || 0);
}

function parsePositiveInt(value: string): number | undefined {
  const trimmed = value.trim();
  if (!trimmed) return undefined;
  const parsed = Number.parseInt(trimmed, 10);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : undefined;
}

function SectionRow({ section }: { section: MemoryPreviewSection }) {
  const { t } = useTranslation();
  return (
    <article className="card p-4 space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <span className={`px-2 py-1 rounded-md border text-xs font-mono ${sourceTone(section.source)}`}>
          {t(`memory.sources.${section.source}`, { defaultValue: section.source })}
        </span>
        <span className="px-2 py-1 rounded-md bg-gray-800 text-gray-300 text-xs font-mono">
          {section.scope}
        </span>
        <span className="text-xs text-gray-500 font-mono">
          {t("memory.section.priority", { priority: section.priority })}
        </span>
        <span className="text-xs text-gray-500 font-mono">
          {t("memory.section.chars", { count: formatNumber(section.size) })}
        </span>
      </div>
      <pre className="text-xs leading-relaxed whitespace-pre-wrap break-words text-gray-300 bg-black/30 border border-gray-800 rounded-md p-3 max-h-64 overflow-auto">
        {section.preview || t("memory.empty.no_preview")}
      </pre>
    </article>
  );
}

export default function Memory() {
  const { t } = useTranslation();
  const [form, setForm] = useState<MemoryFormState>(defaultForm);
  const [preview, setPreview] = useState<MemoryPreviewResponse | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const sectionStats = useMemo(() => {
    const sections = preview?.sections || [];
    const sourceLabels = sections.map((section) => t(`memory.sources.${section.source}`, { defaultValue: section.source }));
    return {
      count: sections.length,
      sources: sourceLabels.length > 0 ? Array.from(new Set(sourceLabels)).join(", ") : t("common.no_data"),
    };
  }, [preview, t]);

  const updateField = (field: keyof MemoryFormState, value: string) => {
    setForm((current) => ({ ...current, [field]: value }));
  };

  const loadPreview = async () => {
    const chatId = Number.parseInt(form.chatId.trim(), 10);
    if (!Number.isFinite(chatId) || chatId === 0) {
      setError(t("memory.errors.chat_id_required"));
      setPreview(null);
      return;
    }

    setLoading(true);
    setError(null);
    try {
      const result = await api.getMemoryPreview({
        chatId,
        threadId: parsePositiveInt(form.threadId) ?? 0,
        projectDir: form.projectDir.trim() || undefined,
        issue: parsePositiveInt(form.issue),
        message: form.message.trim() || undefined,
        mode: form.mode.trim() || "preview",
        budget: parsePositiveInt(form.budget),
      });
      setPreview(result);
    } catch (err) {
      setError(err instanceof Error ? err.message : t("memory.errors.load_failed"));
      setPreview(null);
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="space-y-6 animate-fade-in">
      <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
        <div>
          <h2 className="text-lg font-semibold text-white flex items-center gap-2">
            <BrainCircuit className="w-5 h-5 text-primary" />
            {t("memory.title")}
          </h2>
          <p className="text-sm text-gray-500 mt-1">{t("memory.subtitle")}</p>
        </div>
        <button
          onClick={loadPreview}
          disabled={loading}
          className="inline-flex items-center justify-center gap-2 px-3 py-2 text-sm bg-primary hover:bg-primary-light disabled:opacity-60 disabled:cursor-not-allowed text-white rounded-md transition-colors"
        >
          {loading ? <RefreshCw className="w-4 h-4 animate-spin" /> : <Search className="w-4 h-4" />}
          {t("memory.actions.preview")}
        </button>
      </div>

      <section className="card p-5">
        <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-4 gap-4">
          <label className="space-y-1">
            <span className="text-xs font-medium text-gray-400">{t("memory.form.chat_id")}</span>
            <input
              value={form.chatId}
              onChange={(event) => updateField("chatId", event.target.value)}
              className="w-full bg-gray-900 border border-gray-700 rounded-md px-3 py-2 text-sm text-white focus:outline-none focus:border-primary"
              placeholder={t("memory.placeholders.chat_id")}
            />
          </label>
          <label className="space-y-1">
            <span className="text-xs font-medium text-gray-400">{t("memory.form.thread_id")}</span>
            <input
              value={form.threadId}
              onChange={(event) => updateField("threadId", event.target.value)}
              className="w-full bg-gray-900 border border-gray-700 rounded-md px-3 py-2 text-sm text-white focus:outline-none focus:border-primary"
              placeholder={t("memory.placeholders.thread_id")}
            />
          </label>
          <label className="space-y-1">
            <span className="text-xs font-medium text-gray-400">{t("memory.form.issue")}</span>
            <input
              value={form.issue}
              onChange={(event) => updateField("issue", event.target.value)}
              className="w-full bg-gray-900 border border-gray-700 rounded-md px-3 py-2 text-sm text-white focus:outline-none focus:border-primary"
              placeholder={t("memory.placeholders.issue")}
            />
          </label>
          <label className="space-y-1">
            <span className="text-xs font-medium text-gray-400">{t("memory.form.budget")}</span>
            <input
              value={form.budget}
              onChange={(event) => updateField("budget", event.target.value)}
              className="w-full bg-gray-900 border border-gray-700 rounded-md px-3 py-2 text-sm text-white focus:outline-none focus:border-primary"
              placeholder={t("memory.placeholders.budget")}
            />
          </label>
          <label className="space-y-1 md:col-span-2">
            <span className="text-xs font-medium text-gray-400">{t("memory.form.project_dir")}</span>
            <input
              value={form.projectDir}
              onChange={(event) => updateField("projectDir", event.target.value)}
              className="w-full bg-gray-900 border border-gray-700 rounded-md px-3 py-2 text-sm text-white focus:outline-none focus:border-primary"
              placeholder={t("memory.placeholders.project_dir")}
            />
          </label>
          <label className="space-y-1">
            <span className="text-xs font-medium text-gray-400">{t("memory.form.mode")}</span>
            <select
              value={form.mode}
              onChange={(event) => updateField("mode", event.target.value)}
              className="w-full bg-gray-900 border border-gray-700 rounded-md px-3 py-2 text-sm text-white focus:outline-none focus:border-primary"
            >
              <option value="preview">{t("memory.mode.preview")}</option>
              <option value="hermes">{t("memory.mode.hermes")}</option>
              <option value="direct_resume_fallback">{t("memory.mode.direct_resume_fallback")}</option>
              <option value="document">{t("memory.mode.document")}</option>
              <option value="photo">{t("memory.mode.photo")}</option>
              <option value="voice">{t("memory.mode.voice")}</option>
            </select>
          </label>
          <label className="space-y-1 md:col-span-2 xl:col-span-1">
            <span className="text-xs font-medium text-gray-400">{t("memory.form.message")}</span>
            <input
              value={form.message}
              onChange={(event) => updateField("message", event.target.value)}
              className="w-full bg-gray-900 border border-gray-700 rounded-md px-3 py-2 text-sm text-white focus:outline-none focus:border-primary"
              placeholder={t("memory.continue_placeholder")}
            />
          </label>
        </div>
        {error && (
          <div className="mt-4 border border-error/30 bg-error/10 text-error rounded-md px-3 py-2 text-sm">
            {error}
          </div>
        )}
      </section>

      <section className="grid grid-cols-1 md:grid-cols-3 gap-4">
        <div className="card p-5">
          <div className="flex items-center gap-2 text-sm text-gray-400 mb-2">
            <Layers3 className="w-4 h-4 text-primary" />
            {t("memory.stats.sections")}
          </div>
          <div className="text-2xl font-bold font-mono text-white">{sectionStats.count}</div>
        </div>
        <div className="card p-5">
          <div className="flex items-center gap-2 text-sm text-gray-400 mb-2">
            <FileText className="w-4 h-4 text-success" />
            {t("memory.stats.rendered_size")}
          </div>
          <div className="text-2xl font-bold font-mono text-white">{formatNumber(preview?.rendered_size || 0)}</div>
        </div>
        <div className="card p-5">
          <div className="flex items-center gap-2 text-sm text-gray-400 mb-2">
            <Database className="w-4 h-4 text-warning" />
            {t("memory.stats.sources")}
          </div>
          <div className="text-sm font-mono text-white break-words">{sectionStats.sources}</div>
        </div>
      </section>

      <section className="grid grid-cols-1 xl:grid-cols-2 gap-6">
        <div className="space-y-4">
          <h3 className="text-sm font-semibold text-gray-300">{t("memory.sections.title")}</h3>
          {preview?.sections?.length ? (
            preview.sections.map((section, index) => (
              <SectionRow key={`${section.source}-${section.scope}-${index}`} section={section} />
            ))
          ) : (
            <div className="card p-6 text-sm text-gray-500">{t("memory.empty.no_sections")}</div>
          )}
        </div>
        <div className="space-y-4">
          <h3 className="text-sm font-semibold text-gray-300">{t("memory.sections.rendered_bundle")}</h3>
          <pre className="card p-4 text-xs leading-relaxed whitespace-pre-wrap break-words text-gray-300 min-h-64 max-h-[36rem] overflow-auto">
            {preview?.rendered_preview || t("memory.empty.no_rendered_preview")}
          </pre>
        </div>
      </section>
    </div>
  );
}
