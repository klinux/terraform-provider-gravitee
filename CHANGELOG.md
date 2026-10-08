# Changelog

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
