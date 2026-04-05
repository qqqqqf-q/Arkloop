import { DocsLayout } from 'fumadocs-ui/layouts/docs';
import { docsZhSource, docsEnSource } from '@/lib/source';

export default async function DocsLayoutRoute({
  children,
  params,
}: {
  children: React.ReactNode;
  params: Promise<{ lang: string }>;
}) {
  const { lang } = await params;
  const source = lang === 'zh' ? docsZhSource : docsEnSource;

  return (
    <DocsLayout
      tree={source.pageTree}
      nav={{ enabled: false }}
      sidebar={{ collapsible: false }}
    >
      {children}
    </DocsLayout>
  );
}
