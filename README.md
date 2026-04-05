# Caddy CNAME Sync

A Caddy app module that automatically reconciles DNS CNAME records for every HTTP route in a configured zone, all pointing at a single target host.
Reconciliation runs on every Caddy config reload, records are created for new routes and deleted when routes are removed.

DNS Providers are modular. You'll need to plug in [a DNS provider module from caddy-dns](https://github.com/caddy-dns) so that your DNS records can be updated.

> [!NOTE]
> This is not an official repository of the [Caddy Web Server](https://github.com/caddyserver) organization.

Heavily inspired by [mholt/caddy-dynamicdns](https://github.com/mholt/caddy-dynamicdns).

## Why

This module was built to solve a personal requirement. I run a split DNS setup: an internal server for my clients, with a wildcard CNAME on the public side that never needs to change. The internal zone is the only moving part: every new service added to Caddy needs a matching internal DNS record so local clients resolve directly to the VPN IP rather than routing out through the internet.

Split DNS works well once it's set up, but keeping the internal zone in sync with the Caddyfile is the kind of small manual step that's easy to forget. Having Caddy own that responsibility directly means the internal DNS state is always a reflection of what's actually running.

## How ownership works

The module identifies the records it owns by a simple invariant: **any existing CNAME in the zone whose target matches the configured `target` is considered owned**. This means:

- You can safely create non-matching CNAMEs manually, they'll be left alone.
- If you change `target`, the old CNAMEs (pointing at the old target) will be orphaned rather than deleted. Delete them manually or do a one-time cleanup.

## Build

```bash
xcaddy build \
    --with github.com/luishfonseca/caddy-cname-sync \
    --with github.com/caddy-dns/cloudflare   # or whichever provider you use
```

## Caddyfile

| Field        | Default             | Description |
| ------------ | ------------------- | -- |
| zone         | required            | DNS zone to manage |
| dns_provider | required            | DNS provider module (see [caddy-dns](https://github.com/caddy-dns)) |
| target       | `<hostname>.<zone>` | CNAME target all managed records point to |
| strict       | true                | Only create CNAME records for configured zone |
| ttl          | 1h                  | TTL applied to newly created records |

### Example

```caddyfile
{
    cname_sync {
        zone     example.com
        target   foo.example.com
        ttl      5m
        provider cloudflare {
            api_token {env.CF_API_TOKEN}
        }
    }
}

web.example.com {
    reverse_proxy localhost:8080
}

api.example.com {
    reverse_proxy localhost:9090
}
```

After `caddy reload`, the following records will exist in your zone:

```
web.example.com.  300  IN  CNAME  foo.example.com.
api.example.com.  300  IN  CNAME  foo.example.com.
```

Remove `api.example.com` from the Caddyfile and reload again, its CNAME is deleted.
