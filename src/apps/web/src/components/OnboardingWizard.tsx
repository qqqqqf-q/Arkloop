import { useState, useEffect, useCallback, useRef, useMemo } from "react";
import {
  CheckCircle,
  ChevronRight,
  Cloud,
  HardDrive,
  Search,
  Server,
  Settings,
  XCircle,
} from "lucide-react";
import { ErrorCallout, isApiError, type AppError } from "@arkloop/shared";
import {
  Reveal,
  inputCls,
  inputStyle,
  labelStyle,
  SpinnerIcon,
  normalizeError,
} from "@arkloop/shared/components/auth-ui";
import { getDesktopApi, getDesktopAccessToken } from "@arkloop/shared/desktop";
import { useLocale } from "../contexts/LocaleContext";
import {
  createLlmProvider,
  createProviderModel,
  listAvailableModels,
  listLlmProviders,
  updateLlmProvider,
} from "../api";
import type { AvailableModel, LlmProvider, LlmProviderModel } from "../api";
import { routeAdvancedJsonFromAvailableCatalog } from "@arkloop/shared/llm/available-catalog-advanced-json";

type Step = "welcome" | "mode" | "provider" | "complete";

type Vendor = "openai_responses" | "openai_chat_completions" | "anthropic";
type VerifyStatus = "idle" | "verifying" | "verified" | "failed";
type ModelImportStatus =
  | "idle"
  | "loading"
  | "ready"
  | "empty"
  | "importing"
  | "done"
  | "failed";

type Props = { onComplete: () => void };

const LOCAL_ACCESS_TOKEN =
  getDesktopAccessToken() ?? "arkloop-desktop-local-token";

const VENDOR_OPTIONS = [
  {
    key: "openai_responses" as const,
    label: "OpenAI (Responses)",
    provider: "openai",
    openai_api_mode: "responses" as string | undefined,
  },
  {
    key: "openai_chat_completions" as const,
    label: "OpenAI (Chat Completions)",
    provider: "openai",
    openai_api_mode: "chat_completions" as string | undefined,
  },
  {
    key: "anthropic" as const,
    label: "Anthropic",
    provider: "anthropic",
    openai_api_mode: undefined as string | undefined,
  },
] as const;

const VENDOR_URLS: Record<Vendor, string> = {
  openai_responses: "https://api.openai.com/v1",
  openai_chat_completions: "https://api.openai.com/v1",
  anthropic: "https://api.anthropic.com/v1",
};

const RECOMMENDED_PATTERNS: Record<Vendor, string[]> = {
  openai_responses: ["gpt-4o", "gpt-4o-mini"],
  openai_chat_completions: ["gpt-4o", "gpt-4o-mini"],
  anthropic: ["claude-3-5-sonnet", "claude-3-5-haiku", "claude-3-haiku"],
};

function getDefaultSelectedIds(
  models: AvailableModel[],
  vendor: Vendor,
): Set<string> {
  const patterns = RECOMMENDED_PATTERNS[vendor];
  const selected = new Set<string>();
  const importable = models.filter((m) => !m.configured);

  for (const pattern of patterns) {
    const match = importable.find((m) => m.id.toLowerCase().includes(pattern));
    if (match) selected.add(match.id);
  }

  if (selected.size === 0) {
    importable.slice(0, 2).forEach((m) => selected.add(m.id));
  }

  return selected;
}

const btnBase: React.CSSProperties = {
  height: "38px",
  borderRadius: "10px",
  border: "none",
  cursor: "pointer",
  fontSize: "14px",
  fontWeight: 500,
  fontFamily: "inherit",
  display: "flex",
  alignItems: "center",
  justifyContent: "center",
  gap: "6px",
  width: "100%",
};

const primaryBtn: React.CSSProperties = {
  ...btnBase,
  background: "var(--c-btn-bg)",
  color: "var(--c-btn-text)",
};

const ghostBtn: React.CSSProperties = {
  ...btnBase,
  marginTop: "4px",
  background: "transparent",
  color: "var(--c-placeholder)",
};

const sectionCardStyle: React.CSSProperties = {
  border: "0.5px solid var(--c-border-subtle)",
  borderRadius: "14px",
  background: "var(--c-bg-menu)",
  padding: "14px",
};

function normalizeMode(mode?: string | null): string | null {
  const value = mode?.trim();
  return value ? value : null;
}

function providerMatches(
  provider: LlmProvider,
  vendorOpt: (typeof VENDOR_OPTIONS)[number],
): boolean {
  return (
    provider.provider === vendorOpt.provider &&
    normalizeMode(provider.openai_api_mode) ===
      normalizeMode(vendorOpt.openai_api_mode)
  );
}

