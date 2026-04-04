import { useEffect, useMemo, useRef, useState } from "react";
import { motion } from "framer-motion";
import {
  ChevronLeft,
  SlidersHorizontal,
  Settings,
  Cpu,
  Brain,
  Database,
  Bot,
  Radio,
  Puzzle,
  Server,
  Palette,
  Route,
  MessageSquare,
  Wrench,
  Code2,
  Loader2,
  Shield,
} from "lucide-react";
import { getDesktopApi } from "@arkloop/shared/desktop";
import type { MeResponse } from "../api";
import type { DesktopConfig } from "@arkloop/shared/desktop";
import { listPlatformSettings } from "../api-admin";
import { bridgeClient } from "../api-bridge";
import { useLocale } from "../contexts/LocaleContext";
import { readDeveloperMode } from "../storage";
import { GeneralSettings } from "./settings/GeneralSettings";
import { DesktopAppearanceSettings } from "./settings/DesktopAppearanceSettings";
import { ProvidersSettings } from "./settings/ProvidersSettings";
import { RoutingSettings } from "./settings/RoutingSettings";
import { PersonasSettings } from "./settings/PersonasSettings";
import { DesktopChannelsSettings } from "./settings/DesktopChannelsSettings";
import { SkillsSettings } from "./settings/SkillsSettings";
import { MCPSettings } from "./settings/MCPSettings";
import { ToolsSettings } from "./settings/ToolsSettings";
import { AdvancedSettings } from "./settings/AdvancedSettings";
import { MemorySettings } from "./settings/MemorySettings";
import { NotebookSettings } from "./settings/NotebookSettings";
import { ConnectionSettings } from "./settings/ConnectionSettings";
import { ChatSettings } from "./settings/ChatSettings";
import { ExtensionsSettings } from "./settings/ExtensionsSettings";
import { ModulesSettings } from "./settings/ModulesSettings";
import { DeveloperSettings } from "./settings/DeveloperSettings";
import { DesktopPromptInjectionSettings } from "./settings/DesktopPromptInjectionSettings";
import { VoiceSettings } from "./settings/VoiceSettings";
import { DesignTokensSettings } from "./settings/DesignTokensSettings";

export type DesktopSettingsKey =
  | "general"
  | "appearance"
  | "providers"
  | "routing"
  | "personas"
  | "channels"
  | "skills"
  | "mcp"
  | "tools"
  | "advanced"
  | "notebook"
  | "memory"
  | "connection"
  | "chat"
  | "voice"
  | "promptInjection"
  | "modules"
  | "extensions"
  | "developer"
  | "design-tokens";

type NavItem = {
  key: DesktopSettingsKey;
  icon: typeof Settings;
};

type NavEntry = NavItem | { header: string };

const NAV_ENTRIES: NavEntry[] = [
  // 第一段：基础用户设置（无 header，"< 设置"返回按钮充当隐含 header）
  { key: "general",    icon: Settings },
  { key: "appearance", icon: Palette },
  { key: "providers",  icon: Cpu },
  { key: "channels",   icon: Radio },
  // 第二段：agent 核心组件（英文专有名词区）
  { header: "agentCoreHeader" },
  { key: "skills",           icon: Puzzle },
  { key: "mcp",              icon: Server },
  { key: "notebook",         icon: Brain },
  { key: "memory",           icon: Database },
  { key: "chat",             icon: MessageSquare },
  { key: "promptInjection",  icon: Shield },
  // 第三段：低频管理
  { header: "managementHeader" },
  { key: "tools",      icon: Wrench },
  { key: "personas",   icon: Bot },
  { key: "routing",    icon: Route },
  { key: "advanced",   icon: SlidersHorizontal },
];

function SettingsPaneFallback() {
  return (
    <div className="flex min-h-[240px] items-center justify-center">
      <Loader2 size={18} className="animate-spin text-[var(--c-text-muted)]" />
    </div>
  );
}

type Props = {
  me: MeResponse | null;
  accessToken: string;
  initialSection?: DesktopSettingsKey;
  onClose: () => void;
  onLogout: () => void;
  onMeUpdated?: (me: MeResponse) => void;
  onTrySkill?: (prompt: string) => void;
};

export type DesktopSettingsHydrationSnapshot = {
  config: DesktopConfig | null;
  platformSettings: Record<string, string> | null;
  executionMode: "local" | "vm" | null;
  platformSettingsError: string;
  executionModeError: string;
};

