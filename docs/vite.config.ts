import { fumadocsMdx } from 'fumadocs-mdx/vite';
import { defineConfig } from 'vite';
import vinext from 'vinext';

// vinext builds and serves this Next.js app with Vite. It reads
// next.config.mjs, but it does not run the webpack/turbopack loaders that
// fumadocs-mdx/next installs there, so Fumadocs' own Vite plugin must expand
// the `defineDocs` macro in lib/source.ts. Without `fumadocsMdx()` the build
// still passes and every MDX-backed route answers 500 at runtime.
export default defineConfig({
  // Copied from vinext's fumadocs example: keep the Fumadocs packages out of
  // dependency pre-bundling in dev so React contexts are not duplicated.
  optimizeDeps: {
    exclude: ['fumadocs-ui', 'fumadocs-core'],
    include: [
      'fumadocs-ui > debug',
      'fumadocs-core > extend',
      'fumadocs-mdx > extend',
      'fumadocs-core > style-to-js',
      'fumadocs-mdx > style-to-js',
      '@unpic/react',
    ],
  },
  plugins: [fumadocsMdx(), vinext()],
});
