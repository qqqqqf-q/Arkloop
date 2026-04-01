import { defineConfig } from 'astro/config';
import mdx from '@astrojs/mdx';
import tailwindcss from '@tailwindcss/vite';

export default defineConfig({
  site: 'https://arkloop.cn',
  integrations: [mdx({ gfm: true })],
  redirects: {
    '/docs': '/docs/guide',
    '/en/docs': '/en/docs/guide',
  },
  markdown: {
    gfm: true,
    syntaxHighlight: 'shiki',
    shikiConfig: {
      themes: {
        light: 'one-light',
        dark: 'github-dark',
      },
    },
  },
  vite: {
    plugins: [tailwindcss()],
  },
});
