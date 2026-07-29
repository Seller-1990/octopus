---
status: accepted
---

# Isolate Clash node switching

Octopus supports both ordinary proxy endpoints and optional Clash or Mihomo Controller integration, but automatic node switching is restricted to explicitly configured dedicated proxy groups. It does not modify the global selector by default, preventing site recovery attempts from changing network behavior for unrelated applications sharing the same proxy instance.
