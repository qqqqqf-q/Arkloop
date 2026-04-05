import { notFound } from 'next/navigation';
import { DocsPage, DocsBody, DocsTitle, DocsDescription } from 'fumadocs-ui/page';
import defaultMdxComponents from 'fumadocs-ui/mdx';
import { apiZhSource, apiEnSource } from '@/lib/source';

export default async function ApiPageRoute({
  params,
}: {
  params: Promise<{ lang: string; slug?: string[] }>;
}) {
  const { lang, slug } = await params;
  const source = lang === 'zh' ? apiZhSource : apiEnSource;
  const page = source.getPage(slug);
  if (!page) notFound();

  const MDX = page.data.body;

  return (
    <DocsPage toc={page.data.toc}>
      <DocsTitle>{page.data.title}</DocsTitle>
      <DocsDescription>{page.data.description}</DocsDescription>
      <DocsBody>
        <MDX components={defaultMdxComponents} />
      </DocsBody>
    </DocsPage>
  );
}

export async function generateStaticParams() {
  return [
    ...apiZhSource.generateParams().map((p) => ({ lang: 'zh', slug: p.slug })),
    ...apiEnSource.generateParams().map((p) => ({ lang: 'en', slug: p.slug })),
  ];
}
