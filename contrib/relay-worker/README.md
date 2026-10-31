# Kumiho control-plane relay

This directory contains a self-contained Cloudflare Worker for the optional
Kumiho control-plane relay. It forwards only the five Mozilla hosts Kumiho
uses for Firefox Accounts, Guardian and the VPN server list.

A relay is only useful when this region returns HTTP 451 for Mozilla's
control plane. It cannot fix a 451 from the VPN edge itself: the tunnel data
path still originates from your network, so an edge refusal remains terminal.

## Deploy

From this directory:

```sh
npx wrangler login
npx wrangler deploy
npx wrangler secret put RELAY_TOKEN
```

`wrangler deploy` prints the Worker URL (normally
`https://kumiho-relay.<account>.workers.dev`). The `wrangler.toml` route and
custom domain are intentionally left to the operator; add a route in the
Cloudflare dashboard or in `wrangler.toml` if you want a stable custom host.

The `RELAY_TOKEN` secret is optional. When it is set, every request must carry
the same value in `X-Kumiho-Relay-Token`. Use a long random value and never
commit it. Set the same value in Kumiho with the write-only `relay_token`
setting; the token is stored in `relay.token` beside `tokens.json` (mode 0600)
and is never shown by the GUI, TUI or CLI.

## Configure Kumiho

Set the Worker origin as the relay URL, for example:

```sh
kumiho settings set relay_url https://kumiho-relay.<account>.workers.dev
kumiho settings set relay_token
```

The second command prompts for the token without putting it in shell history.
The GUI and TUI expose the same two fields under Settings → Region and include
a Test relay action.

## Test

The relay test fetches the head of the allowlisted server-list collection
through the Worker and reports status and latency. A direct curl is also
useful while deploying:

```sh
curl -i \
  -H "X-Kumiho-Relay-Token: $RELAY_TOKEN" \
  "https://kumiho-relay.<account>.workers.dev/firefox.settings.services.mozilla.com/v1/buckets/main/collections/vpn-serverlist/records?limit=1"
```

For local development:

```sh
npx wrangler dev --var RELAY_TOKEN:dev-token
curl -i \
  -H "X-Kumiho-Relay-Token: dev-token" \
  "http://127.0.0.1:8787/firefox.settings.services.mozilla.com/v1/buckets/main/collections/vpn-serverlist/records?limit=1"
```

## Request shape and allowlist

The first path segment is the target host; the rest is the target path, with
the query preserved:

```text
/<target-host>/<target-path>?<query>
```

Only these hosts are accepted:

- `api.accounts.firefox.com`
- `oauth.accounts.firefox.com`
- `profile.accounts.firefox.com`
- `vpn.mozilla.org`
- `firefox.settings.services.mozilla.com`

The Worker rejects other hosts, ports, userinfo, empty hosts and path
traversal. It forwards the method, headers (minus `X-Kumiho-Relay-Token` and
`Host`), body and query, with `redirect: "manual"`; upstream status, headers
and body are returned unchanged. It does not cache or follow redirects.

The browser sign-in page at `accounts.firefox.com` is deliberately **not**
proxied. Kumiho opens it directly, and the Worker only handles the control
plane after the browser hands back the redirect URL.

## Trust boundary

This Worker is deployed and operated by you. It sees the relay token and the
control-plane request headers and bodies that pass through it, including FxA
authorization headers; that is why the allowlist is fixed and why the Worker
does not log requests, tokens or target hosts. Cloudflare still processes the
traffic under your account and its own policies. Deploy it in a region and
account you trust, and rotate the token if it is exposed.

The template has no npm dependencies and no lockfile. It is not part of the Go
build or CI.
