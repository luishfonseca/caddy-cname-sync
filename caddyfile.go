package caddycnamesync

import (
	"strconv"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
)

func init() {
	httpcaddyfile.RegisterGlobalOption("cname_sync", parseApp)
}

func parseApp(d *caddyfile.Dispenser, _ any) (any, error) {
	app := new(App)

	// consume the option name
	if !d.Next() {
		return nil, d.ArgErr()
	}

	for d.NextBlock(0) {
		switch d.Val() {

		case "zone":
			if !d.NextArg() {
				return nil, d.ArgErr()
			}
			app.Zone = d.Val()

		case "target":
			if !d.NextArg() {
				return nil, d.ArgErr()
			}
			app.Target = d.Val()

		case "ttl":
			if !d.NextArg() {
				return nil, d.ArgErr()
			}
			dur, err := time.ParseDuration(d.Val())
			if err != nil {
				return nil, d.Errf("invalid TTL %q: %v", d.Val(), err)
			}
			app.TTL = caddy.Duration(dur)

		case "strict":
			if !d.NextArg() {
				return nil, d.ArgErr()
			}
			b, err := strconv.ParseBool(d.Val())
			if err != nil {
				return nil, d.Errf("invalid strict value %q: %v", d.Val(), err)
			}
			app.Strict = &b

		case "provider":
			if !d.NextArg() {
				return nil, d.ArgErr()
			}
			provName := d.Val()
			modID := "dns.providers." + provName
			unm, err := caddyfile.UnmarshalModule(d, modID)
			if err != nil {
				return nil, err
			}
			app.DNSProviderRaw = caddyconfig.JSONModuleObject(unm, "name", provName, nil)

		default:
			return nil, d.Errf("unknown subdirective %q", d.Val())
		}
	}

	return httpcaddyfile.App{
		Name:  "cname_sync",
		Value: caddyconfig.JSON(app, nil),
	}, nil
}
