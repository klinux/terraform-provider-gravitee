# terraform-provider-gravitee

A Terraform provider for **Gravitee APIM 3.x**, talking to the Management API.

Gravitee's own provider (`gravitee-io/apim`) requires APIM 4.8 or newer, speaks
to the Automation API, and only manages v4 APIs. This one targets the 3.x
Management API and v2 API definitions (`flows`), which that provider cannot see.

## Resources

| Resource | Covers |
| --- | --- |
| `gravitee_application` | full CRUD, plus import by id |
| `gravitee_subscription` | create, read, close, import by `<application_id>:<plan_id>` |
| `gravitee_api` | v2 definitions as opaque JSON, plans included, conditional deploy |

Tested against APIM 3.15.22.

## Why the provider is thin

It does not model policies. A policy's `configuration` is an arbitrary JSON
object — there are dozens of policy types and installations carry their own
custom ones — so typing it would be neither feasible nor useful. The definition
is passed through exactly as `GET /apis/{id}/export` returns it.

What the provider *does* know is the set of asymmetries in the 3.x Management
API that are not in its published OpenAPI document. Each was found by testing
against a live instance.

## What the API does that its OpenAPI document does not say

The instance publishes its own spec at `GET /management/swagger.json`, and
Gravitee publishes one per version. Both are useful, and both are silent or
wrong on the following.

### Deleting a subscription is a close, not a delete

`DELETE /applications/{app}/subscriptions/{sub}` is documented as *"Close the
subscription"*. The record survives with `status: CLOSED` and a `closed_at`.
The default listing hides it, but a `GET` by id returns it.

Treating `CLOSED` as absent is what makes `destroy` followed by `apply`
converge; without it Terraform sees a resource that will never go away.

### Subscription references come back in two shapes

`POST .../subscriptions` and `GET .../subscriptions/{id}` return `api`, `plan`
and `application` as objects (and the detail endpoint returns `application` as
`null`). The list endpoint returns them as plain UUID strings. The spec types
the first two correctly and leaves the list as a generic `PagedResult` with no
item schema.

### Importing an API needs the API's own id in the body

`PUT /apis/{id}/import` pairs plans by id. Bisected against a live instance:

| top-level `id` | `plans[].id` | Result |
| --- | --- | --- |
| present | present | `200`, plans preserved |
| present | absent | `400 plan.notDeletable` |
| absent | present | `400 plan.notDeletable` |

With either missing, the import treats the plans as new and tries to delete the
existing ones. If a plan has subscriptions you get
`"You can't delete a plan with existing subscriptions"`. **If it has none, the
plan is deleted silently.**

The provider sends the top-level id and pairs plans by name to carry their ids
across. Duplicate plan names fail the apply, because the pairing would be
ambiguous.

### The import is not atomic

An import that ends in `400` may already have applied part of the definition.
In testing, a policy's configuration was written and only then did the plan
stage fail. The provider says so in the error, and skips the write entirely
when the definition has not changed.

### Deleting an API requires closing its plans first

`DELETE /apis/{id}` answers `400` with
`"Plan(s) [...] must be closed before being able to delete the API"`. The
provider closes them and warns, because closing a plan closes its
subscriptions.

### The import does not honour a plan's `order`

Send `order` 0 and 1 and both plans come back as 0. The field is treated as
server-owned, and the apply warns when a declared order did not take effect,
rather than hiding it or producing a diff that never converges.

### The export returns plans in its own order

Not the order they were sent in. Plans are a set keyed by `name`, so the
provider aligns them by name before comparing; otherwise every API with more
than one plan would report a false difference.

### The server adds defaults

A plan comes back with `comment_required: false` and `type: "API"`; a step gains
`description: ""`. That is not a divergence. Verification therefore checks that
everything sent is present and equal — a subset check — and the state is
projected recursively onto the shape the definition declares.

### Creating an application without `groups` assigns one

The server picks a default group. Both `groups` and `client_id` are therefore
`Optional + Computed`.

### An API export can contain secrets

The export includes `resources`, and an `oauth2-keycloak-resource` carries its
client secret in clear text. Do not commit a raw export to version control.
Since an undeclared key is preserved (see below), the simplest answer is to keep
`resources` out of the file you version.

## Design decisions

**It reads back and compares.** After every create and update the provider
re-reads the object and checks it field by field against what was sent, failing
the apply on a mismatch. The Management API accepts fields it does not
understand and answers `200` without applying them — this is how a
`selectionRule` written in camelCase becomes a silent no-op. This check is the
main reason to use a provider rather than a script.

