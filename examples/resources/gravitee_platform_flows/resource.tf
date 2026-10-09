# Platform flows run for EVERY API this gateway serves. A mistake here is not
# scoped to one API.
#
# There is one set per organization, so this resource adopts what is already
# configured instead of creating anything:
#
#   terraform import gravitee_platform_flows.p DEFAULT
#
resource "gravitee_platform_flows" "p" {
  flows = file("${path.module}/platform-flows.json")

  # DEFAULT runs every matching flow, in order; BEST_MATCH runs only the
  # closest one. Left as it stands on the server when not declared.
  flow_mode = "DEFAULT"

  # Destroy leaves the gateway untouched by default and only drops the
  # resource from state. Set this to true if you really want destroy to empty
  # every platform flow.
  clear_on_destroy = false
}
