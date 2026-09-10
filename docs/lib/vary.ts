import { ServerResponse } from 'node:http';

// Keeps `Accept` in the Vary header of every response that already carries it.
//
// proxy.ts sets `Vary: Accept` on a docs page URL because the same URL answers
// with HTML or Markdown depending on the request's Accept header. The Next.js
// server applies that header and appends its own Vary tokens, but the
// compiled app-page runtime (next 16.3.4, app-page*.runtime.prod.js) then
// calls `res.setHeader('vary', 'rsc, next-router-state-tree, …')` with a plain
// string while sending a page, discarding what was there. Prerendered pages
// are served with `Cache-Control: s-maxage=31536000`, so a shared cache would
// hand the HTML to a Markdown client. Route handlers are sent through a path
// that appends, which is why the Markdown responses keep the header without
// this. vinext merges the header itself, so this is a no-op there.
function hasAccept(value: number | string | readonly string[] | undefined): boolean {
  if (value === undefined) return false;
  const tokens = Array.isArray(value) ? value.flatMap((item) => String(item).split(',')) : String(value).split(',');
  return tokens.some((token) => token.trim().toLowerCase() === 'accept');
}

export function keepAcceptInVary() {
  const setHeader = ServerResponse.prototype.setHeader;
  ServerResponse.prototype.setHeader = function (this: ServerResponse, name, value) {
    if (typeof name === 'string' && name.toLowerCase() === 'vary' && !hasAccept(value) && hasAccept(this.getHeader('vary'))) {
      value = Array.isArray(value) ? ['Accept', ...value] : `Accept, ${String(value)}`;
    }
    return setHeader.call(this, name, value);
  } as typeof setHeader;
}