function mergeConfiguredModels(
  current: LlmProviderModel[],
  next: LlmProviderModel[],
): LlmProviderModel[] {
  const merged = new Map<string, LlmProviderModel>();
  for (const model of current) merged.set(model.model, model);
  for (const model of next) merged.set(model.model, model);
  return Array.from(merged.values());
}

function StepIndicator({
  current,
  total,
  stepOf,
}: {
  current: number;
  total: number;
  stepOf: (c: number, t: number) => string;
}) {
  return (
    <div
      style={{
        fontSize: "12px",
        color: "var(--c-text-muted)",
        marginBottom: "24px",
      }}
    >
      {stepOf(current, total)}
    </div>
  );
}

function ProgressBar({ percent }: { percent: number }) {
  return (
    <div
      style={{
        height: "4px",
        borderRadius: "2px",
        background: "var(--c-border-subtle)",
        overflow: "hidden",
      }}
    >
      <div
        style={{
          height: "100%",
          borderRadius: "2px",
          background: "var(--c-btn-bg)",
          width: `${percent}%`,
          transition: "width 0.3s ease",
        }}
      />
    </div>
  );
}

function ToggleSwitch({
  checked,
  onChange,
  disabled,
}: {
  checked: boolean;
  onChange?: () => void;
  disabled?: boolean;
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      onClick={onChange}
      disabled={disabled}
      style={{
        width: "36px",
        height: "20px",
        borderRadius: "10px",
        background: checked ? "var(--c-btn-bg)" : "var(--c-border-subtle)",
        border: "none",
        cursor: disabled ? "default" : "pointer",
        position: "relative",
        transition: "background 0.2s",
        flexShrink: 0,
        opacity: disabled ? 0.5 : 1,
        padding: 0,
      }}
    >
      <span
        style={{
          position: "absolute",
          top: "2px",
          left: checked ? "18px" : "2px",
          width: "16px",
          height: "16px",
          borderRadius: "50%",
          background: "white",
          transition: "left 0.2s ease",
          boxShadow: "0 1px 2px rgba(0,0,0,0.25)",
          display: "block",
        }}
      />
    </button>
  );
}

function ModeCard({
  icon,
  title,
  desc,
  onClick,
  disabled,
  comingSoon,
}: {
  icon: React.ReactNode;
  title: string;
  desc: string;
  onClick?: () => void;
  disabled?: boolean;
  comingSoon?: string;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      className="onb-mode-card"
      style={{
        border: "0.5px solid var(--c-border-subtle)",
        borderRadius: "12px",
        background: "var(--c-bg-menu)",
        padding: "14px 16px",
        cursor: disabled ? "default" : "pointer",
        textAlign: "left",
        width: "100%",
        fontFamily: "inherit",
        opacity: disabled ? 0.55 : 1,
        display: "flex",
        alignItems: "center",
        gap: "14px",
        transition: "border-color 0.15s, background 0.15s",
      }}
    >
      <div
        style={{
          width: "36px",
          height: "36px",
          borderRadius: "10px",
          background: "var(--c-bg-deep)",
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          flexShrink: 0,
          color: "var(--c-text-secondary)",
        }}
      >
        {icon}
      </div>
      <div style={{ flex: 1, minWidth: 0 }}>
        <div
          style={{
            fontSize: "14px",
            fontWeight: 500,
            color: "var(--c-text-primary)",
            display: "flex",
            alignItems: "center",
            gap: "8px",
          }}
        >
          {title}
          {comingSoon && (
            <span
              style={{
                fontSize: "10px",
                color: "var(--c-text-muted)",
                background: "var(--c-bg-deep)",
                padding: "2px 6px",
                borderRadius: "4px",
                fontWeight: 400,
              }}
            >
              {comingSoon}
            </span>
          )}
        </div>
        <div
          style={{
            fontSize: "12px",
            color: "var(--c-placeholder)",
            marginTop: "2px",
          }}
        >
          {desc}
        </div>
      </div>
      {!disabled && (
        <ChevronRight
          size={16}
          style={{ color: "var(--c-text-muted)", flexShrink: 0 }}
        />
      )}
    </button>
  );
}

