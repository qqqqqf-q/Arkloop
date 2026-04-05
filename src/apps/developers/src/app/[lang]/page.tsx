'use client';

import { use, useState, useEffect } from 'react';
import Link from 'next/link';
import styles from './landing.module.css';

type Lang = 'zh' | 'en';

const CONTENT: Record<Lang, {
  heroTitle: string[];
  heroDesc: string;
  dlBtnLabel: string;
  docsBtnLabel: string;
  docsHref: string;
  platforms: { label: string; url: string; icon: string }[];
  contribTitle: string;
  contribDesc: string;
  githubLabel: string;
  contribGuideLabel: string;
  contribGuideHref: string;
}> = {
  zh: {
    heroTitle: ['干净、强大，属于你的', 'AI Agent 平台'],
    heroDesc: '你的私人 AI 运行时 -- 工具执行、记忆系统、沙盒代码、多模型路由，全部运行在你自己的设备上。',
    dlBtnLabel: '下载 Arkloop',
    docsBtnLabel: '文档',
    docsHref: '/zh/docs/guide',
    platforms: [
      { label: 'macOS (Apple Silicon)', url: 'https://github.com/qqqqqf-q/Arkloop/releases/latest/download/Arkloop-mac-arm64.dmg', icon: 'apple' },
      { label: 'macOS (Intel)', url: 'https://github.com/qqqqqf-q/Arkloop/releases/latest/download/Arkloop-mac-x64.dmg', icon: 'apple' },
      { label: 'Windows (x64)', url: 'https://github.com/qqqqqf-q/Arkloop/releases/latest/download/Arkloop-win-x64.exe', icon: 'windows' },
      { label: 'Linux (x64)', url: 'https://github.com/qqqqqf-q/Arkloop/releases/latest/download/Arkloop-linux-x86_64.AppImage', icon: 'linux' },
    ],
    contribTitle: '贡献',
    contribDesc: '我们欢迎所有形式的贡献。即使你不是开发者，只是一个普通用户 -- 如果你在使用中感到任何不舒服的地方，哪怕只是一点间距、一个颜色、一个很小很小的细节，或者是一个很大很大的方向，都可以直接开一个 issue。我们认真对待每一个体验细节，你的反馈会让所有人的体验变得更好。',
    githubLabel: 'GitHub',
    contribGuideLabel: '贡献指南',
    contribGuideHref: '/zh/docs/guide',
  },
  en: {
    heroTitle: ['Clean, powerful AI agents.', 'Yours to own.'],
    heroDesc: 'Your personal AI runtime -- tool execution, memory, sandboxed code, and multi-model routing, all running on your own device.',
    dlBtnLabel: 'Get Arkloop',
    docsBtnLabel: 'Docs',
    docsHref: '/en/docs/guide',
    platforms: [
      { label: 'macOS (Apple Silicon)', url: 'https://github.com/qqqqqf-q/Arkloop/releases/latest/download/Arkloop-mac-arm64.dmg', icon: 'apple' },
      { label: 'macOS (Intel)', url: 'https://github.com/qqqqqf-q/Arkloop/releases/latest/download/Arkloop-mac-x64.dmg', icon: 'apple' },
      { label: 'Windows (x64)', url: 'https://github.com/qqqqqf-q/Arkloop/releases/latest/download/Arkloop-win-x64.exe', icon: 'windows' },
      { label: 'Linux (x64)', url: 'https://github.com/qqqqqf-q/Arkloop/releases/latest/download/Arkloop-linux-x86_64.AppImage', icon: 'linux' },
    ],
    contribTitle: 'Contributing',
    contribDesc: "We welcome contributions of all kinds. Even if you're not a developer -- if something feels off, a bit of spacing, a color that doesn't sit right, any tiny detail or even a big-picture direction -- please open an issue. We take every UX detail seriously, and your feedback makes Arkloop better for everyone.",
    githubLabel: 'GitHub',
    contribGuideLabel: 'Contributing Guide',
    contribGuideHref: '/en/docs/guide',
  },
};

