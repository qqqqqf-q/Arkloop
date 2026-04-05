'use client';

import Link from 'next/link';
import { usePathname } from 'next/navigation';
import { useTheme } from 'next-themes';
import { useEffect, useState } from 'react';

type Lang = 'zh' | 'en';

const NAV: Record<Lang, { label: string; href: string }[]> = {
  zh: [
    { label: '文档', href: '/zh/docs/guide' },
    { label: 'API', href: '/zh/api' },
  ],
  en: [
    { label: 'Docs', href: '/en/docs/guide' },
    { label: 'API', href: '/en/api' },
  ],
};

export default function Topbar({ lang }: { lang: Lang }) {
  const pathname = usePathname();
  const otherLang = lang === 'zh' ? 'en' : 'zh';
  const switchPath = pathname.replace(new RegExp(`^/${lang}`), `/${otherLang}`);
  const { resolvedTheme, setTheme } = useTheme();
  const [mounted, setMounted] = useState(false);
  useEffect(() => setMounted(true), []);

  return (
    <header className="topbar">
      <div className="topbar-inner">
        <Link href={`/${lang}`} className="brand">
          Arkloop Developers
        </Link>
        <nav className="topnav">
          {NAV[lang].map((item) => (
            <Link
              key={item.href}
              href={item.href}
              className={`nav-link${pathname.startsWith(item.href.replace('/guide', '')) ? ' nav-link-active' : ''}`}
            >
              {item.label}
            </Link>
          ))}
        </nav>
        <div className="topbar-actions">
          <Link href={switchPath} className="lang-switch">
            {lang === 'zh' ? 'EN' : '中文'}
          </Link>
          <button
            className="theme-toggle"
            onClick={() => setTheme(resolvedTheme === 'dark' ? 'light' : 'dark')}
            aria-label="Toggle theme"
            suppressHydrationWarning
          >
            {mounted && resolvedTheme === 'light' ? (
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                <circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.93 4.93l1.41 1.41M17.66 17.66l1.41 1.41M2 12h2M20 12h2M6.34 17.66l-1.41 1.41M19.07 4.93l-1.41 1.41"/>
              </svg>
            ) : (
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                <path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"/>
              </svg>
            )}
          </button>
        </div>
      </div>
    </header>
  );
}
