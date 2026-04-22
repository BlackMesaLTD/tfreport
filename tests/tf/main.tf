terraform {
  required_providers {
    null = {
      source  = "hashicorp/null"
      version = "~> 3.2"
    }
  }
}

resource "null_resource" "tfreport_smoke" {
  triggers = {
    fixture = "tfreport-ci-smoke"
  }
}
