import type { NextConfig } from 'next';
import { createMDX } from 'fumadocs-mdx/next';

// 线上文档站挂在 GitHub Pages 子路径 /Arkloop；本地 dev 用根路径，否则 localhost:3000/* 会全部 404
const config: NextConfig = {
  output: 'export',
  distDir: 'dist',
  basePath: process.env.NODE_ENV === 'development' ? '' : '/Arkloop',
};

const withMDX = createMDX();
export default withMDX(config);