**An undeclared key is preserved, not removed.** A partial `definition` manages
only the top-level keys it declares; `Update` starts from the current export and
overlays what was declared. The alternative is destructive: omitting `resources`
would delete the API's `oauth2-keycloak-resource` and break its OAuth2 plan. To
stop managing a key, remove it; to delete something, declare it empty.

**Plans live inside `gravitee_api`.** Not a preference: the import deletes any
plan missing from the payload, so a separate plan resource would be destroyed by
the API resource's own import.

**Updating an application sends the whole body.** `UpdateApplicationEntity`
requires `name`, `description` and `settings`, and a partial `PUT` wipes
`settings`. The provider does a read-modify-write and preserves
`settings.oauth`, which it does not model.

**Subscriptions have no update.** The API exposes none, so `application_id` and
`plan_id` force replacement — and replacing one briefly cuts the consumer's
access, which the plan makes visible.

**An existing subscription is adopted, not duplicated.** A `POST` for an
application/plan pair that already has an active subscription fails, so `Create`
looks first and adopts with a warning. This keeps an apply idempotent over
objects created by hand before Terraform arrived.

**`plan_ids` uses `UseStateForUnknown`.** Without it any update to an API leaves
`plan_ids` unknown at plan time, which propagates into
`gravitee_subscription.plan_id` and forces a replacement — a cosmetic API change
would tear down a consumer's subscription.

**v1 definitions are rejected at plan time.** In a `1.0.0` definition the policy
id *is* the object key, with its configuration inlined
(`{"methods":["GET"],"policy-request-validation":{...}}`), while v2 uses named
fields. The provider does not model the v1 shape, and `ValidateConfig` refuses
it during `plan` rather than at apply — which matters, because the import is not
atomic.

## Usage

```hcl
terraform {
  required_providers {
    gravitee = {
      source  = "klinux/gravitee"
      version = "~> 0.1"
    }
  }
}

provider "gravitee" {
  endpoint = "https://apim.example.com/management"
  # token comes from GRAVITEE_TOKEN
}
```

Provider settings, each with an environment variable fallback:

| Attribute | Environment variable | Default |
| --- | --- | --- |
| `endpoint` | `GRAVITEE_ENDPOINT` | — |
| `token` | `GRAVITEE_TOKEN` | — |
| `organization` | `GRAVITEE_ORGANIZATION` | `DEFAULT` |
| `environment` | `GRAVITEE_ENVIRONMENT` | `DEFAULT` |
| `timeout_seconds` | — | `60` |

See [`examples/`](examples/) for each resource.

## Adopting objects that already exist

```bash
terraform import gravitee_application.x <application-uuid>
terraform import gravitee_subscription.y '<application-uuid>:<plan-uuid>'
terraform import gravitee_api.z <api-uuid>
```

For an API, generate the definition from the instance first — export it and drop
the fields the server owns (`id`, `primaryOwner`, `members`, `pages`, and
`id`/`created_at`/`updated_at`/`api`/`order` on each plan):

```bash
curl -sH "Authorization: Bearer $GRAVITEE_TOKEN" \
  "$GRAVITEE_ENDPOINT/organizations/DEFAULT/environments/DEFAULT/apis/<id>/export" \
  | jq 'del(.id,.primaryOwner,.members,.pages)
        | .plans |= map(del(.id,.created_at,.updated_at,.api,.order))' \
  > apis/<name>.json
```

With a complete definition, `plan` right after the import reports no changes and
writes nothing. With a partial one, the first `plan` shows the undeclared keys
leaving the state; that diff is about representation, not destruction — the
apply preserves what was not declared, and the next `plan` is clean.

## Building from source

```bash
go build -o terraform-provider-gravitee .
```

To use the local build, point Terraform at it with a dev override and skip
`terraform init`:

```hcl
# dev.tfrc — then: export TF_CLI_CONFIG_FILE=$PWD/dev.tfrc
provider_installation {
  dev_overrides {
    "klinux/gravitee" = "/path/to/terraform-provider-gravitee"
  }
  direct {}
}
```

Terraform warns about the override on every command, which is expected.

## Status

Early. Validated end to end against APIM 3.15.22: create, idempotence, drift
detection and correction, update preserving subscriptions and plan ids, the
plan-deletion guard, rejection of v1 definitions, destroy, and adoption of
existing objects by import.

Not covered yet: v1 (`paths`) definitions, API members and pages, documentation
pages, API metadata, and anything outside applications, subscriptions and v2 API
definitions.

Code comments are in Portuguese; the documentation is in English.

## License

Apache License 2.0. See [LICENSE](LICENSE).
