---
status: accepted
---

# Use assisted Cloudflare session recovery

When Octopus detects a Cloudflare challenge, it pauses blind retries and asks an administrator to complete browser verification or import a legitimate session. The resulting Verification Session is encrypted, time-limited, and bound to the Site Account, Proxy Path, and User-Agent; Octopus does not attempt automated CAPTCHA circumvention.
