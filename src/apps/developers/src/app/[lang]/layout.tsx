import { redirect } from 'next/navigation';
import { RootProvider } from 'fumadocs-ui/provider';
import Topbar from '@/components/Topbar';

const validLangs = ['zh', 'en'] as const;
type Lang = (typeof validLangs)[number];

export function generateStaticParams() {
  return validLangs.map((lang) => ({ lang }));
}

export default async function LangLayout({
  children,
  params,
}: {
  children: React.ReactNode;
  params: Promise<{ lang: string }>;
}) {
  const { lang } = await params;
  if (!(validLangs as readonly string[]).includes(lang)) {
    redirect('/zh');
  }

  return (
    <RootProvider
      theme={{
        attribute: 'class',
        defaultTheme: 'dark',
        storageKey: 'arkloop:developers:theme',
      }}
    >
      <Topbar lang={lang as Lang} />
      {children}
    </RootProvider>
  );
}
