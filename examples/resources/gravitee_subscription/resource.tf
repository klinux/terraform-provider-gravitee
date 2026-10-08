resource "gravitee_subscription" "partner_billing" {
  application_id = gravitee_application.partner.id

  # Taking the id from gravitee_api.plan_ids avoids hardcoding plan UUIDs,
  # which differ between environments.
  plan_id = gravitee_api.billing.plan_ids["OAuth2"]
}

# Importing an existing subscription accepts either
# "<application_id>:<plan_id>" or "<application_id>:<subscription_id>".
import {
  to = gravitee_subscription.partner_billing
  id = "00000000-0000-0000-0000-000000000000:11111111-1111-1111-1111-111111111111"
}
