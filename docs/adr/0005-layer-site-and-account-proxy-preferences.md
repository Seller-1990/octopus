---
status: accepted
---

# Layer site and account proxy preferences

Octopus learns a Preferred Proxy Path at the Site level so accounts on the same upstream can reuse a validated network route. A Site Account may override that preference for account-specific Cloudflare sessions, regional behavior, or access differences, and falls back to the Site preference when its override is unavailable.
