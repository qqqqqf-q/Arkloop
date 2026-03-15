import { useState, useEffect, useCallback } from "react";
import {
  Shield,
  Database,
  Search,
  Globe,
  Bot,
  CheckCircle,
  XCircle,
  type LucideIcon,
} from "lucide-react";
import { SpinnerIcon } from "@arkloop/shared/components/auth-ui";
import { useLocale } from "../../contexts/LocaleContext";
import {
  bridgeClient,
  checkBridgeAvailable,
  type ModuleInfo,
  type ModuleAction,
  type ModuleStatus,
} from "../../api-bridge";

type ModuleDisplaySpec = {
  id: string;
  icon: LucideIcon;
  titleKey: string;
  descKey: string;
};

const MODULE_SPECS: ModuleDisplaySpec[] = [
  {
    id: "sandbox-docker",
    icon: Shield,
    titleKey: "sandboxTitle",
    descKey: "sandboxDesc",
  },
  {
    id: "openviking",
    icon: Database,
    titleKey: "memoryTitle",
    descKey: "memoryDesc",
  },
  {
    id: "searxng",
    icon: Search,
    titleKey: "searchTitle",
    descKey: "searchDesc",
  },
  {
    id: "firecrawl",
    icon: Globe,
    titleKey: "crawlerTitle",
    descKey: "crawlerDesc",
  },
  {
    id: "browser",
    icon: Bot,
    titleKey: "browserTitle",
    descKey: "browserDesc",
  },
];

function statusColor(status: ModuleStatus): string {
  switch (status) {
    case "running":
      return "#22c55e";
    case "stopped":
    case "installed_disconnected":
      return "#f59e0b";
    case "error":
      return "#ef4444";
    default:
      return "var(--c-text-muted)";
  }
}

function statusLabel(status: ModuleStatus): string {
  switch (status) {
    case "running":
      return "Running";
    case "stopped":
      return "Stopped";
    case "installed_disconnected":
      return "Disconnected";
    case "pending_bootstrap":
      return "Pending";
    case "error":
      return "Error";
    case "not_installed":
      return "Not installed";
    default:
      return status;
  }
}

function actionForStatus(status: ModuleStatus): ModuleAction | null {
  switch (status) {
    case "not_installed":
      return "install";
    case "stopped":
      return "start";
    case "running":
      return "stop";
    default:
      return null;
  }
}

function actionLabel(action: ModuleAction): string {
  switch (action) {
    case "install":
      return "Install";
    case "start":
      return "Start";
    case "stop":
      return "Stop";
    case "restart":
      return "Restart";
    default:
      return action;
  }
}

