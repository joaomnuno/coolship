// Runs once when the server starts. The Node.js-only work lives in lib/vary.ts
// so that the Edge bundle of this file never sees `node:http`; see that file
// for why the Next.js build needs it.
export async function register() {
  if (process.env.NEXT_RUNTIME === 'nodejs') {
    const { keepAcceptInVary } = await import('./lib/vary');
    keepAcceptInVary();
  }
}
