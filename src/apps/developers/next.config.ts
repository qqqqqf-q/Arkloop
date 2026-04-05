import type { NextConfig } from 'next';
import { createMDX } from 'fumadocs-mdx/next';

const config: NextConfig = {};

const withMDX = createMDX();
export default withMDX(config);
