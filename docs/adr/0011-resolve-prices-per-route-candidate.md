---
status: accepted
---

# Resolve prices per route candidate

Octopus resolves an Effective Price for the actual Route Candidate rather than assigning one price to a Canonical Model. Explicit route overrides take precedence over exact and inherited Site prices, stale last-known prices, and global fallback prices, while each request stores the source and price snapshot used for its historical cost.
