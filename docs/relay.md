# Control-plane relay

Kumiho's optional relay forwards only the Mozilla **control-plane** requests
that a blocked region answers with HTTP 451. It is a small, user-operated
Cloudflare Worker template in [`contrib/relay-worker/`](../contrib/relay-worker/).
It is not a VPN and it does not carry tunnel traffic.

## What it can and cannot fix

A relay can fix a 451 from:

- `api.accounts.firefox.com`
- `oauth.accounts.firefox.com`
- `profile.accounts.firefox.com`
- `vpn.mozilla.org` control-plane requests
- `firefox.settings.services.mozilla.com` (Remote Settings / the server list)

It cannot fix a 451 from the **VPN edge**. The tunnel still originates from
your network, so an edge refusal remains terminal:

> The VPN edge refused this region (HTTP 451). A control-plane relay cannot
> fix the VPN data path; connect from a supported region.

The browser sign-in page at `accounts.firefox.com` is also deliberately not
proxied. Kumiho opens it directly; the relay handles only the engine's HTTP
requests after the browser hands back the redirect URL.

## Relay vs. another VPN

The relay is the guide's **Option 2**. The recommended first run is **Option
1**: turn on another VPN, sign in, run `kumiho prepare` (or choose **Prepare**
in any client), turn the other VPN off, and connect. Prepare stores a
ten-minute pass, and after connect Kumiho renews its own control-plane
requests through the tunnel; the relay is then needed only when a request
still answers 451.

Because the relay does not proxy the browser page, a blocked region cannot
complete browser sign-in through the relay alone. Password sign-in uses the
relay for its API calls and works; browser sign-in still needs another VPN or
another device. The full first-run steps are in
[usage.md](usage.md#blocked-regions-and-the-first-run) and
[troubleshooting.md](troubleshooting.md#mozilla-answers-http-451).

## Deploy the Worker

The template has no npm dependencies. From `contrib/relay-worker/`:

```sh
npx wrangler login
npx wrangler deploy
npx wrangler secret put RELAY_TOKEN
```

`wrangler deploy` prints the Worker origin, usually
`https://kumiho-relay.<account>.workers.dev`. A route or custom domain is up to
the operator; see [`contrib/relay-worker/README.md`](../contrib/relay-worker/README.md)
for the `wrangler.toml` comment and local `wrangler dev` instructions.

`RELAY_TOKEN` is optional. When it is set, every request must carry it in the
`X-Kumiho-Relay-Token` header. Use a long random value. The Worker compares it
before forwarding, strips the header, and never logs requests, the token or
target hosts.

## Configure Kumiho

Set the Worker **origin** as the relay URL. It must be HTTPS with no path,
query, fragment or userinfo; a trailing slash is normalized away:

```sh
kumiho settings set relay_url https://kumiho-relay.<account>.workers.dev
kumiho settings set relay_token    # prompts without echo, or pass a value
```

`relay_token` is write-only. The engine stores it as `relay.token` beside
`tokens.json` with mode 0600; it never appears in `config.toml`, `state.json`,
logs, `settings get`, the GUI, the TUI or Android. Use `kumiho settings reset
relay_token` to delete it. `kumiho settings get relay_token_set` reports only
whether a token is stored.

The source-compatible `kumiho settings get relay_token` is an alias for
`relay_token_set`.

Every client has the same fields under **Settings → Region**:

- Allow locked locations (`allow_locked`)
- Control-plane relay (`relay_url`)
- Relay token (write-only, with Clear)
- Test relay

The help text in every client says the relay is used only after HTTP 451 and
does not proxy the browser sign-in page.

## Test it

From a shell with the daemon running:

```sh
kumiho relay test
```

The test makes one direct request through the relay to the head of the
allowlisted server-list collection and reports status and latency. It does not
require a live 451 and never touches the data path. The same action is **Test
relay** in the GUI, TUI and Android Region settings.

Expected results:

| Output | Meaning |
|---|---|
| `Relay OK — N ms` | Worker, token and upstream reachable |
| `Relay rejected the token` | Worker returned 401/403; compare `RELAY_TOKEN` |
| `relay_url is not set` | no relay configured |
| `Relay unreachable: <detail>` | DNS, TLS, Worker or upstream failure |

A direct `curl` is useful while deploying:

```sh
curl -i \
  -H "X-Kumiho-Relay-Token: $RELAY_TOKEN" \
  "https://kumiho-relay.<account>.workers.dev/firefox.settings.services.mozilla.com/v1/buckets/main/collections/vpn-serverlist/records?limit=1"
```

For local development, `wrangler dev --var RELAY_TOKEN:dev-token` serves
`http://127.0.0.1:8787`; set that URL in Kumiho only for the test.

## Request shape and safety

The first path segment is the target host; the rest is the target path, with
the query preserved:

```text
/<target-host>/<target-path>?<query>
```

Only the five hosts listed above are accepted. The Worker rejects other hosts,
ports, userinfo, empty hosts and path traversal, and allows `GET`, `POST`,
`PUT`, `PATCH`, `DELETE` and `HEAD`. It forwards the method, headers (minus
`X-Kumiho-Relay-Token` and `Host`), body and query, uses `redirect: "manual"`,
and returns upstream status, headers and body unchanged. It does not cache.

The engine applies the same allowlist client-side as defense in depth. It uses
the relay only after a 451, retries exactly once, never retries a request
already addressed to the relay host, and treats a relay 451 or transport
failure as terminal with the configured-relay error. Normal requests stay
direct and are byte-for-byte unchanged.

## Trust boundary

You deploy and operate the Worker. It sees the relay token and the
control-plane headers and bodies that pass through it, including Firefox
Accounts authorization headers. That is why the allowlist is fixed and why the
template does not log. Cloudflare still processes the traffic under your
account and its policies. Deploy it in an account and region you trust, use a
dedicated Worker, and rotate the token if it is exposed.

## Limitations

- One relay attempt only; there is no fallback chain, cache or retry loop.
- The relay cannot move the VPN data path or the browser sign-in page.
- A Worker failure does not fall back to an older server list; the control
  request fails with the message above.
- `relay.token` is local engine state, not part of `config.toml` or a synced
  account setting. Re-enter it on each device.
