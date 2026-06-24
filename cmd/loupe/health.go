package main

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// healthComponent is one card the UI renders. Name is the descriptive label
// (a component name, or a lens's canonicalName); Detail is a secondary line
// (the component instance id, or "lens · <description>"); Key is the raw Health
// KV key (kept for reference / control-plane lookups).
type healthComponent struct {
	Key       string   `json:"key"`
	Group     string   `json:"group"`
	Name      string   `json:"name"`
	Detail    string   `json:"detail,omitempty"`
	Status    string   `json:"status"`
	Freshness string   `json:"freshness"`
	Issues    []string `json:"issues,omitempty"`
}

// healthRollup is the GET /api/health response.
type healthRollup struct {
	Overall    string            `json:"overall"`
	Components []healthComponent `json:"components"`
	Alerts     []string          `json:"alerts"`
}

// How a Health KV key is rendered.
const (
	kindComponent = "component"
	kindLens      = "lens"
	kindBootstrap = "bootstrap"
	kindAlert     = "alert"
	kindGate      = "gate"
	kindEvent     = "event"
)

// classifyHealthKey groups a Health KV key. A `health.<component>.<instance>`
// key (no further dots) is that component's Contract #5 heartbeat — this covers
// processor, refractor, loom, weaver, bridge, and object-store-manager
// uniformly. A deeper `health.<component>.…` key is a per-component event;
// bootstrap / gate / alert keys are recognized explicitly. Everything else is a
// bare-NanoID lens reporter (the lens's meta.lens vertex id).
func classifyHealthKey(key string) (group, kind string) {
	switch {
	case strings.HasPrefix(key, "health.bootstrap."):
		return "bootstrap", kindBootstrap
	case strings.HasPrefix(key, "health.gates."):
		return "gate", kindGate
	case strings.HasPrefix(key, "health.alerts."):
		return "alert", kindAlert
	case strings.HasPrefix(key, "health."):
		comp, inst, found := strings.Cut(strings.TrimPrefix(key, "health."), ".")
		if found && comp != "" && inst != "" && !strings.Contains(inst, ".") {
			return comp, kindComponent
		}
		return comp, kindEvent
	default:
		return "lens", kindLens
	}
}

// freshness formats time-since as "Xs ago", clamping a future timestamp (clock
// skew between emitter and Loupe host) to "0s ago".
func freshness(t time.Time) string {
	d := time.Since(t).Round(time.Second)
	if d < 0 {
		d = 0
	}
	return fmt.Sprintf("%ds ago", int64(d.Seconds()))
}

// parseHealthTime reads an RFC3339 timestamp out of a JSON value map.
func parseHealthTime(doc map[string]any, key string) (time.Time, bool) {
	v, ok := doc[key].(string)
	if !ok {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, v)
	return t, err == nil
}

// componentHeartbeat reads a component's heartbeat timestamp. Most daemons stamp
// "heartbeatAt"; object-store-manager stamps "updatedAt" — both are tried.
func componentHeartbeat(doc map[string]any) (time.Time, bool) {
	for _, field := range []string{"heartbeatAt", "updatedAt"} {
		if ts, ok := parseHealthTime(doc, field); ok {
			return ts, true
		}
	}
	return time.Time{}, false
}

// computeHealth evaluates every Health KV entry into component cards plus an
// overall rollup (green/yellow/red). readEntry returns the decoded JSON doc for
// a key (and false to skip). resolveLens maps a lens reporter id to its
// (canonicalName, description) for a readable card label (nil disables it, e.g.
// in tests). staleThreshold is the heartbeat age past which a component is
// "stale" (yellow).
func computeHealth(
	keys []string,
	readEntry func(string) (map[string]any, bool),
	resolveLens func(id string) (name, desc string),
	staleThreshold time.Duration,
) healthRollup {
	const (
		green  = 0
		yellow = 1
		red    = 2
	)
	overall := green
	worse := func(lvl int) {
		if lvl > overall {
			overall = lvl
		}
	}

	components := make([]healthComponent, 0, len(keys))
	alerts := make([]string, 0)
	bootstrapPresent := false

	for _, k := range keys {
		doc, ok := readEntry(k)
		if !ok {
			continue
		}
		group, kind := classifyHealthKey(k)
		switch kind {
		case kindComponent:
			c := healthComponent{Key: k, Group: group, Name: group}
			if comp, ok := doc["component"].(string); ok && comp != "" {
				c.Name = comp
			}
			if inst, ok := doc["instance"].(string); ok && inst != "" {
				c.Detail = inst
			}
			if ts, ok := componentHeartbeat(doc); ok {
				c.Freshness = freshness(ts)
				if time.Since(ts) > staleThreshold {
					c.Status = "stale"
					c.Issues = append(c.Issues, "heartbeat older than "+staleThreshold.String())
					worse(yellow)
				} else {
					c.Status = "green"
				}
			} else {
				c.Status = "unknown"
				c.Freshness = "-"
				worse(yellow)
			}
			components = append(components, c)

		case kindLens:
			c := healthComponent{Key: k, Group: "lens", Name: k, Detail: "lens", Freshness: "-"}
			if resolveLens != nil {
				if name, desc := resolveLens(k); name != "" {
					c.Name = name
					if desc != "" {
						c.Detail = "lens · " + desc
					}
				}
			}
			status, _ := doc["status"].(string)
			consumerLag, _ := doc["consumerLag"].(float64)
			errorCount, _ := doc["errorCount"].(float64)
			switch status {
			case "active":
				if consumerLag > 0 {
					c.Status = "yellow"
					c.Issues = append(c.Issues, fmt.Sprintf("consumerLag=%.0f", consumerLag))
					worse(yellow)
				} else {
					c.Status = "active"
				}
			case "paused", "rebuilding":
				c.Status = status
				worse(yellow)
			default:
				c.Status = "unknown"
				worse(yellow)
			}
			if errorCount > 0 {
				c.Issues = append(c.Issues, fmt.Sprintf("errorCount=%.0f", errorCount))
				worse(yellow)
			}
			components = append(components, c)

		case kindBootstrap:
			bootstrapPresent = true
			components = append(components, healthComponent{
				Key:       k,
				Group:     "bootstrap",
				Name:      "bootstrap",
				Status:    "green",
				Freshness: "-",
			})

		case kindAlert:
			severity, _ := doc["severity"].(string)
			msg, _ := doc["message"].(string)
			alerts = append(alerts, fmt.Sprintf("[%s] %s: %s", severity, k, msg))
			switch severity {
			case "error":
				worse(red)
			case "warning":
				worse(yellow)
			}
		}
		// kindGate and kindEvent are not rendered as cards.
	}

	if !bootstrapPresent {
		worse(red)
	}

	sort.Slice(components, func(i, j int) bool {
		if components[i].Name != components[j].Name {
			return components[i].Name < components[j].Name
		}
		return components[i].Key < components[j].Key
	})

	return healthRollup{
		Overall:    [...]string{"green", "yellow", "red"}[overall],
		Components: components,
		Alerts:     alerts,
	}
}
