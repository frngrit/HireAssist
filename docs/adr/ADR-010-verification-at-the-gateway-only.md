# ADR-010: The gateway is the only place an access token is verified

**Date:** 2026-10-17
**Deciders:** Patiphon Puntusin (API Gateway), proposed to the team

> In the context of every recruiter-facing request arriving with a signed access token, facing a
> rule that each service behind the gateway verify that token itself, in its own language, we
> decided that **only the API gateway verifies**, and the services accept whatever reaches them,
> to achieve one implementation of the access check instead of one per language, accepting that
> network position becomes the services' only protection: anything able to open a connection to
> a service is already past every access check in the system.

---

## Summary

### Issue

[ADR-009](ADR-009-jwt-with-refresh-tokens.md) decided that access is carried by a short-lived
signed token, verified locally by whoever uses it rather than by calling the service that issued
it. It asked for that verification in two places: at the gateway, and again in **every service
that acts on the caller's role**, so that no service accepts a role from an unsigned header.
[ADR-001](ADR-001-service-decomposition.md) is where that second requirement comes from — it
warns that enforcement cannot live at the gateway alone.

The gateway's half is built. The services' half has not started, and this record is about
whether it should.

Four forces.

**The check would be written once per language.** Services choose their own languages
([ADR-005](ADR-005-per-service-language.md)); there are three across five services. Verifying a
signature, enforcing an expiry with clock leeway, refusing any algorithm but the one agreed, and
mapping a role to a permission would be implemented separately in each, by different people, to
be got right every time. ADR-005 names a privilege mistake in one implementation as the highest
risk it accepts.

**Most internal calls carry no token at all.** Resume Processing calls back into Hiring when a
resume has been scored. The retention sweep calls erasure operations on two services. A
scheduler triggers the sweep. There is no member behind any of these, so there is no member
token to verify. A rule that every service verifies the caller's token therefore does not
describe the traffic those services actually receive — it would need a second mechanism, for
service-to-service identity, that nothing has specified.

**One deployment serves one company** ([ADR-006](ADR-006-single-tenant-deployment.md)). The
services of an installation run together, operated by the company whose data they hold, and only
the gateway is reachable from outside. There is no shared cluster and no second tenant whose
traffic could arrive at a service by another route.

**The team is four part-time developers under a deadline.** Security code that is written five
times and reviewed once is a worse bet than security code written once.

### Decision

**The API gateway is the only component that verifies an access token.** Everything behind it
treats the request it receives as already authorised.

- The gateway verifies the signature and the expiry of every access token, rejects a request
  above the caller's role, and records the rejected attempt. A request it forwards has passed
  those checks.
- **No other service verifies anything.** The services behind the gateway do not read the token
  to decide what a caller may do, do not call anyone to check it, and carry no verification key.
- **The token is still forwarded unchanged.** A service may read it to attribute an action in a
  log, knowing that what it reads is only as trustworthy as the network it arrived on.
- **Only the gateway is published.** No other service may be given an ingress, a published port
  or any other route from outside the deployment. This is a deployment rule, and it is the whole
  of the protection the services have.
- Identity headers a client sends are stripped at the gateway, so a caller cannot claim a role
  by asserting one.

### Status

**Proposed.** It binds two services whose owners have not agreed to it: the Identity Service and
the Hiring Service both stop being responsible for their own access checks. It becomes Accepted
when those owners agree and the deployment configuration publishes the gateway alone.

### Group

Security · Deployment

---

## Details

### Assumptions

- Nothing but the gateway is reachable from outside the deployment, and nothing but the gateway
  ever will be without this record being revisited.
- Every process inside the deployment is run by the company that owns the data in it. A workload
  belonging to someone else does not run beside these services.
- An installation has tens of members and one company's data, so the worst case of a mistake is
  one company's hiring record, not many.
- Developers running a service directly on a laptop understand that it has no access control at
  all.
- Adding verification to a service later is additive: the token is already forwarded, so a
  service that needs to check it can start doing so without any other service changing.

### Constraints

- ADR-009 supplies the token, its claims and its lifetime. Nothing here changes what is issued,
  only who checks it.
- ADR-001 puts a gateway in front of the system and requires a verified identity and role on
  every request. That still happens; this record decides it happens once.
- The course requires authentication alongside the business use cases, and an API gateway.
- Candidate personal data is protected under Thailand's PDPA, so who may reach it is a legal
  question, not only an engineering one. This decision concentrates the answer in one component.

### Positions

1. **Every service verifies the token it receives** — ADR-009 as written.
2. **The gateway verifies; the services trust the private network** *(chosen)*.
3. **The gateway verifies, and proves itself to each service** with a credential the service
   checks — mutual TLS, or a signed header only the gateway can produce.
4. **A service mesh** enforces identity and policy between services, outside their code.
5. **A partial rule**: services verify only on operations that touch candidate personal data.

### Argument

**Against verification everywhere (1).** Its cost is five implementations of the same security
code in three languages, each able to be wrong on its own. Its benefit is defence against a
caller that reaches a service directly — but the rule as written does not actually provide that,
because the internal callers named above carry no member token, so each service would need to
accept unauthenticated internal traffic anyway or a second, unspecified mechanism to identify a
calling service. The requirement is incomplete rather than merely expensive, and completing it
is positions 3 and 4.

