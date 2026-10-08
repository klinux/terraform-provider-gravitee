# Look up an API this configuration does not manage, by name.
data "gravitee_api" "billing" {
  name = "billing"
}

# Or by id.
data "gravitee_api" "by_id" {
  id = "00000000-0000-0000-0000-000000000000"
}

# plan_ids keeps plan UUIDs out of the configuration, so the same files work
# against more than one environment.
resource "gravitee_subscription" "consumer" {
  application_id = gravitee_application.consumer.id
  plan_id        = data.gravitee_api.billing.plan_ids["OAuth2"]
}

# Subscribing one application to several APIs, all resolved by name.
locals {
  apis = ["billing", "payments", "transfers"]
}

data "gravitee_api" "all" {
  for_each = toset(local.apis)
  name     = each.value
}

resource "gravitee_subscription" "fanout" {
  for_each       = data.gravitee_api.all
  application_id = gravitee_application.consumer.id
  plan_id        = each.value.plan_ids["OAuth2"]
}
