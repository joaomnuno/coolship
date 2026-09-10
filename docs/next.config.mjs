import { createMDX } from 'fumadocs-mdx/next';

const withMDX = createMDX();

/** @type {import('next').NextConfig} */
const config = {
  // The site runs as a Node.js server in a container. `standalone` makes
  // `next build` write a self-contained server to .next/standalone, which
  // Dockerfile.next copies; `vinext build` (the default toolchain, see
  // vite.config.ts) writes the equivalent to dist/standalone.
  output: 'standalone',
  reactStrictMode: true,
};

export default withMDX(config);
