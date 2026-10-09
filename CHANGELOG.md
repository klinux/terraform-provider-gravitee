# Changelog

## 0.2.2

`gravitee_api` now manages the API's `state`, defaulting to `STARTED`.

An API created through the import endpoint comes up **stopped** -- the server
does this on purpose, the same as the console, where you create an API and
then start it. The provider did not expose it, so it created APIs that the
gateway answered `No context-path matches the request URI` for, as if they
did not exist, while the apply reported success. Nothing in the plan or the
output hinted at it.

## 0.2.1

Computed attributes now hold their value across an update instead of going
unknown at plan time: `groups`, `status`, `type` and `client_id` on an
application, and `api_id`, `status` and `client_id` on a subscription.

Found while adopting real applications. With `groups` undeclared, the plan
rendered it leaving and becoming `(known after apply)`, while the apply
actually preserved it -- the plan was describing something that would not
happen, on an attribute that controls who can see the application.

## 0.2.0

Adds `gravitee_platform_flows`, the organization's flows, which run for every
API on the gateway. They are fields on the organization rather than a flow
endpoint, written with a whole-entity `PUT`, so the resource preserves the
organization's other fields. Destroy leaves them in place unless
`clear_on_destroy` says otherwise.

## 0.1.1

First usable release. 0.1.0 was published with the registry manifest missing
from `SHA256SUMS`, which the registry refuses, and has been withdrawn.

## 0.1.0 (withdrawn)

Resources `gravitee_application`, `gravitee_subscription` and `gravitee_api`
(v2 definitions). Data sources `gravitee_api` and `gravitee_application`,
looking objects up by `id` or by name.

Calls are retried with backoff on `429`, `503` and, except for `POST`, `502`
and `504`; calls in flight are capped. Configured with `retries` and
`max_concurrent_requests`.

Fixed before any release: an application update used to clear fields the
configuration did not declare, which removed the group the server had assigned
at creation.

Validated against APIM 3.15.x, with an acceptance suite gated on `TF_ACC`.
