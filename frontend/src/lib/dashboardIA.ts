export interface DashboardIABlock {
  key: string;
  titleKey: string;
  descriptionKey: string;
  items: string[];
  route: string;
}

export const dashboardIABlocks: DashboardIABlock[] = [
  {
    key: "overview",
    titleKey: "dashboard.ia.blocks.overview.title",
    descriptionKey: "dashboard.ia.blocks.overview.description",
    items: [
      "dashboard.ia.blocks.overview.items.hermes_runs",
      "dashboard.ia.blocks.overview.items.health",
      "dashboard.ia.blocks.overview.items.blockers",
      "dashboard.ia.blocks.overview.items.trends",
    ],
    route: "/",
  },
  {
    key: "issues_runs",
    titleKey: "dashboard.ia.blocks.issues_runs.title",
    descriptionKey: "dashboard.ia.blocks.issues_runs.description",
    items: [
      "dashboard.ia.blocks.issues_runs.items.issue_progress",
      "dashboard.ia.blocks.issues_runs.items.latest_run",
      "dashboard.ia.blocks.issues_runs.items.review_verdict",
      "dashboard.ia.blocks.issues_runs.items.evidence",
    ],
    route: "/issues-runs",
  },
  {
    key: "run_inspector",
    titleKey: "dashboard.ia.blocks.run_inspector.title",
    descriptionKey: "dashboard.ia.blocks.run_inspector.description",
    items: [
      "dashboard.ia.blocks.run_inspector.items.summary_kpi",
      "dashboard.ia.blocks.run_inspector.items.timeline",
      "dashboard.ia.blocks.run_inspector.items.subtasks",
      "dashboard.ia.blocks.run_inspector.items.diagnostics",
    ],
    route: "/run-inspector",
  },
  {
    key: "secondary",
    titleKey: "dashboard.ia.blocks.secondary.title",
    descriptionKey: "dashboard.ia.blocks.secondary.description",
    items: [
      "dashboard.ia.blocks.secondary.items.runtime",
      "dashboard.ia.blocks.secondary.items.quality",
      "dashboard.ia.blocks.secondary.items.checkpoints",
      "dashboard.ia.blocks.secondary.items.cost",
    ],
    route: "/quality",
  },
];
