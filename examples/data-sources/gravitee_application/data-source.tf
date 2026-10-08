# Look up an application something else owns — a console operator or a script —
# without bringing it under Terraform first.
data "gravitee_application" "partner" {
  name = "partner-app"
}

# Subscribing it to an API, with neither the application id nor the plan id
# written out.
data "gravitee_api" "billing" {
  name = "billing"
}

resource "gravitee_subscription" "partner_billing" {
  application_id = data.gravitee_application.partner.id
  plan_id        = data.gravitee_api.billing.plan_ids["OAuth2"]
}
