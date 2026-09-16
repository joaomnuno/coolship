import { repo } from '@/lib/site';

// `curl -fsSL https://coolship.itrocas.com/install.sh | sh` follows this
// redirect to the script on the default branch, so the site never serves a
// copy that could fall behind scripts/install.sh.
export const revalidate = false;

export function GET() {
  return Response.redirect(
    `https://raw.githubusercontent.com/${repo.owner}/${repo.name}/${repo.branch}/scripts/install.sh`,
    302,
  );
}
