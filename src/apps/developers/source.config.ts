import { defineDocs, defineConfig } from 'fumadocs-mdx/config';

export const docsZh = defineDocs({ dir: 'content/docs/zh' });
export const docsEn = defineDocs({ dir: 'content/docs/en' });
export const apiZh = defineDocs({ dir: 'content/api/zh' });
export const apiEn = defineDocs({ dir: 'content/api/en' });

export default defineConfig();
