---
status: accepted
---

# Prefer passthrough with overridable automatic transformation

Octopus automatically selects a compatible API Protocol path, preferring same-protocol passthrough and using transformation only when no suitable passthrough route exists. Channel and Canonical Model policies can restrict routing to `passthrough-only` or permit transformation with `transform-allowed`, preventing special upstreams from being silently routed through an unwanted conversion.