export function ModulesSettings() {
  const { t } = useLocale();
  const ds = t.desktopSettings;

  const [bridgeOnline, setBridgeOnline] = useState<boolean | null>(null);
  const [modules, setModules] = useState<ModuleInfo[]>([]);
  const [loading, setLoading] = useState(true);
  const [actionInProgress, setActionInProgress] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const loadModules = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const online = await checkBridgeAvailable();
      setBridgeOnline(online);
      if (online) {
        const list = await bridgeClient.listModules();
        setModules(list);
      } else {
        setModules([]);
      }
    } catch (err) {
      setBridgeOnline(false);
      setError(err instanceof Error ? err.message : "Failed to load modules");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void loadModules();
  }, [loadModules]);

  const handleAction = useCallback(
    async (moduleId: string, action: ModuleAction) => {
      setActionInProgress(moduleId);
      setError(null);
      try {
        const { operation_id } = await bridgeClient.performAction(
          moduleId,
          action,
        );
        await new Promise<void>((resolve, reject) => {
          let finished = false;
          const stop = bridgeClient.streamOperation(
            operation_id,
            () => {},
            (result) => {
              if (finished) return;
              finished = true;
              stop();
              if (result.status === "completed") resolve();
              else reject(new Error(result.error ?? `${action} failed`));
            },
          );
        });
        await loadModules();
      } catch (err) {
        setError(err instanceof Error ? err.message : `${action} failed`);
      } finally {
        setActionInProgress(null);
      }
    },
    [loadModules],
  );

  const getModuleInfo = (id: string): ModuleInfo | undefined =>
    modules.find((m) => m.id === id);

  return (
    <div className="flex flex-col gap-6">
      <div>
        <h3 className="text-base font-semibold text-[var(--c-text-heading)]">
          {ds.modulesTitle}
        </h3>
        <p className="mt-1 text-sm text-[var(--c-text-secondary)]">
          {ds.modulesDesc}
        </p>
      </div>

      {/* Bridge status */}
      <div
        className="flex items-center justify-between rounded-xl bg-[var(--c-bg-menu)] px-4 py-3"
        style={{ border: "0.5px solid var(--c-border-subtle)" }}
      >
        <div className="flex items-center gap-2">
          <div
            className="h-2 w-2 rounded-full"
            style={{
              background:
                bridgeOnline === null
                  ? "var(--c-text-muted)"
                  : bridgeOnline
                    ? "#22c55e"
                    : "#ef4444",
            }}
          />
          <span className="text-sm text-[var(--c-text-primary)]">
            Installer Bridge
          </span>
        </div>
        <span className="text-xs text-[var(--c-text-muted)]">
          {bridgeOnline === null ? "..." : bridgeOnline ? "Online" : "Offline"}
        </span>
      </div>

      {error && (
        <div
          className="flex items-center gap-2 rounded-lg px-3 py-2 text-sm"
          style={{ background: "rgba(239, 68, 68, 0.08)", color: "#ef4444" }}
        >
          <XCircle size={14} />
          {error}
        </div>
      )}

      {loading ? (
        <div className="flex items-center justify-center py-8">
          <SpinnerIcon />
        </div>
      ) : bridgeOnline === false ? (
        <div
          className="rounded-xl bg-[var(--c-bg-menu)] px-4 py-6 text-center text-sm text-[var(--c-text-muted)]"
          style={{ border: "0.5px solid var(--c-border-subtle)" }}
        >
          {ds.modulesOffline}
        </div>
      ) : (
        <div className="flex flex-col gap-3">
          {MODULE_SPECS.map(({ id, icon: Icon, titleKey, descKey }) => {
            const info = getModuleInfo(id);
            const status = info?.status ?? "not_installed";
            const action = actionForStatus(status);
            const isActing = actionInProgress === id;

            return (
              <div
                key={id}
                className="flex items-center gap-4 rounded-xl bg-[var(--c-bg-menu)] px-4 py-3"
                style={{ border: "0.5px solid var(--c-border-subtle)" }}
              >
                <div
                  className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl"
                  style={{
                    background:
                      status === "running"
                        ? "var(--c-btn-bg)"
                        : "var(--c-bg-sub)",
                    color:
                      status === "running"
                        ? "var(--c-btn-text)"
                        : "var(--c-text-secondary)",
                  }}
                >
                  <Icon size={18} />
                </div>

                <div className="min-w-0 flex-1">
                  <div className="text-sm font-medium text-[var(--c-text-heading)]">
                    {ds[titleKey as keyof typeof ds] as string}
                  </div>
                  <div className="mt-0.5 text-xs text-[var(--c-text-muted)]">
                    {ds[descKey as keyof typeof ds] as string}
                  </div>
                </div>

                <div className="flex items-center gap-3">
                  <span
                    className="text-xs"
                    style={{ color: statusColor(status) }}
                  >
                    {statusLabel(status)}
                  </span>
                  {action && (
                    <button
                      onClick={() => void handleAction(id, action)}
                      disabled={isActing || actionInProgress !== null}
                      className="rounded-md px-3 py-1.5 text-xs font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-50"
                      style={{
                        border: "0.5px solid var(--c-border-subtle)",
                        background: "var(--c-bg-deep)",
                        color: "var(--c-text-secondary)",
                      }}
                    >
                      {isActing ? <SpinnerIcon /> : actionLabel(action)}
                    </button>
                  )}
                </div>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
