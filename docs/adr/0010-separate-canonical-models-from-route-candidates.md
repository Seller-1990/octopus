---
status: accepted
---

# Separate canonical models from route candidates

Octopus treats a Canonical Model as the stable client-facing identity and a Route Candidate as one Site, account, group, Channel, protocol, and Upstream Model combination that can serve it. Health, price, headers, protocol capability, and lifecycle belong to the Route Candidate, preventing alias consolidation from erasing meaningful upstream differences.
