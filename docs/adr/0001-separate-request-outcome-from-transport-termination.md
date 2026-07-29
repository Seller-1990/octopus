---
status: accepted
---

# Separate request outcome from transport termination

A canceled connection does not by itself make a Client Request fail. Octopus determines Request Outcome from protocol completion semantics, records pre-terminal client disconnects as Client Cancellation without penalizing channel health, and retains any actual usage and cost; historical records are repaired only when completion can be proven through a previewed and auditable process.
