import { Suspense, lazy, useRef, useState } from "react";
import { motion } from "framer-motion";
import {
  ChevronLeft,
  SlidersHorizontal,
  Settings,
  Cpu,
  Bot,
  Radio,
  Puzzle,
  Server,
  Palette,
  Route,
  MessageSquare,
  Wrench,
  Loader2,
} from "lucide-react";
import type { MeResponse } from "../api";
import { useLocale } from "../contexts/LocaleContext";

const GeneralSettings = lazy(async () => ({ default: (await import("./settings/GeneralSettings")).GeneralSettings }));
const DesktopAppearanceSettings = lazy(async () => ({ default: (await import("./settings/DesktopAppearanceSettings")).DesktopAppearanceSettings }));
const ProvidersSettings = lazy(async () => ({ default: (await import("./settings/ProvidersSettings")).ProvidersSettings }));
const RoutingSettings = lazy(async () => ({ default: (await import("./settings/RoutingSettings")).RoutingSettings }));
const PersonasSettings = lazy(async () => ({ default: (await import("./settings/PersonasSettings")).PersonasSettings }));
const DesktopChannelsSettings = lazy(async () => ({ default: (await import("./settings/DesktopChannelsSettings")).DesktopChannelsSettings }));
const SkillsSettings = lazy(async () => ({ default: (await import("./settings/SkillsSettings")).SkillsSettings }));
const MCPSettings = lazy(async () => ({ default: (await import("./settings/MCPSettings")).MCPSettings }));
const ToolsSettings = lazy(async () => ({ default: (await import("./settings/ToolsSettings")).ToolsSettings }));
const AdvancedSettings = lazy(async () => ({ default: (await import("./settings/AdvancedSettings")).AdvancedSettings }));
const MemorySettings = lazy(async () => ({ default: (await import("./settings/MemorySettings")).MemorySettings }));
const ConnectionSettings = lazy(async () => ({ default: (await import("./settings/ConnectionSettings")).ConnectionSettings }));
const ChatSettings = lazy(async () => ({ default: (await import("./settings/ChatSettings")).ChatSettings }));
const ExtensionsSettings = lazy(async () => ({ default: (await import("./settings/ExtensionsSettings")).ExtensionsSettings }));
const ModulesSettings = lazy(async () => ({ default: (await import("./settings/ModulesSettings")).ModulesSettings }));
const DeveloperSettings = lazy(async () => ({ default: (await import("./settings/DeveloperSettings")).DeveloperSettings }));
const DesktopPromptInjectionSettings = lazy(async () => ({ default: (await import("./settings/DesktopPromptInjectionSettings")).DesktopPromptInjectionSettings }));
const VoiceSettings = lazy(async () => ({ default: (await import("./settings/VoiceSettings")).VoiceSettings }));
const DesignTokensSettings = lazy(async () => ({ default: (await import("./settings/DesignTokensSettings")).DesignTokensSettings }));

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

const NAV_ITEMS: NavItem[] = [
  { key: "general",    icon: Settings },
  { key: "appearance", icon: Palette },
  { key: "providers",  icon: Cpu },
  { key: "routing",    icon: Route },
  { key: "personas",   icon: Bot },
  { key: "channels",   icon: Radio },
  { key: "skills",     icon: Puzzle },
  { key: "mcp",        icon: Server },
  { key: "tools",      icon: Wrench },
  { key: "chat",       icon: MessageSquare },
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
  const [activeKey, setActiveKey] =
    useState<DesktopSettingsKey>(initialSection);
  const [scrolled, setScrolled] = useState(false);
  const scrollRef = useRef<HTMLDivElement>(null);

  const handleTabChange = (key: DesktopSettingsKey) => {
    setActiveKey(key);
    setScrolled(false);
    if (scrollRef.current) scrollRef.current.scrollTop = 0;
  };

  const renderNav = (items: NavItem[]) =>
    items.map(({ key, icon: Icon }) => (
      <button
        key={key}
        onClick={() => handleTabChange(key)}
        className={[
          "flex h-[38px] items-center gap-2.5 rounded-lg px-2.5 text-[14px] font-medium transition-all duration-[120ms] active:scale-[0.96]",
          activeKey === key
            ? "bg-[var(--c-bg-deep)] text-[var(--c-text-heading)] rounded-[10px]"
            : "text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-heading)]",
        ].join(" ")}
      >
        <Icon size={16} />
        <span>{(ds as unknown as Record<string, string>)[key]}</span>
      </button>
    ));

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
      case "memory":
        return <MemorySettings accessToken={accessToken} />;
      case "connection":
        return <ConnectionSettings />;
      case "chat":
        return <ChatSettings accessToken={accessToken} />;
      case "voice":
        return <VoiceSettings accessToken={accessToken} />;
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
      className="flex h-full min-h-0 flex-1"
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
          <div className="flex flex-col gap-[3px]">{renderNav(NAV_ITEMS)}</div>
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
          onScroll={(e) => setScrolled((e.currentTarget as HTMLDivElement).scrollTop > 8)}
        >
          <Suspense fallback={<SettingsPaneFallback />}>
            {renderContent()}
          </Suspense>
        </div>
      </div>
    </motion.div>
  );
}
