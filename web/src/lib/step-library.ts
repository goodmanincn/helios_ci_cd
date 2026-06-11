export type StepCategory =
  | "code-build"
  | "test-quality"
  | "deploy"
  | "control-flow"
  | "notify";

export interface StepLibraryItem {
  id: string;
  name: string;
  icon: string;
  category: StepCategory;
  /** 默认 stage type */
  nodeType: "step" | "matrix" | "approval" | "notify";
  /** 默认 YAML 片段 (会被填入属性面板) */
  defaultConfig: Record<string, unknown>;
}

export interface StepCategoryMeta {
  key: StepCategory;
  label: string;
  items: StepLibraryItem[];
}

export const STEP_LIBRARY: StepCategoryMeta[] = [
  {
    key: "code-build",
    label: "代码与构建",
    items: [
      {
        id: "git-checkout",
        name: "Git Checkout",
        icon: "📥",
        category: "code-build",
        nodeType: "step",
        defaultConfig: { runs_on: { image: "alpine/git" } },
      },
      {
        id: "docker-build",
        name: "Docker Build",
        icon: "🐳",
        category: "code-build",
        nodeType: "step",
        defaultConfig: {},
      },
      {
        id: "kaniko",
        name: "Kaniko",
        icon: "📦",
        category: "code-build",
        nodeType: "step",
        defaultConfig: {
          with: { dockerfile: "Dockerfile", context: ".", destination: "" },
        },
      },
      {
        id: "buildpacks",
        name: "Buildpacks",
        icon: "🏗",
        category: "code-build",
        nodeType: "step",
        defaultConfig: {},
      },
    ],
  },
  {
    key: "test-quality",
    label: "测试与质量",
    items: [
      {
        id: "unit-test",
        name: "单元测试",
        icon: "🧪",
        category: "test-quality",
        nodeType: "step",
        defaultConfig: {},
      },
      {
        id: "sonarqube",
        name: "SonarQube",
        icon: "🔍",
        category: "test-quality",
        nodeType: "step",
        defaultConfig: {},
      },
      {
        id: "trivy",
        name: "Trivy 扫描",
        icon: "🛡",
        category: "test-quality",
        nodeType: "step",
        defaultConfig: { with: { image: "", severity: "CRITICAL,HIGH" } },
      },
    ],
  },
  {
    key: "deploy",
    label: "部署",
    items: [
      {
        id: "k8s-deploy",
        name: "K8s Deploy",
        icon: "☸",
        category: "deploy",
        nodeType: "step",
        defaultConfig: {
          with: { cluster: "", namespace: "default", manifest: "" },
        },
      },
      {
        id: "helm-release",
        name: "Helm Release",
        icon: "🐝",
        category: "deploy",
        nodeType: "step",
        defaultConfig: {},
      },
      {
        id: "ssh-deploy",
        name: "SSH Deploy",
        icon: "🚀",
        category: "deploy",
        nodeType: "step",
        defaultConfig: {},
      },
      {
        id: "rsync",
        name: "rsync",
        icon: "📤",
        category: "deploy",
        nodeType: "step",
        defaultConfig: {},
      },
    ],
  },
  {
    key: "control-flow",
    label: "控制流",
    items: [
      {
        id: "approval",
        name: "人工审批",
        icon: "✋",
        category: "control-flow",
        nodeType: "approval",
        defaultConfig: { approvers: ["*"], mode: "any", timeout: "24h" },
      },
      {
        id: "parallel",
        name: "并行分组",
        icon: "⚡",
        category: "control-flow",
        nodeType: "step",
        defaultConfig: {},
      },
      {
        id: "condition",
        name: "条件分支",
        icon: "🔀",
        category: "control-flow",
        nodeType: "step",
        defaultConfig: { if: 'branch == "main"' },
      },
      {
        id: "matrix",
        name: "Matrix",
        icon: "🔁",
        category: "control-flow",
        nodeType: "matrix",
        defaultConfig: { matrix: { "go-version": ["1.21", "1.22", "1.23"] } },
      },
    ],
  },
  {
    key: "notify",
    label: "通知",
    items: [
      {
        id: "dingtalk",
        name: "钉钉",
        icon: "💬",
        category: "notify",
        nodeType: "notify",
        defaultConfig: {},
      },
      {
        id: "email",
        name: "邮件",
        icon: "📧",
        category: "notify",
        nodeType: "notify",
        defaultConfig: {},
      },
      {
        id: "webhook",
        name: "Webhook",
        icon: "🌐",
        category: "notify",
        nodeType: "notify",
        defaultConfig: {},
      },
    ],
  },
];

export function flattenSteps(): StepLibraryItem[] {
  return STEP_LIBRARY.flatMap((c) => c.items);
}

export function searchSteps(q: string): StepLibraryItem[] {
  const query = q.trim().toLowerCase();
  if (!query) return flattenSteps();
  return flattenSteps().filter(
    (s) =>
      s.name.toLowerCase().includes(query) ||
      s.id.toLowerCase().includes(query),
  );
}