const ICONS = {
  apple: (
    <svg className={styles.dlOptIcon} viewBox="0 0 24 24" fill="currentColor">
      <path d="M18.71 19.5c-.83 1.24-1.71 2.45-3.05 2.47-1.34.03-1.77-.79-3.29-.79-1.53 0-2 .77-3.27.82-1.31.05-2.3-1.32-3.14-2.53C4.25 17 2.94 12.45 4.7 9.39c.87-1.52 2.43-2.48 4.12-2.51 1.28-.02 2.5.87 3.29.87.78 0 2.26-1.07 3.8-.91.65.03 2.47.26 3.64 1.98-.09.06-2.17 1.28-2.15 3.81.03 3.02 2.65 4.03 2.68 4.04-.03.07-.42 1.44-1.38 2.83M13 3.5c.73-.83 1.94-1.46 2.94-1.5.13 1.17-.34 2.35-1.04 3.19-.69.85-1.83 1.51-2.95 1.42-.15-1.15.41-2.35 1.05-3.11z" />
    </svg>
  ),
  windows: (
    <svg className={styles.dlOptIcon} viewBox="0 0 24 24" fill="currentColor">
      <path d="M3 12V6.75l6-1.32v6.57H3zM20 3v8.75h-8V4.38L20 3zM3 13h6v6.43l-6-1.33V13zm17 .25V22l-8-1.42V13.25h8z" />
    </svg>
  ),
  linux: (
    <svg className={styles.dlOptIcon} viewBox="0 0 24 24" fill="currentColor">
      <path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm-1 14H9V8h2v8zm4 0h-2V8h2v8z" />
    </svg>
  ),
};