export function OnboardingWizard({ onComplete }: Props) {
  const { t, locale } = useLocale();
  const ob = t.onboarding;
  const api = getDesktopApi();

  const [step, setStep] = useState<Step>("welcome");

  // Sidecar readiness
  const [sidecarReady, setSidecarReady] = useState<boolean | null>(null);
  const [downloadPhase, setDownloadPhase] = useState("");
  const [downloadPercent, setDownloadPercent] = useState(0);
  const [downloadError, setDownloadError] = useState("");

  // Provider state
  const [vendor, setVendor] = useState<Vendor>("openai_responses");
  const [apiKey, setApiKey] = useState("");
  const [baseUrl, setBaseUrl] = useState(VENDOR_URLS.openai_responses);
  const [verifyStatus, setVerifyStatus] = useState<VerifyStatus>("idle");
  const [providerError, setProviderError] = useState<AppError | null>(null);

  // Model state
  const [modelImportStatus, setModelImportStatus] =
    useState<ModelImportStatus>("idle");
  const [availableModels, setAvailableModels] = useState<AvailableModel[]>([]);
  const [selectedModelIds, setSelectedModelIds] = useState<Set<string>>(
    new Set(),
  );
  const [createdProviderId, setCreatedProviderId] = useState<string | null>(
    null,
  );
  const [configuredModels, setConfiguredModels] = useState<LlmProviderModel[]>(
    [],
  );
  const [modelError, setModelError] = useState<AppError | null>(null);
  const [modelSearchQuery, setModelSearchQuery] = useState("");
  const [manualModelName, setManualModelName] = useState("");
  const [addingModel, setAddingModel] = useState(false);

  const [saving, setSaving] = useState(false);
  const apiKeyRef = useRef<HTMLInputElement>(null);
  const manualModelRef = useRef<HTMLInputElement>(null);

  const stepMeta = useMemo(() => {
    switch (step) {
      case "provider":
        return { n: 1, total: 2 };
      case "complete":
        return { n: 2, total: 2 };
      default:
        return null;
    }
  }, [step]);

  const providerVerified =
    step === "provider" && verifyStatus === "verified" && !!createdProviderId;

  // Models filtered + sorted (stable sort — enabled first by initial state)
  const sortedModels = useMemo(() => {
    return [...availableModels].sort((a, b) => {
      if (a.configured !== b.configured) return a.configured ? -1 : 1;
      return (a.name || a.id).localeCompare(b.name || b.id);
    });
  }, [availableModels]);

  const filteredModels = useMemo(() => {
    const query = modelSearchQuery.toLowerCase().trim();
    if (!query) return sortedModels;
    return sortedModels.filter(
      (m) =>
        m.id.toLowerCase().includes(query) ||
        (m.name && m.name.toLowerCase().includes(query)),
    );
  }, [sortedModels, modelSearchQuery]);

  const enabledCount = useMemo(
    () =>
      availableModels.filter((m) => m.configured || selectedModelIds.has(m.id))
        .length,
    [availableModels, selectedModelIds],
  );

  const resetProviderState = useCallback(() => {
    setVerifyStatus("idle");
    setProviderError(null);
    setModelError(null);
    setModelImportStatus("idle");
    setAvailableModels([]);
    setSelectedModelIds(new Set());
    setCreatedProviderId(null);
    setConfiguredModels([]);
    setModelSearchQuery("");
    setManualModelName("");
  }, []);

  const handleVendorChange = useCallback(
    (nextVendor: Vendor) => {
      setVendor(nextVendor);
      setBaseUrl(VENDOR_URLS[nextVendor]);
      resetProviderState();
    },
    [resetProviderState],
  );

  const ensureSidecar = useCallback(async () => {
    if (!api) return;
    const available = await api.sidecar.isAvailable();
    if (available) {
      setSidecarReady(true);
      return;
    }

    setSidecarReady(false);
    setDownloadError("");
    setDownloadPercent(0);
    setDownloadPhase(ob.localDownloading);

    const unsub = api.sidecar.onDownloadProgress((progress) => {
      setDownloadPhase(progress.phase);
      setDownloadPercent(progress.percent);
      if (progress.error) setDownloadError(progress.error);
    });

    try {
      const result = await api.sidecar.download();
      unsub();
      if (!result.ok) {
        setDownloadError(ob.localDownloadFailed);
        return;
      }
      setDownloadPhase(ob.localStarting);
      await api.sidecar.restart();
      setSidecarReady(true);
    } catch (error) {
      unsub();
      setDownloadError(
        error instanceof Error ? error.message : ob.localDownloadFailed,
      );
    }
  }, [api, ob.localDownloadFailed, ob.localDownloading, ob.localStarting]);

  useEffect(() => {
    if (step === "provider") {
      void ensureSidecar();
    }
  }, [ensureSidecar, step]);

  useEffect(() => {
    if (step === "provider" && sidecarReady) {
      const timer = setTimeout(() => apiKeyRef.current?.focus(), 420);
      return () => clearTimeout(timer);
    }
  }, [step, sidecarReady]);

  const handleWelcomeNext = useCallback(() => {
    setStep("mode");
  }, []);

  const handleModeSelectLocal = useCallback(async () => {
    if (!api) return;
    setStep("provider"); // immediate, no wait
    api.config.get().then((current) => api.config.set({ ...current, mode: "local" })).catch(() => {});
  }, [api]);

  const upsertProviderCredential =
    useCallback(async (): Promise<LlmProvider> => {
      const vendorOpt = VENDOR_OPTIONS.find((option) => option.key === vendor)!;
      const trimmedUrl = baseUrl.trim().replace(/\/$/, "");
      const providers = await listLlmProviders(LOCAL_ACCESS_TOKEN);
      const existing =
        providers.find(
          (provider) =>
            provider.name === vendorOpt.label &&
            providerMatches(provider, vendorOpt),
        ) ?? providers.find((provider) => providerMatches(provider, vendorOpt));

      if (existing) {
        return await updateLlmProvider(LOCAL_ACCESS_TOKEN, existing.id, {
          name: vendorOpt.label,
          provider: vendorOpt.provider,
          api_key: apiKey.trim(),
          base_url: trimmedUrl || null,
          openai_api_mode: vendorOpt.openai_api_mode ?? null,
        });
      }

      try {
        return await createLlmProvider(LOCAL_ACCESS_TOKEN, {
          name: vendorOpt.label,
          provider: vendorOpt.provider,
          api_key: apiKey.trim(),
          ...(trimmedUrl ? { base_url: trimmedUrl } : {}),
          ...(vendorOpt.openai_api_mode
            ? { openai_api_mode: vendorOpt.openai_api_mode }
            : {}),
        });
      } catch (error) {
        if (
          !isApiError(error) ||
          error.code !== "llm_providers.name_conflict"
        ) {
          throw error;
        }

        const latestProviders = await listLlmProviders(LOCAL_ACCESS_TOKEN);
        const conflicted =
          latestProviders.find(
            (provider) =>
              provider.name === vendorOpt.label &&
              providerMatches(provider, vendorOpt),
          ) ??
          latestProviders.find((provider) => provider.name === vendorOpt.label);

        if (!conflicted) throw error;

        return await updateLlmProvider(LOCAL_ACCESS_TOKEN, conflicted.id, {
          name: vendorOpt.label,
          provider: vendorOpt.provider,
          api_key: apiKey.trim(),
          base_url: trimmedUrl || null,
          openai_api_mode: vendorOpt.openai_api_mode ?? null,
        });
      }
    }, [apiKey, baseUrl, vendor]);

  const handleVerify = useCallback(async () => {
    setVerifyStatus("verifying");
    setProviderError(null);
    setModelError(null);
    setModelImportStatus("idle");
    try {
      const provider = await upsertProviderCredential();
      setCreatedProviderId(provider.id);
      setConfiguredModels(provider.models ?? []);

      // Real connectivity test: fetch available models from the provider API
      setModelImportStatus("loading");
      const response = await listAvailableModels(
        LOCAL_ACCESS_TOKEN,
        provider.id,
      );
      const models = response.models ?? [];
      setAvailableModels(models);
      setSelectedModelIds(getDefaultSelectedIds(models, vendor));
      setModelImportStatus(
        models.filter((m) => !m.configured).length > 0 ? "ready" : "empty",
      );

      // Only set verified after the real HTTP test succeeds
      setVerifyStatus("verified");
    } catch (error) {
      setVerifyStatus("failed");
      setModelImportStatus("idle");
      setProviderError(normalizeError(error, t.requestFailed));
    }
  }, [t.requestFailed, upsertProviderCredential, vendor]);

  const toggleModelSelection = useCallback((modelId: string) => {
    setSelectedModelIds((current) => {
      const next = new Set(current);
      if (next.has(modelId)) next.delete(modelId);
      else next.add(modelId);
      return next;
    });
  }, []);

  const handleAddModel = useCallback(async () => {
    const model = manualModelName.trim();
    if (!createdProviderId || !model) return;

    setAddingModel(true);
    setModelError(null);
    try {
      const created = await createProviderModel(
        LOCAL_ACCESS_TOKEN,
        createdProviderId,
        {
          model,
          is_default: configuredModels.length === 0,
        },
      );
      setConfiguredModels((current) =>
        mergeConfiguredModels(current, [created]),
      );
      setAvailableModels((current) => {
        const exists = current.some((m) => m.id === model);
        if (exists)
          return current.map((m) =>
            m.id === model ? { ...m, configured: true } : m,
          );
        return [...current, { id: model, name: model, configured: true }];
      });
      setManualModelName("");
      // Keep form visible so user can add multiple models
      setTimeout(() => manualModelRef.current?.focus(), 50);
    } catch (error) {
      setModelError(normalizeError(error, t.requestFailed));
    } finally {
      setAddingModel(false);
    }
  }, [
    configuredModels.length,
    createdProviderId,
    manualModelName,
    t.requestFailed,
  ]);

  const handleNextFromPanel = useCallback(async () => {
    if (!createdProviderId || selectedModelIds.size === 0) {
      setStep("complete");
      return;
    }

    setModelImportStatus("importing");
    setModelError(null);

    try {
      const ids = Array.from(selectedModelIds);
      const imported: LlmProviderModel[] = [];
      for (const [index, modelId] of ids.entries()) {
        const am = availableModels.find((m) => m.id === modelId);
        const created = await createProviderModel(
          LOCAL_ACCESS_TOKEN,
          createdProviderId,
          {
            model: modelId,
            is_default: configuredModels.length === 0 && index === 0,
            priority: Math.max(ids.length - index, 1),
            advanced_json: routeAdvancedJsonFromAvailableCatalog({
              id: modelId,
              name: am?.name ?? modelId,
              type: am?.type,
              context_length: am?.context_length,
              max_output_tokens: am?.max_output_tokens,
              input_modalities: am?.input_modalities,
              output_modalities: am?.output_modalities,
            }),
          },
        );
        imported.push(created);
      }

      setConfiguredModels((current) =>
        mergeConfiguredModels(current, imported),
      );
      setSelectedModelIds(new Set());
      setModelImportStatus("done");
      setStep("complete");
    } catch (error) {
      setModelImportStatus("failed");
      setModelError(normalizeError(error, t.requestFailed));
    }
  }, [
    availableModels,
    configuredModels.length,
    createdProviderId,
    selectedModelIds,
    t.requestFailed,
  ]);

  const handleBackToMode = useCallback(() => {
    setStep("mode");
  }, []);

  const handleComplete = useCallback(async () => {
    if (!api) return;
    setSaving(true);
    try {
      await api.onboarding.complete();
      onComplete();
    } finally {
      setSaving(false);
    }
  }, [api, onComplete]);

  if (!api) return null;

  return (
    <div
      style={{
        minHeight: "100vh",
        background: "var(--c-bg-page)",
        display: "flex",
        flexDirection: "column",
        position: "relative",
        overflow: "hidden",
      }}
    >
      <style>{`
        @keyframes onb-slide-in {
          from { opacity: 0; transform: translateX(20px); }
          to { opacity: 1; transform: translateX(0); }
        }
        .onb-mode-card:not(:disabled):hover {
          border-color: var(--c-border) !important;
          background: var(--c-bg-deep) !important;
        }
        .onb-model-row {
          display: flex;
          align-items: center;
          gap: 10px;
          padding: 8px 4px;
          border-bottom: 0.5px solid var(--c-border-subtle);
          transition: background 0.12s;
        }
        .onb-model-row:last-child { border-bottom: none; }
        .onb-search-wrap { position: relative; }
        .onb-search-wrap svg { position: absolute; left: 10px; top: 50%; transform: translateY(-50%); pointer-events: none; color: var(--c-placeholder); }
        .onb-search-input { padding-left: 32px !important; }
      `}</style>
      <div className="auth-dots" />
      <div className="auth-glow auth-glow-top" />
      <div className="auth-glow auth-glow-bottom" />

      <div
        style={{
          flex: 1,
          display: "flex",
          flexDirection: "column",
          alignItems: "center",
          justifyContent: "center",
          padding: "48px 20px",
          position: "relative",
          zIndex: 1,
        }}
      >
        {/* Two-column layout: right column pre-allocated on provider step to prevent jitter */}
        <div style={{ display: "flex", gap: "24px", justifyContent: "center", alignItems: "flex-start", width: "100%" }}>
          <section style={{ width: "min(520px, 100%)", flexShrink: 0 }}>
            {stepMeta && (
              <StepIndicator
                current={stepMeta.n}
                total={stepMeta.total}
                stepOf={ob.stepOf}
              />
            )}

            {/* Welcome */}
            <Reveal active={step === "welcome"}>
              <div style={{ textAlign: "center" }}>
                <div
                  style={{
                    fontSize: "28px",
                    fontWeight: 500,
                    color: "var(--c-text-primary)",
                    marginBottom: "8px",
                  }}
                >
                  {ob.welcomeTitle}
                </div>
                <div
                  style={{
                    fontSize: "14px",
                    color: "var(--c-placeholder)",
                    marginBottom: "32px",
                  }}
                >
                  {ob.welcomeDesc}
                </div>
                <button
                  type="button"
                  onClick={handleWelcomeNext}
                  style={primaryBtn}
                >
                  {ob.getStarted}
                </button>
              </div>
            </Reveal>

            {/* Mode selection */}
            <Reveal active={step === "mode"}>
              <div>
                <div
                  style={{
                    fontSize: "18px",
                    fontWeight: 500,
                    color: "var(--c-text-heading)",
                    marginBottom: "4px",
                  }}
                >
                  {ob.modeTitle}
                </div>
                <div
                  style={{
                    fontSize: "13px",
                    color: "var(--c-placeholder)",
                    marginBottom: "20px",
                  }}
                >
                  {ob.modeDesc}
                </div>
                <div
                  style={{
                    display: "flex",
                    flexDirection: "column",
                    gap: "10px",
                    marginBottom: "16px",
                  }}
                >
                  <ModeCard
                    icon={<HardDrive size={18} />}
                    title={ob.localTitle}
                    desc={ob.localDesc}
                    onClick={() => void handleModeSelectLocal()}
                  />
                  <ModeCard
                    icon={<Cloud size={18} />}
                    title={ob.saasTitle}
                    desc={ob.saasDesc}
                    disabled
                    comingSoon={t.comingSoon}
                  />
                  <ModeCard
                    icon={<Server size={18} />}
                    title={ob.selfHostTitle}
                    desc={ob.selfHostDesc}
                    disabled
                    comingSoon={t.comingSoon}
                  />
                </div>
                <button
                  type="button"
                  onClick={() => setStep("welcome")}
                  style={ghostBtn}
                >
                  {ob.back}
                </button>
              </div>
            </Reveal>

            {/* Provider configuration */}
            <Reveal active={step === "provider"}>
              <div>
                {sidecarReady === false && !downloadError && (
                  <div style={{ marginBottom: "24px" }}>
                    <div
                      style={{
                        fontSize: "14px",
                        color: "var(--c-placeholder)",
                        marginBottom: "12px",
                      }}
                    >
                      {downloadPhase || ob.localDownloading}
                    </div>
                    <ProgressBar percent={downloadPercent} />
                  </div>
                )}

                {downloadError && (
                  <div style={{ marginBottom: "24px" }}>
                    <div
                      className="flex items-center gap-2"
                      style={{
                        fontSize: "13px",
                        color: "#ef4444",
                        marginBottom: "12px",
                      }}
                    >
                      <XCircle size={14} />
                      {downloadError}
                    </div>
                    <button
                      type="button"
                      onClick={() => {
                        setDownloadError("");
                        void ensureSidecar();
                      }}
                      style={primaryBtn}
                    >
                      {ob.localRetryDownload}
                    </button>
                  </div>
                )}

                {(sidecarReady === true || sidecarReady === null) && (
                  <>
                    <div
                      style={{
                        fontSize: "18px",
                        fontWeight: 500,
                        color: "var(--c-text-heading)",
                        marginBottom: "4px",
                      }}
                    >
                      {ob.localProviderTitle}
                    </div>
                    <div
                      style={{
                        fontSize: "13px",
                        color: "var(--c-placeholder)",
                        marginBottom: "20px",
                      }}
                    >
                      {ob.localProviderDesc}
                    </div>

                    <div
                      style={{
                        display: "flex",
                        flexDirection: "column",
                        gap: "14px",
                        marginBottom: "18px",
                      }}
                    >
                      <div>
                        <label style={labelStyle}>
                          {ob.localProviderVendor}
                        </label>
                        <select
                          className={inputCls}
                          style={{ ...inputStyle, cursor: "pointer" }}
                          value={vendor}
                          onChange={(event) =>
                            handleVendorChange(event.target.value as Vendor)
                          }
                        >
                          {VENDOR_OPTIONS.map((option) => (
                            <option key={option.key} value={option.key}>
                              {option.label}
                            </option>
                          ))}
                        </select>
                      </div>

                      <div>
                        <label style={labelStyle}>
                          {ob.localProviderApiKey}
                        </label>
                        <input
                          ref={apiKeyRef}
                          className={inputCls}
                          style={inputStyle}
                          type="password"
                          placeholder={ob.localProviderApiKeyPlaceholder}
                          value={apiKey}
                          onChange={(event) => {
                            setApiKey(event.target.value);
                            resetProviderState();
                          }}
                          autoComplete="off"
                        />
                      </div>

                      <div>
                        <label style={labelStyle}>
                          {ob.localProviderBaseUrl}
                        </label>
                        <input
                          className={inputCls}
                          style={inputStyle}
                          type="text"
                          placeholder={ob.localProviderBaseUrlPlaceholder}
                          value={baseUrl}
                          onChange={(event) => {
                            setBaseUrl(event.target.value);
                            resetProviderState();
                          }}
                        />
                      </div>
                    </div>

                    {verifyStatus === "verified" && (
                      <div
                        style={{
                          fontSize: "13px",
                          color: "#22c55e",
                          marginBottom: "12px",
                        }}
                      >
                        {ob.localProviderVerified}
                      </div>
                    )}

                    {verifyStatus === "failed" && !providerError && (
                      <div
                        className="flex items-center gap-2"
                        style={{
                          fontSize: "13px",
                          color: "#ef4444",
                          marginBottom: "12px",
                        }}
                      >
                        <XCircle size={14} />
                        {ob.localProviderFailed}
                      </div>
                    )}

                    {providerError && (
                      <ErrorCallout
                        error={providerError}
                        locale={locale}
                        requestFailedText={t.requestFailed}
                      />
                    )}

                    <button
                      type="button"
                      onClick={() => void handleVerify()}
                      disabled={!apiKey.trim() || verifyStatus === "verifying"}
                      style={primaryBtn}
                      className="disabled:cursor-not-allowed disabled:opacity-50"
                    >
                      {verifyStatus === "verifying" ? (
                        <>
                          <SpinnerIcon /> {ob.localProviderVerifying}
                        </>
                      ) : (
                        ob.localProviderVerify
                      )}
                    </button>

                    <button
                      type="button"
                      onClick={
                        verifyStatus === "verified"
                          ? handleBackToMode
                          : () => setStep("complete")
                      }
                      style={ghostBtn}
                    >
                      {verifyStatus === "verified"
                        ? ob.back
                        : ob.localProviderSkip}
                    </button>
                  </>
                )}
              </div>
            </Reveal>

            {/* Completion */}
            <Reveal active={step === "complete"}>
              <div style={{ textAlign: "center" }}>
                <CheckCircle
                  size={40}
                  style={{ color: "#22c55e", margin: "0 auto 16px" }}
                />
                <div
                  style={{
                    fontSize: "18px",
                    fontWeight: 500,
                    color: "var(--c-text-heading)",
                    marginBottom: "8px",
                  }}
                >
                  {ob.completionTitle}
                </div>
                <div
                  style={{
                    fontSize: "14px",
                    color: "var(--c-placeholder)",
                    marginBottom: "12px",
                  }}
                >
                  {ob.completionDesc}
                </div>
                <div
                  className="flex items-center justify-center gap-2"
                  style={{
                    fontSize: "13px",
                    color: "var(--c-text-muted)",
                    marginBottom: "32px",
                    padding: "10px 16px",
                    borderRadius: "10px",
                    background: "var(--c-bg-menu)",
                    border: "0.5px solid var(--c-border-subtle)",
                  }}
                >
                  <Settings size={14} />
                  {ob.completionModulesHint}
                </div>
                <button
                  type="button"
                  onClick={() => void handleComplete()}
                  disabled={saving}
                  style={primaryBtn}
                  className="disabled:cursor-not-allowed disabled:opacity-50"
                >
                  {saving ? <SpinnerIcon /> : ob.startChatting}
                </button>
              </div>
            </Reveal>
          </section>

          {/* Right panel — always pre-allocated when on provider step, invisible until verified */}
          {/* Using opacity so layout is stable (no jitter when it appears) */}
          {step === "provider" && (
            <div
              style={{
                width: providerVerified ? "min(360px, 40vw)" : "0px",
                flexShrink: 0,
                alignSelf: "flex-start",
                overflow: "hidden",
                transition: "width 0.38s cubic-bezier(0.4,0,0.2,1)",
              }}
            >
              <div
                style={{
                  width: "min(360px, 40vw)",
                  opacity: providerVerified ? 1 : 0,
                  transition: "opacity 0.3s ease 0.1s",
                  pointerEvents: providerVerified ? "auto" : "none",
                }}
              >
              <div
                style={{
                  ...sectionCardStyle,
                  display: "flex",
                  flexDirection: "column",
                  maxHeight: "420px",
                  gap: 0,
                }}
              >
                {/* Header */}
                <div style={{ marginBottom: "12px", flexShrink: 0 }}>
                  <div
                    style={{
                      fontSize: "14px",
                      fontWeight: 500,
                      color: "var(--c-text-heading)",
                    }}
                  >
                    {ob.localImportModels}
                  </div>
                  <div
                    style={{
                      fontSize: "12px",
                      color: "var(--c-placeholder)",
                      marginTop: "3px",
                    }}
                  >
                    {ob.localImportModelsDesc}
                  </div>
                </div>

                {/* Search */}
                <div
                  className="onb-search-wrap"
                  style={{ marginBottom: "8px", flexShrink: 0 }}
                >
                  <Search size={14} />
                  <input
                    className={`${inputCls} onb-search-input`}
                    style={{ ...inputStyle, height: "34px", fontSize: "13px" }}
                    type="text"
                    placeholder={ob.localSearchModels}
                    value={modelSearchQuery}
                    onChange={(e) => setModelSearchQuery(e.target.value)}
                  />
                </div>

                {/* Model count */}
                {modelImportStatus !== "loading" &&
                  availableModels.length > 0 && (
                    <div
                      style={{
                        fontSize: "11px",
                        color: "var(--c-text-muted)",
                        marginBottom: "6px",
                        flexShrink: 0,
                      }}
                    >
                      {ob.localModelsShowing(
                        filteredModels.length,
                        enabledCount,
                      )}
                    </div>
                  )}

                {/* Model list */}
                <div
                  style={{
                    flex: 1,
                    overflowY: "auto",
                    minHeight: 0,
                    margin: "0 -2px",
                    padding: "0 2px",
                  }}
                >
                  {modelImportStatus === "loading" && (
                    <div
                      className="flex items-center gap-2"
                      style={{
                        fontSize: "13px",
                        color: "var(--c-text-muted)",
                        padding: "12px 0",
                      }}
                    >
                      <SpinnerIcon />
                      {ob.localImportingModels}
                    </div>
                  )}

                  {modelImportStatus !== "loading" &&
                    filteredModels.length === 0 && (
                      <div
                        style={{
                          fontSize: "13px",
                          color: "var(--c-text-muted)",
                          padding: "12px 0",
                        }}
                      >
                        {modelSearchQuery
                          ? ob.localNoModels
                          : ob.localNoImportableModels}
                      </div>
                    )}

                  {filteredModels.map((model) => {
                    const isOn =
                      model.configured || selectedModelIds.has(model.id);
                    return (
                      <div key={model.id} className="onb-model-row">
                        <div style={{ flex: 1, minWidth: 0 }}>
                          <div
                            style={{
                              fontSize: "13px",
                              color: "var(--c-text-primary)",
                              fontWeight: 450,
                              overflow: "hidden",
                              textOverflow: "ellipsis",
                              whiteSpace: "nowrap",
                            }}
                          >
                            {model.name || model.id}
                          </div>
                          {model.name && model.name !== model.id && (
                            <div
                              style={{
                                fontSize: "11px",
                                color: "var(--c-text-muted)",
                                overflow: "hidden",
                                textOverflow: "ellipsis",
                                whiteSpace: "nowrap",
                              }}
                            >
                              {model.id}
                            </div>
                          )}
                        </div>
                        <ToggleSwitch
                          checked={isOn}
                          onChange={
                            model.configured
                              ? undefined
                              : () => toggleModelSelection(model.id)
                          }
                          disabled={model.configured}
                        />
                      </div>
                    );
                  })}
                </div>

                {/* Add model inline */}
                <div
                  style={{
                    marginTop: "10px",
                    paddingTop: "10px",
                    borderTop: "0.5px solid var(--c-border-subtle)",
                    flexShrink: 0,
                  }}
                >
                  {modelError && (
                    <div style={{ marginBottom: "8px" }}>
                      <ErrorCallout
                        error={modelError}
                        locale={locale}
                        requestFailedText={t.requestFailed}
                      />
                    </div>
                  )}
                  <div className="flex gap-2">
                    <input
                      ref={manualModelRef}
                      className={inputCls}
                      style={{
                        ...inputStyle,
                        flex: 1,
                        height: "34px",
                        fontSize: "13px",
                      }}
                      type="text"
                      placeholder={ob.localManualModelPlaceholder}
                      value={manualModelName}
                      onChange={(e) => setManualModelName(e.target.value)}
                      onKeyDown={(e) => {
                        if (e.key === "Enter") void handleAddModel();
                      }}
                    />
                    <button
                      type="button"
                      onClick={() => void handleAddModel()}
                      disabled={!manualModelName.trim() || addingModel}
                      style={{
                        height: "34px",
                        borderRadius: "10px",
                        border: "0.5px solid var(--c-border-subtle)",
                        background: "var(--c-bg-page)",
                        color: "var(--c-text-secondary)",
                        fontSize: "13px",
                        fontFamily: "inherit",
                        cursor: "pointer",
                        padding: "0 14px",
                        flexShrink: 0,
                        display: "flex",
                        alignItems: "center",
                        gap: "4px",
                        fontWeight: 500,
                        whiteSpace: "nowrap",
                      }}
                      className="disabled:cursor-not-allowed disabled:opacity-50"
                    >
                      {addingModel ? <SpinnerIcon /> : ob.localAddModel}
                    </button>
                  </div>
                </div>

                {/* Next button */}
                <button
                  type="button"
                  onClick={() => void handleNextFromPanel()}
                  disabled={modelImportStatus === "importing"}
                  style={{ ...primaryBtn, marginTop: "10px", flexShrink: 0 }}
                  className="disabled:cursor-not-allowed disabled:opacity-50"
                >
                  {modelImportStatus === "importing" ? (
                    <>
                      <SpinnerIcon /> {ob.localImportingModels}
                    </>
                  ) : (
                    ob.next
                  )}
                </button>
              </div>
              </div>
            </div>
          )}
        </div>
      </div>

      <footer
        style={{
          textAlign: "center",
          padding: "16px",
          fontSize: "12px",
          color: "var(--c-text-muted)",
          position: "relative",
          zIndex: 1,
        }}
      >
        &copy; 2026 Arkloop
      </footer>
    </div>
  );
}
