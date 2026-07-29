---
status: accepted
---

# Run Cloudflare verification in the administrator browser

Octopus uses a paired administrator-side Verification Bridge to open temporary browser windows and complete interactive Cloudflare verification, following the mechanism demonstrated by `all-api-hub`. After pairing and one-time task-token validation, the bridge submits the target cookies and bound User-Agent; Octopus validates that data and encrypts the resulting Verification Session before persistence. Manual Cookie import remains a fallback, and server-side Playwright or Chromium is not required for the initial release.
