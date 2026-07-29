---
status: accepted
---

# Retain aggregates beyond raw logs

Octopus keeps raw relay logs on the existing configurable retention policy, which defaults to seven days, while retaining hourly aggregates for 90 days and daily aggregates long term. Historical analytics therefore remain available after sensitive and storage-heavy request details are removed, with drill-down disabled for periods whose raw logs have expired.
