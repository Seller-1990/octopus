# Octopus Routing Context

Octopus unifies multiple upstream AI services behind a routable API. This glossary keeps product language stable while observability, routing, protocol, and pricing behavior evolve.

## Upstream Access

**Site**:
An upstream AI service installation or commercial service with its own API, accounts, models, and pricing. The analytics UI uses Site as its supplier dimension.
_Avoid_: Provider

**Site Account**:
A credentialed identity within one Site. Multiple Site Accounts may belong to the same Site and expose different groups, balances, or models.
_Avoid_: Site, channel

**Channel**:
A routable upstream access target used by Octopus to serve client requests. A Channel may be managed directly or projected from a Site Account.
_Avoid_: Site, provider

**Projected Channel**:
A Channel derived from Site Account data so that synchronized credentials, groups, and models can participate in routing.
_Avoid_: Site channel

**Proxy Path**:
The direct connection or named proxy route used for a Site operation or upstream request. A Proxy Path may include a Proxy Endpoint and a selected Clash Node.
_Avoid_: Proxy endpoint

**Proxy Endpoint**:
An HTTP, HTTPS, SOCKS, or SOCKS5 address through which Octopus sends traffic.
_Avoid_: Clash node

**Clash Node**:
A selectable outbound route managed inside a configured Clash or Mihomo proxy group.
_Avoid_: Proxy endpoint

**Clash Controller**:
The management API used to inspect and switch nodes in a dedicated Clash or Mihomo proxy group.
_Avoid_: Proxy endpoint

**Preferred Proxy Path**:
A recently validated Proxy Path that a Site tries first. A Site Account may override the Site preference when its access conditions differ.
_Avoid_: Permanent proxy binding

**Verification Session**:
A time-limited browser-validated access session bound to a Site Account, Proxy Path, and User-Agent.
_Avoid_: Cloudflare bypass, site credential

**Verification Bridge**:
An administrator-side browser extension that completes an interactive Site verification task and returns only the required Verification Session to Octopus.
_Avoid_: Server-side browser

## Models And Routing

**Upstream Model**:
A model identifier exactly as exposed by a specific Channel.
_Avoid_: Canonical model

**Canonical Model**:
The normalized model identity presented for aggregation and routing across Channels.
_Avoid_: Upstream model, model group

**Model Alias**:
An alternate model name that resolves to one Canonical Model, such as `GLM-5.1` resolving to `glm-5.1`.
_Avoid_: Model copy

**Route Candidate**:
A Channel and Upstream Model pair eligible to serve a Canonical Model.
_Avoid_: Supplier

**API Protocol**:
The wire contract used by a client or Channel, such as OpenAI Chat Completions, OpenAI Responses, or Anthropic Messages.
_Avoid_: Model format

**Protocol Policy**:
The rule that decides whether a Route Candidate may use same-protocol passthrough, protocol transformation, or both.
_Avoid_: Channel type

**Protocol Passthrough**:
Forwarding a request and response through the same API Protocol while preserving supported provider-specific fields.
_Avoid_: Header passthrough

**Protocol Transformation**:
Converting request and response semantics between different API Protocols.
_Avoid_: Model mapping

**Header Policy**:
A named rule that determines which client headers are forwarded and which fixed headers are applied for a Route Candidate.
_Avoid_: Protocol passthrough

**Effective Price**:
The resolved Site Model Price used to estimate a request cost after source precedence and inheritance are applied.
_Avoid_: Canonical model price

## Requests And Outcomes

**Client Request**:
One API invocation received by Octopus from a client.
_Avoid_: Upstream attempt

**Upstream Attempt**:
One attempt to serve a Client Request through a specific Route Candidate.
_Avoid_: Request

**Request Outcome**:
The semantic final result of a Client Request, independent from how its transport connection later closes.
_Avoid_: Connection status

**Transport Termination**:
The way a client or upstream connection ended. A cancellation does not by itself determine the Request Outcome.
_Avoid_: Request outcome

**Client Cancellation**:
A Request Outcome used when the client disconnects before a semantic terminal response. It is distinct from both successful completion and an upstream failure.
_Avoid_: Channel failure, success

## Pricing

**Site Model Price**:
Pricing terms published by a specific Site for an Upstream Model.
_Avoid_: Global model price

**Price Source**:
The origin and retrieval time of pricing terms used by Octopus.
_Avoid_: Price
