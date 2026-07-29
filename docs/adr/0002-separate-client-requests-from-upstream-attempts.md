---
status: accepted
---

# Separate client requests from upstream attempts

Octopus counts each received Client Request once even when routing performs retries or failover. Channel health and upstream performance metrics are calculated from individual Upstream Attempts, while Token and cost are attributed to every route that actually incurred measurable usage, preventing both inflated traffic totals and hidden upstream failures.
