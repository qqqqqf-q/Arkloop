import type { NextConfig } from 'next';
import { createMDX } from 'fumadocs-mdx/next';

const config: NextConfig = {
  output: 'export',
  distDir: 'dist',
  basePath: '/Arkloop',
};

const withMDX = createMDX();
export default withMDX(config);
