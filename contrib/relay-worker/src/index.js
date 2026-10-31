// Kumiho control-plane relay for Cloudflare Workers.
//
// The Worker is deliberately small and stateless: it forwards only the fixed
// Mozilla control-plane allowlist, never logs requests, and never follows
// redirects. See ../README.md for deployment and the trust boundary.

const ALLOWED_HOSTS = new Set([
  "api.accounts.firefox.com",
  "oauth.accounts.firefox.com",
  "profile.accounts.firefox.com",
  "vpn.mozilla.org",
  "firefox.settings.services.mozilla.com",
]);

const ALLOWED_METHODS = new Set(["GET", "POST", "PUT", "PATCH", "DELETE", "HEAD"]);
const RELAY_TOKEN_HEADER = "X-Kumiho-Relay-Token";

function badRequest(message) {
  return new Response(message, { status: 400 });
}

function hasTraversal(path) {
  let decoded;
  try {
    decoded = decodeURIComponent(path);
  } catch {
    return true;
  }
  return decoded.split("/").some((segment) => segment === "." || segment === ".." || segment.includes("\\"));
}

export default {
  async fetch(request, env) {
    if (!ALLOWED_METHODS.has(request.method)) {
      return new Response("method not allowed", {
        status: 405,
        headers: { Allow: [...ALLOWED_METHODS].join(", ") },
      });
    }

    const incoming = new URL(request.url);
    const pathname = incoming.pathname;
    if (!pathname.startsWith("/")) {
      return badRequest("invalid relay path");
    }

    const withoutLeadingSlash = pathname.slice(1);
    const slash = withoutLeadingSlash.indexOf("/");
    const rawHost = slash === -1 ? withoutLeadingSlash : withoutLeadingSlash.slice(0, slash);
    const targetPath = slash === -1 ? "/" : withoutLeadingSlash.slice(slash);
    const host = rawHost.toLowerCase();

    if (
      host === "" ||
      host.includes("@") ||
      host.includes(":") ||
      host === "." ||
      host === ".." ||
      !ALLOWED_HOSTS.has(host)
    ) {
      return badRequest("target host is not allowed");
    }
    if (!targetPath.startsWith("/") || hasTraversal(targetPath)) {
      return badRequest("invalid target path");
    }
    if (hasTraversal(incoming.search)) {
      return badRequest("invalid target query");
    }

    const requiredToken = env.RELAY_TOKEN;
    if (requiredToken) {
      if (request.headers.get(RELAY_TOKEN_HEADER) !== requiredToken) {
        return new Response("unauthorized", { status: 401 });
      }
    }

    const headers = new Headers(request.headers);
    headers.delete(RELAY_TOKEN_HEADER);
    headers.delete("host");

    const target = `https://${host}${targetPath}${incoming.search}`;
    let upstream;
    try {
      upstream = await fetch(target, {
        method: request.method,
        headers,
        body: request.method === "GET" || request.method === "HEAD" ? undefined : request.body,
        redirect: "manual",
      });
    } catch {
      return new Response("relay upstream error", { status: 502 });
    }

    return new Response(upstream.body, {
      status: upstream.status,
      statusText: upstream.statusText,
      headers: upstream.headers,
    });
  },
};