export function DesktopSettings({
  me,
  accessToken,
  initialSection = "general",
  onClose,
  onLogout,
  onMeUpdated,
  onTrySkill,
}: Props) {
  const { t } = useLocale();
  const ds = t.desktopSettings;
  const desktopApi = useMemo(() => getDesktopApi(), []);
  const [activeKey, setActiveKey] =
    useState<DesktopSettingsKey>(initialSection);
  const [scrolled, setScrolled] = useState(false);
  const scrollRef = useRef<HTMLDivElement>(null);
  const [devMode, setDevMode] = useState(() => readDeveloperMode());
  const [hydrationLoading, setHydrationLoading] = useState(true);
  const [hydrationSnapshot, setHydrationSnapshot] =
    useState<DesktopSettingsHydrationSnapshot>({
      config: null,
      platformSettings: null,
      executionMode: null,
      platformSettingsError: "",
      executionModeError: "",
    });

  useEffect(() => {
    const handler = (e: Event) => setDevMode((e as CustomEvent<boolean>).detail);
    window.addEventListener("arkloop:developer_mode", handler);
    return () => window.removeEventListener("arkloop:developer_mode", handler);
  }, []);

  useEffect(() => {
    let cancelled = false;

    const loadSnapshot = async () => {
      setHydrationLoading(true);
      const [configResult, platformResult, executionResult] = await Promise.allSettled([
        desktopApi?.config.get() ?? Promise.resolve(null),
        listPlatformSettings(accessToken),
        bridgeClient.getExecutionMode(),
      ]);

      if (cancelled) return;

      setHydrationSnapshot({
        config:
          configResult.status === "fulfilled"
            ? configResult.value
            : null,
        platformSettings:
          platformResult.status === "fulfilled"
            ? Object.fromEntries(platformResult.value.map((row) => [row.key, row.value]))
            : null,
        executionMode:
          executionResult.status === "fulfilled"
            ? executionResult.value
            : null,
        platformSettingsError:
          platformResult.status === "rejected"
            ? (platformResult.reason instanceof Error ? platformResult.reason.message : t.requestFailed)
            : "",
        executionModeError:
          executionResult.status === "rejected"
            ? (executionResult.reason instanceof Error ? executionResult.reason.message : t.requestFailed)
            : "",
      });
      setHydrationLoading(false);
    };

    void loadSnapshot();

    return () => {
      cancelled = true;
    };
  }, [accessToken, desktopApi, t.requestFailed]);

  useEffect(() => {
    if (!desktopApi?.config) return;
    return desktopApi.config.onChanged((config) => {
      setHydrationSnapshot((current) => ({ ...current, config }));
    });
  }, [desktopApi]);

  const navEntries = useMemo(() => {
    const entries = [...NAV_ENTRIES];
    if (devMode) entries.push({ key: "developer" as DesktopSettingsKey, icon: Code2 });
    return entries;
  }, [devMode]);

  const handleTabChange = (key: DesktopSettingsKey) => {
    setActiveKey(key);
    setScrolled(false);
    if (scrollRef.current) scrollRef.current.scrollTop = 0;
  };

  const renderNav = (entries: NavEntry[]) =>
    entries.map((entry) => {
      if ("header" in entry) {
        return (
          <div
            key={entry.header}
            className="mt-4 px-2.5 pb-1 pt-1 text-[12px] font-[375] text-[var(--c-text-tertiary)]"
          >
            {(ds as unknown as Record<string, string>)[entry.header]}
          </div>
        );
      }
      const { key, icon: Icon } = entry;
      return (
        <button
          key={key}
          onClick={() => handleTabChange(key)}
          className={[
            "flex h-[38px] items-center gap-2.5 rounded-lg px-2.5 text-[14px] font-[425] transition-all duration-[120ms] active:scale-[0.96]",
            activeKey === key
              ? "bg-[var(--c-bg-deep)] text-[var(--c-text-heading)] rounded-[10px]"
              : "text-[var(--c-text-secondary)] hover:bg-[color-mix(in_srgb,var(--c-bg-deep)_60%,transparent)] hover:text-[var(--c-text-heading)]",
          ].join(" ")}
        >
          <Icon size={16} />
          <span>{(ds as unknown as Record<string, string>)[key]}</span>
        </button>
      );
    });

  const renderContent = () => {
    switch (activeKey) {
      case "general":
        return (
          <GeneralSettings
            me={me}
            accessToken={accessToken}
            onLogout={onLogout}
            onMeUpdated={onMeUpdated}
          />
        );
      case "appearance":
        return <DesktopAppearanceSettings />;
      case "providers":
        return <ProvidersSettings accessToken={accessToken} />;
      case "routing":
        return <RoutingSettings accessToken={accessToken} />;
      case "personas":
        return <PersonasSettings accessToken={accessToken} />;
      case "channels":
        return <DesktopChannelsSettings accessToken={accessToken} />;
      case "skills":
        return (
          <SkillsSettings accessToken={accessToken} onTrySkill={onTrySkill} />
        );
      case "mcp":
        return <MCPSettings accessToken={accessToken} />;
      case "tools":
        return <ToolsSettings accessToken={accessToken} />;
      case "advanced":
        return <AdvancedSettings accessToken={accessToken} />;
      case "notebook":
        return <NotebookSettings />;
      case "memory":
        return <MemorySettings accessToken={accessToken} />;
      case "connection":
        return <ConnectionSettings initialConfig={hydrationSnapshot.config} />;
      case "chat":
        return (
          <ChatSettings
            accessToken={accessToken}
            initialSnapshot={hydrationSnapshot}
            onExecutionModeChange={(executionMode) => {
              setHydrationSnapshot((current) => ({ ...current, executionMode, executionModeError: "" }));
            }}
            onPlatformSettingsChange={(updates) => {
              setHydrationSnapshot((current) => ({
                ...current,
                platformSettings: {
                  ...(current.platformSettings ?? {}),
                  ...updates,
                },
                platformSettingsError: "",
              }));
            }}
          />
        );
      case "voice":
        return <VoiceSettings accessToken={accessToken} initialConfig={hydrationSnapshot.config} />;
      case "promptInjection":
        return <DesktopPromptInjectionSettings accessToken={accessToken} />;
      case "modules":
        return <ModulesSettings />;
      case "extensions":
        return <ExtensionsSettings />;
      case "developer":
        return <DeveloperSettings accessToken={accessToken} onNavigate={handleTabChange} />;
      case "design-tokens":
        return <DesignTokensSettings />;
      default:
        return null;
    }
  };

  return (
    <motion.div
      className="flex h-full min-h-0 min-w-0 flex-1 overflow-hidden"
      initial={{ opacity: 0, x: 10 }}
      animate={{ opacity: 1, x: 0 }}
      transition={{ duration: 0.18, ease: [0.16, 1, 0.3, 1] }}
    >
      {/* Left navigation sidebar */}
      <div
        className="flex w-[280px] shrink-0 flex-col overflow-y-auto py-4"
        style={{ borderRight: "0.5px solid var(--c-border-subtle)" }}
      >
        {/* Back button / header */}
        <div className="mb-4 px-4">
          <button
            onClick={onClose}
            className="flex h-[38px] w-full items-center gap-2.5 rounded-lg px-2.5 text-[14px] font-medium transition-colors text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-heading)]"
          >
            <ChevronLeft size={16} />
            {ds.settingsTitle}
          </button>
        </div>

        <div className="px-4">
          <div className="flex flex-col gap-[3px]">{renderNav(navEntries)}</div>
        </div>
      </div>

      {/* Right content area with scroll-aware top fade mask */}
      <div className="relative flex min-w-0 flex-1 overflow-hidden">
        {/* Fade mask — only visible once the user has scrolled down */}
        <div
          className="pointer-events-none absolute left-0 right-0 top-0 z-10 h-8 transition-opacity duration-200"
          style={{
            background: 'linear-gradient(to bottom, var(--c-bg-page) 0%, transparent 100%)',
            opacity: scrolled ? 1 : 0,
          }}
        />
        <div
          ref={scrollRef}
          className="flex min-w-0 flex-1 flex-col overflow-y-auto p-6"
          style={{ scrollbarGutter: 'stable' }}
          onScroll={(e) => setScrolled((e.currentTarget as HTMLDivElement).scrollTop > 8)}
        >
          {hydrationLoading ? <SettingsPaneFallback /> : renderContent()}
        </div>
      </div>
    </motion.div>
  );
}
