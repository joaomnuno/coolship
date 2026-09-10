# Coolship documentation site

The site at [coolship.itrocas.com](https://coolship.itrocas.com) is built with [Fumadocs](https://fumadocs.dev) on Next.js, written in MDX, and served by a Node.js server in a Docker container on Coolify. Search runs on the server (Orama, `/api/search`), and every page is also served as Markdown for assistants, with an `llms.txt` index.

## Toolchain

The app is a standard Next.js App Router project, built and served by [vinext](https://github.com/cloudflare/vinext) (a Vite-based implementation of Next.js) rather than `next build`. The Next.js toolchain stays installed and works unchanged, so there are two ways to build the same site:

| | vinext (default) | Next.js (fallback) |
| --- | --- | --- |
| build | `npm run build` → `dist/standalone/` | `npm run build:next` → `.next/standalone/` |
| serve | `npm start` (binds `HOST`, `PORT`) | `npm run start:next` (binds `HOSTNAME`, `PORT`) |
| dev server | `npm run dev` | `npm run dev:next` |
| image | `Dockerfile` | `Dockerfile.next` |

`vite.config.ts` is the one vinext-specific file. Its `plugins: [fumadocsMdx(), vinext()]` line is required: vinext reads `next.config.mjs` but does not run the webpack loaders that `fumadocs-mdx/next` installs there, so Fumadocs' Vite plugin must expand the `defineDocs` macro in `lib/source.ts`. Without it the build passes and every MDX-backed route answers 500 at runtime, which is why the CI smoke test curls the built container.

**Switching to the fallback** is a one-line change in Coolify: set the application's *Dockerfile Location* to `/Dockerfile.next` instead of `/Dockerfile` (Coolify joins it to the base directory, `/docs`) and redeploy. Nothing in the app changes; both images listen on port 3000, run as a non-root user, and carry the same `HEALTHCHECK`. Switch back the same way. The known differences: vinext renders docs pages per request (Next.js pre-renders them at build time, and serves them with `s-maxage=31536000`), and vinext is a beta, so a regression after a dependency update is the reason to switch. `instrumentation.ts` exists for the Next.js build alone: its compiled page runtime overwrites the `Vary` header while sending a page, which would drop the `Vary: Accept` that `proxy.ts` sets on a page URL; the hook keeps the token, and does nothing under vinext, which merges the header itself.

## Run it

Node 24 (see `.nvmrc`) and npm.

```bash
cd docs
npm ci
npm run dev        # http://127.0.0.1:3000
```

`npm run check` type-checks. `npm run build && npm start` runs the production server the way the container does, and `npm run smoke` (or `scripts/smoke.sh http://127.0.0.1:3000`) checks every route the site depends on against it: the home page, a docs page, search, `llms.txt`, `llms-full.txt`, a page as Markdown (`/docs/commands/deploy.md` and `Accept: text/markdown`, with `Vary: Accept` on both representations of the page URL), the Markdown of pages built from steps and cards (`/docs/get-started.md`, `/docs/index.md`, and `llms-full.txt`, where each block must stay on its own line), `robots.txt`, and a 404.

`SITE_URL` (default `https://coolship.itrocas.com`) is the public origin used for the absolute URLs in `llms.txt`, the per-page Markdown, canonical links, and the assistant prompts. Set it at build time and at runtime when the site is served elsewhere; both Dockerfiles take it as a build argument.

## Containers

```bash
docker build -t coolship-docs docs                          # vinext
docker build -t coolship-docs-next -f docs/Dockerfile.next docs   # Next.js fallback
docker run --rm -p 3000:3000 coolship-docs
docs/scripts/smoke.sh http://127.0.0.1:3000
```

Deploy on Coolify as a Dockerfile application with *Base Directory* `/docs`, *Dockerfile Location* `/Dockerfile`, port 3000, and coolship.itrocas.com as its domain: the repository README and `llms.txt` link there, and Coolify refuses a domain that another application still holds, so move it to this one first. `.github/workflows/docs-site.yml` builds both images on pushes to `main` and pull requests that touch `docs/`, runs each one, and runs the smoke test against it.

## Write docs

Pages live in `content/docs/` as MDX files with `title` and `description` frontmatter; both are required, and the description doubles as the summary in `llms.txt`. Each folder's `meta.json` names its title and the order of its pages. Slugs follow file paths: `content/docs/commands/deploy.mdx` is `/docs/commands/deploy`.

Link to other pages with root-relative paths (`[deploy](/docs/commands/deploy)`). Besides Markdown, these components are available without imports: `Callout`, `Cards` and `Card`, `Steps` and `Step`, `Tabs` and `Tab`. They are rendered into plain Markdown for the `.md` pages and `llms-full.txt` by `lib/llm.ts`, and `lib/source.ts` lists them so that their children reach it as Markdown blocks; anything new you add to `components/mdx.tsx` needs an entry in both.

Content is derived from the repository's `README.md`, `ARCHITECTURE.md`, `CHANGELOG.md`, and each command's `--help` (`scripts/build`, then `bin/coolship <command> --help`). Do not document behavior the CLI does not have.

## Layout

| Path | Purpose |
| --- | --- |
| `content/docs/` | The pages, in sidebar order via `meta.json` files |
| `app/(home)/page.tsx` | The home page |
| `app/docs/[[...slug]]/page.tsx` | Renders a page with its table of contents and the page actions |
| `app/api/search/route.ts` | Orama search on the server |
| `app/llms.txt`, `app/llms-full.txt` | The index and the full text for assistants |
| `app/llms.mdx/docs/[[...slug]]` | One page as Markdown |
| `proxy.ts` | Rewrites `/docs/<page>.md` and `Accept: text/markdown` requests to that route, and sets `Vary: Accept` on both representations of a page URL |
| `instrumentation.ts` | Keeps `Accept` in `Vary` on the Next.js build, whose page runtime overwrites the header |
| `components/page-actions.tsx` | Copy as Markdown, View as Markdown, Open in Claude/ChatGPT, Edit |
| `lib/source.ts` | The content source, the components kept for `lib/llm.ts`, and the Markdown URL helpers |
| `lib/llm.ts` | Markdown renditions of pages and the `llms.txt` index |
| `site.config.mjs` | Public URL (`SITE_URL`) and repository |
| `vite.config.ts` | The vinext build, with the required `fumadocsMdx()` plugin |
| `Dockerfile`, `Dockerfile.next` | The vinext image and the Next.js fallback image |
| `scripts/smoke.sh` | Route checks against a running server, used by CI |