export default function LandingPage({
  params,
}: {
  params: Promise<{ lang: string }>;
}) {
  const { lang: rawLang } = use(params);
  const lang: Lang = rawLang === 'en' ? 'en' : 'zh';
  const [dlOpen, setDlOpen] = useState(false);
  const c = CONTENT[lang];

  useEffect(() => {
    if (!dlOpen) return;
    const handler = (e: MouseEvent) => {
      const target = e.target as Node;
      const wrap = document.getElementById('dl-btn-wrap');
      if (wrap && !wrap.contains(target)) setDlOpen(false);
    };
    document.addEventListener('click', handler);
    return () => document.removeEventListener('click', handler);
  }, [dlOpen]);

  return (
    <main className={styles.landingWrap}>
      {/* Hero */}
      <section className={styles.hero}>
        <h1 className={styles.heroTitle}>
          {c.heroTitle[0]}
          <br />
          {c.heroTitle[1]}
        </h1>
        <p className={styles.heroDesc}>{c.heroDesc}</p>
        <div className={styles.heroActions}>
          <div className={styles.dlBtnWrap} id="dl-btn-wrap">
            <button
              className={styles.btnPrimary}
              onClick={() => setDlOpen((v) => !v)}
            >
              <span>{c.dlBtnLabel}</span>
              <svg className={styles.dlChevron} viewBox="0 0 10 6" fill="none">
                <path d="M1 1l4 4 4-4" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
              </svg>
            </button>
            {dlOpen && (
              <div className={styles.dlDropdown}>
                {c.platforms.map((p) => (
                  <a key={p.label} className={styles.dlOption} href={p.url}>
                    {ICONS[p.icon as keyof typeof ICONS]}
                    {p.label}
                  </a>
                ))}
              </div>
            )}
          </div>
          <Link href={c.docsHref} className={styles.btnSecondary}>
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <path d="M2 3h6a4 4 0 0 1 4 4v14a3 3 0 0 0-3-3H2z" />
              <path d="M22 3h-6a4 4 0 0 0-4 4v14a3 3 0 0 1 3-3h7z" />
            </svg>
            {c.docsBtnLabel}
          </Link>
        </div>
        <div className={styles.heroMeta}>
          <a className={styles.metaLink} href="https://github.com/qqqqqf-q/Arkloop" target="_blank" rel="noopener">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor">
              <path d="M12 2C6.477 2 2 6.484 2 12.017c0 4.425 2.865 8.18 6.839 9.504.5.092.682-.217.682-.483 0-.237-.008-.868-.013-1.703-2.782.605-3.369-1.343-3.369-1.343-.454-1.158-1.11-1.466-1.11-1.466-.908-.62.069-.608.069-.608 1.003.07 1.531 1.032 1.531 1.032.892 1.53 2.341 1.088 2.91.832.092-.647.35-1.088.636-1.338-2.22-.253-4.555-1.113-4.555-4.951 0-1.093.39-1.988 1.029-2.688-.103-.253-.446-1.272.098-2.65 0 0 .84-.27 2.75 1.026A9.564 9.564 0 0112 6.844c.85.004 1.705.115 2.504.337 1.909-1.296 2.747-1.027 2.747-1.027.546 1.379.202 2.398.1 2.651.64.7 1.028 1.595 1.028 2.688 0 3.848-2.339 4.695-4.566 4.943.359.309.678.92.678 1.855 0 1.338-.012 2.419-.012 2.747 0 .268.18.58.688.482A10.019 10.019 0 0022 12.017C22 6.484 17.522 2 12 2z" />
            </svg>
            GitHub
          </a>
          <a className={styles.metaLink} href="https://t.me/Arkloop_io" target="_blank" rel="noopener">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor">
              <path d="M12 0C5.373 0 0 5.373 0 12s5.373 12 12 12 12-5.373 12-12S18.627 0 12 0zm5.562 8.248l-1.97 9.289c-.145.658-.537.818-1.084.508l-3-2.21-1.447 1.394c-.16.16-.295.295-.605.295l.213-3.053 5.56-5.023c.242-.213-.054-.333-.373-.12L6.514 14.51l-2.948-.924c-.64-.203-.654-.64.136-.95l11.52-4.44c.534-.194 1.001.13.34.052z" />
            </svg>
            Telegram
          </a>
        </div>
      </section>

      {/* Showcase */}
      <section className={styles.showcase}>
        <div className={styles.showcaseFrame}>
          <div className={styles.showcasePlaceholder}>
            <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1" strokeLinecap="round" strokeLinejoin="round" opacity={0.3}>
              <polygon points="5 3 19 12 5 21 5 3" />
            </svg>
          </div>
        </div>
      </section>

      {/* Contributing */}
      <div className={styles.contribCard}>
        <div className={styles.contribLeft}>
          <svg className={styles.contribIcon} viewBox="0 0 24 24" fill="currentColor" stroke="none">
            <path d="M12 2C6.477 2 2 6.484 2 12.017c0 4.425 2.865 8.18 6.839 9.504.5.092.682-.217.682-.483 0-.237-.008-.868-.013-1.703-2.782.605-3.369-1.343-3.369-1.343-.454-1.158-1.11-1.466-1.11-1.466-.908-.62.069-.608.069-.608 1.003.07 1.531 1.032 1.531 1.032.892 1.53 2.341 1.088 2.91.832.092-.647.35-1.088.636-1.338-2.22-.253-4.555-1.113-4.555-4.951 0-1.093.39-1.988 1.029-2.688-.103-.253-.446-1.272.098-2.65 0 0 .84-.27 2.75 1.026A9.564 9.564 0 0112 6.844c.85.004 1.705.115 2.504.337 1.909-1.296 2.747-1.027 2.747-1.027.546 1.379.202 2.398.1 2.651.64.7 1.028 1.595 1.028 2.688 0 3.848-2.339 4.695-4.566 4.943.359.309.678.92.678 1.855 0 1.338-.012 2.419-.012 2.747 0 .268.18.58.688.482A10.019 10.019 0 0022 12.017C22 6.484 17.522 2 12 2z" />
          </svg>
          <div>
            <h2 className={styles.contribTitle}>{c.contribTitle}</h2>
            <p className={styles.contribDesc}>{c.contribDesc}</p>
          </div>
        </div>
        <div className={styles.contribActions}>
          <a className={`${styles.contribBtn} ${styles.contribBtnPrimary}`} href="https://github.com/qqqqqf-q/Arkloop" target="_blank" rel="noopener">
            {c.githubLabel}
          </a>
          <Link className={`${styles.contribBtn} ${styles.contribBtnSecondary}`} href={c.contribGuideHref}>
            {c.contribGuideLabel}
          </Link>
        </div>
      </div>
    </main>
  );
}
