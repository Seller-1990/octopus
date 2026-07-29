---
status: accepted
---

# Layer header policies and protect credentials

Header Policy values inherit from global through Site, Site Account, Channel, Canonical Model, and Route Candidate scopes, with more specific rules winning. Client passthrough uses a safe allowlist, while authentication, proxy, Cookie, host, length, forwarding, IP, and hop-by-hop headers remain protected and are injected only by trusted transport or verification-session code.
