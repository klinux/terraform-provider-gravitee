# The definition is an opaque JSON document, identical in shape to what
# GET /apis/{id}/export returns. The provider does not model policies: a
# policy's configuration is an arbitrary object, so it is passed through
# untouched.
resource "gravitee_api" "billing" {
  definition = file("${path.module}/billing.json")

  # Calls POST /apis/{id}/deploy when the gateway is out of sync. Needed
  # because changing a plan does not bump the API's updated_at, so the
  # gateway never reloads on its own. Defaults to true.
  deploy = true

  # Deleting a plan deletes its subscriptions. With the default of false,
  # an apply that would drop a plan fails and lists what would be lost.
  allow_plan_deletion = false
}
