import { createSearchAPI } from 'fumadocs-core/search/server';
import { docsZhSource, docsEnSource, apiZhSource, apiEnSource } from '@/lib/source';

export const { GET } = createSearchAPI('advanced', {
  indexes: [
    ...docsZhSource.getPages().map((page) => ({
      title: page.data.title ?? '',
      description: page.data.description ?? '',
      url: `/zh/docs/${page.slugs.join('/')}`,
      id: `zh-docs-${page.slugs.join('-')}`,
      structuredData: page.data.structuredData,
      tag: 'zh',
    })),
    ...docsEnSource.getPages().map((page) => ({
      title: page.data.title ?? '',
      description: page.data.description ?? '',
      url: `/en/docs/${page.slugs.join('/')}`,
      id: `en-docs-${page.slugs.join('-')}`,
      structuredData: page.data.structuredData,
      tag: 'en',
    })),
    ...apiZhSource.getPages().map((page) => ({
      title: page.data.title ?? '',
      description: page.data.description ?? '',
      url: `/zh/api/${page.slugs.join('/')}`,
      id: `zh-api-${page.slugs.join('-')}`,
      structuredData: page.data.structuredData,
      tag: 'zh',
    })),
    ...apiEnSource.getPages().map((page) => ({
      title: page.data.title ?? '',
      description: page.data.description ?? '',
      url: `/en/api/${page.slugs.join('/')}`,
      id: `en-api-${page.slugs.join('-')}`,
      structuredData: page.data.structuredData,
      tag: 'en',
    })),
  ],
});
