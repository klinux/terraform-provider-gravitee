terraform {
  required_providers {
    gravitee = {
      source  = "klinux/gravitee"
      version = "~> 0.1"
    }
  }
}

provider "gravitee" {
  # Base of the Management API, including the /management path.
  endpoint = "https://apim.example.com/management"

  # Prefer the GRAVITEE_TOKEN environment variable over putting the
  # token in your configuration.
  # token = "..."

  # Both default to DEFAULT.
  organization = "DEFAULT"
  environment  = "DEFAULT"
}
