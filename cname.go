package caddycnamesync

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/libdns/libdns"
	"go.uber.org/zap"
)

func init() {
	caddy.RegisterModule(App{})
}

// App reconciles DNS CNAME records for HTTP routes in a given zone.
type App struct {
	// DNS zone to manage
	Zone string `json:"zone,omitempty"`

	// CNAME target that every managed record points to. Defaults to <hostname>.<zone>
	Target string `json:"target,omitempty"`

	// TTL applied to newly created records.
	TTL caddy.Duration `json:"ttl,omitempty"`

	// Only create records for the configured zone. Defaults to true
	Strict *bool `json:"strict,omitempty"`

	// DNS provider module
	DNSProviderRaw json.RawMessage `json:"dns_provider,omitempty" caddy:"namespace=dns.providers inline_key=name"`

	provider dnsProvider
	ctx      caddy.Context
	logger   *zap.Logger
}

// dnsProvider is the subset of libdns interfaces needed for full reconciliation.
type dnsProvider interface {
	libdns.RecordGetter
	libdns.RecordSetter
	libdns.RecordDeleter
}

func (App) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "cname_sync",
		New: func() caddy.Module { return new(App) },
	}
}

func (a *App) Provision(ctx caddy.Context) error {
	a.ctx = ctx
	a.logger = ctx.Logger(a)

	if a.Zone == "" {
		return fmt.Errorf("a zone is required")
	}
	a.Zone = fqdn(a.Zone)

	if a.Target == "" {
		host, _ := os.Hostname()
		a.Target = host + "." + strings.TrimSuffix(a.Zone, ".")
	}
	a.Target = fqdn(a.Target)

	if a.TTL == 0 {
		a.TTL = caddy.Duration(defaultTTL)
	}

	if a.Strict == nil {
		v := true
		a.Strict = &v
	}

	if a.DNSProviderRaw == nil {
		return fmt.Errorf("a DNS provider is required")
	}
	val, err := ctx.LoadModule(a, "DNSProviderRaw")
	if err != nil {
		return fmt.Errorf("loading dns_provider: %w", err)
	}
	p, ok := val.(dnsProvider)
	if !ok {
		return fmt.Errorf("dns provider %T must implement RecordGetter, RecordSetter, and RecordDeleter", val)
	}
	a.provider = p

	return nil
}

func (a *App) Start() error {
	existing, err := a.provider.GetRecords(a.ctx, a.Zone)
	if err != nil {
		return fmt.Errorf("fetching records for zone %s: %w", a.Zone, err)
	}

	// Any existing CNAME whose target is ours
	owned := make(map[string]libdns.Record, len(existing))
	for _, r := range existing {
		rr := r.RR()
		if rr.Type == "CNAME" && fqdn(rr.Data) == a.Target {
			owned[rr.Name] = r
		}
	}

	// Relative names extracted from HTTP host matchers.
	desired := make(map[string]struct{})
	for _, name := range a.desiredNames() {
		desired[name] = struct{}{}
	}

	var toAdd []libdns.Record
	for name := range desired {
		if _, exists := owned[name]; !exists {
			toAdd = append(toAdd, libdns.CNAME{
				Name:   name,
				Target: a.Target,
				TTL:    time.Duration(a.TTL),
			})
		}
	}

	var toDelete []libdns.Record
	for name, rec := range owned {
		if _, exists := desired[name]; !exists {
			toDelete = append(toDelete, rec)
		}
	}

	if len(toAdd) == 0 && len(toDelete) == 0 {
		a.logger.Debug("DNS already in sync", zap.String("zone", a.Zone))
		return nil
	}

	if len(toAdd) > 0 {
		added, err := a.provider.SetRecords(a.ctx, a.Zone, toAdd)
		if err != nil {
			return fmt.Errorf("setting CNAME records: %w", err)
		}
		for _, r := range added {
			rr := r.RR()
			a.logger.Info("CNAME created",
				zap.String("name", rr.Name),
				zap.String("zone", a.Zone),
				zap.String("target", rr.Data),
			)
		}
	}

	if len(toDelete) > 0 {
		deleted, err := a.provider.DeleteRecords(a.ctx, a.Zone, toDelete)
		if err != nil {
			return fmt.Errorf("deleting stale CNAME records: %w", err)
		}
		for _, r := range deleted {
			rr := r.RR()
			a.logger.Info("stale CNAME deleted",
				zap.String("name", rr.Name),
				zap.String("zone", a.Zone),
			)
		}
	}

	return nil
}

// no-op
func (a *App) Stop() error { return nil }

// desiredNames returns the set of relative DNS names that should have CNAME
// records, derived from all `host` matchers in the HTTP app's configured routes.
func (a *App) desiredNames() []string {
	cai, err := a.ctx.App("http")
	if err != nil {
		return nil
	}
	ca := cai.(*caddyhttp.App)

	seen := make(map[string]struct{})
	for _, srv := range ca.Servers {
		for _, route := range srv.Routes {
			for _, matcherSet := range route.MatcherSets {
				for _, m := range matcherSet {
					if hostMatcher, ok := m.(caddyhttp.MatchHost); ok {
						for _, h := range hostMatcher {
							a.logger.Debug("found host", zap.String("name", h))
							seen[h] = struct{}{}
						}
					}
				}
			}
		}
	}

	zoneSuffix := "." + strings.TrimSuffix(a.Zone, ".")

	names := make([]string, 0, len(seen))
	for name := range seen {
		name = strings.ToLower(strings.TrimSuffix(name, "."))
		if !strings.HasSuffix(name, zoneSuffix) && *a.Strict {
			a.logger.Debug("skipping different zone (strict mode)", zap.String("host", name))
			continue
		}

		name := strings.TrimSuffix(name, zoneSuffix)
		if name == "" {
			a.logger.Debug("skipping zone apex", zap.String("host", name))
			continue
		}

		names = append(names, name)
	}

	return names
}

// helper to normalize fqdn
func fqdn(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if !strings.HasSuffix(s, ".") {
		s += "."
	}
	return s
}

const defaultTTL = time.Hour

var (
	_ caddy.App         = (*App)(nil)
	_ caddy.Provisioner = (*App)(nil)
)
