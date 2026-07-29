---
status: accepted
---

# Limit adaptive proxy recovery to site operations

Adaptive Clash node switching initially applies only to site-management operations such as account refresh, model and pricing synchronization, balance checks, and check-in. Real-time model relay continues to use explicit Channel proxy settings, preventing background site recovery from disrupting active streaming requests or changing their network path.