**Against proving the gateway's identity (3) and a service mesh (4).** These are the right
answer for a production system, and what this one should adopt if it outlives the course.
Both cost certificates to issue, distribute and rotate, or sidecars and a control plane to
operate, and the knowledge to debug them when a handshake fails the morning of a demo. Neither
changes the product: both are deployment-level changes that can be added later without touching
a single use case. Paying for them now, with four part-time developers, buys protection against
an attacker who is already inside a single-company cluster.

**Against the partial rule (5).** It pays most of the cost of position 1 — security code in
several languages — and protects some operations. The boundary it draws, *operations that touch
personal data*, is a list that grows every time someone adds an endpoint, and nothing fails when
they forget.

**For the gateway alone (2).** The check exists once, in the component whose purpose is exactly
that, where it is read, reviewed and tested as one thing. It is the only option whose cost does
not scale with the number of languages, which is what makes it affordable to do well rather than
five times badly. The exposure it accepts is bounded by the deployment model: one company, one
cluster, one published entry point. And it is reversible in the direction that matters — if a
service later needs its own check, the token is already in the request.

### Implications

**What this buys us**

- One implementation of signature, expiry, algorithm and role checking, in one language, with
  tests that run in one place.
- The risk ADR-005 named — a privilege mistake in one of several implementations — does not
  arise for this concern, because there is only one implementation.
- Internal calls need no member token and no service identity, so the callback into Hiring, the
  erasure calls and the scheduler's triggers work without a second authentication mechanism.
- Services behind the gateway carry no security code, no verification key and no clock-skew
  rules.

**What it costs us**

- **Network position is the only protection the services have.** Anything that can open a
  connection to a service port can do everything an Admin can do: read every candidate's data,
  change criteria, erase records. A port published by mistake, a port forward left open, or one
  compromised process inside the deployment is total.
- **It withdraws a commitment of ADR-009 and reverses ADR-001.** Those records say enforcement
  cannot live at the gateway alone. They are not edited; this record is where the position
  changes, and a reader of either must read this one.
- **Immediate demotion is lost.** ADR-009 has the Identity Service re-read a caller's role from
  its own database before managing members, so a demoted Admin loses that power at once. With no
  token read in that service, nothing identifies the caller there, and a demoted Admin is stopped
  only when their access token expires — up to fifteen minutes.
- **In-cluster traffic is unauthenticated and, in this deployment, unencrypted.** NFR-11 requires
  data in transit over TLS 1.2 or higher. The gateway terminates TLS; everything behind it is
  plain HTTP between processes, so meeting that requirement now depends entirely on the platform
  the services are deployed onto.
- **A service's own log cannot say who did something** and be believed, because the identity it
  reads was never verified there.
- **Running a service alone is running it open.** A demonstration that does not go through the
  gateway demonstrates a system with no access control, which has to be said rather than
  discovered by a marker.
- **The deployment configuration becomes security-critical.** Publishing only the gateway stops
  being an operational preference and becomes the control that protects candidate personal data,
  so it is the thing to check before an installation is handed over.

---

## Related

### Related decisions

- [ADR-001](ADR-001-service-decomposition.md) — placed a gateway in front of the system and
  warned that role enforcement cannot live there alone. This record takes the opposite position
  on that second point and leaves the first unchanged.
- [ADR-005](ADR-005-per-service-language.md) *(Proposed)* — per-service languages, whose
  accepted cost includes role enforcement implemented more than once. That cost is not paid: the
  check exists in one language only.
- [ADR-006](ADR-006-single-tenant-deployment.md) — one deployment per customer company. This is
  the record that makes a private-network assumption defensible, and the reason the blast radius
  of a mistake is one company.
- [ADR-009](ADR-009-jwt-with-refresh-tokens.md) — the token, its claims and its lifetimes, all
  unchanged. Its requirement that every service acting on the role verify the token itself, and
  that no service accept a role from an unsigned header, no longer holds: no service reads a
  role at all.

### Related requirements

- **FR-0.2**, rejecting a request with a missing, invalid or expired token, is done at the
  gateway and nowhere else.
- **FR-0.3**, two roles with Admin holding every Recruiter permission, is enforced at the
  gateway. No service enforces a permission of its own.
- **FR-0.4**, rejecting a request above the caller's role and recording the attempt, happens in
  the one component that can still tell.
- **FR-6.1**, an Admin creating, removing and re-roling members, is protected by the gateway's
  role check. The removal itself still takes effect in the Identity Service's data.
- **NFR-09**, 99% availability in business hours: unchanged, and the gateway remains a single
  point whose failure stops all user traffic — it was already that.
- **NFR-11**, TLS for all data in transit: met at the edge, and dependent on the deployment
  platform behind it, as described above.
- **Course requirements:** authentication alongside the business use cases, and an API gateway.

### Related artifacts

The deployment manifests, which must publish the gateway and nothing else; the gateway's route
table, which is now the complete statement of who may reach what; and the runtime view and the
service operations table, which describe every service's exposure.

### Related principles

- *Easily reversible* — the token is still forwarded, so a service that later needs its own
  check adds one without any other service changing.
- *PDPA is a design force, not a feature* — the protection of candidate data now rests on one
  component and one deployment rule, which is stated here so that it is checked rather than
  assumed.

---

## Notes

This decision should be revisited the first time any of its assumptions moves: a second tenant
in one deployment, any service published for a reason that seems good at the time, or a cluster
shared with workloads this team does not operate. Position 3 or 4 is the upgrade path, and
neither requires a use case to change.

Recorded after the gateway was built and the services were not, which is the honest order: the
question was live because the second half of the work had not started.
