resource "gravitee_application" "partner" {
  name        = "partner-app"
  description = "Client used by an external partner"

  # Must match the client_id of the Keycloak client when the API's OAuth2
  # plan runs with modeStrict enabled.
  client_id = "partner-app"

  groups = ["00000000-0000-0000-0000-000000000000"]
}
