# Changelog

## Unreleased

Resources `gravitee_application`, `gravitee_subscription` and `gravitee_api`
(v2 definitions). Data sources `gravitee_api` and `gravitee_application`,
looking objects up by `id` or by name.

Calls are retried with backoff on `429`, `503` and, except for `POST`, `502`
and `504`; calls in flight are capped. Configured with `retries` and
`max_concurrent_requests`.

Fixed before any release: an application update used to clear fields the
configuration did not declare, which removed the group the server had assigned
at creation.

Validated against APIM 3.15.22, with an acceptance suite gated on `TF_ACC`.
