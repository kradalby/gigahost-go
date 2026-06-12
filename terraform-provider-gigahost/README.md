# Gigahost Provider for OpenTofu and Terraform

[![License: MPL-2.0](https://img.shields.io/badge/License-MPL_2.0-brightgreen.svg)](https://opensource.org/licenses/MPL-2.0)

Manage [gigahost.no](https://gigahost.no/en/api-dokumentasjon) resources
with [OpenTofu](https://opentofu.org) (primary) or
[Terraform](https://www.terraform.io) (supported).

Both implementations speak the same plugin protocol, so the same
provider binary works unmodified with either CLI.

## Installation

```hcl
terraform {
  required_providers {
    gigahost = {
      source  = "kradalby/gigahost"
      version = "~> 0.0.1"
    }
  }
}
```

The provider is published to both registries:

- **OpenTofu Registry**: `kradalby/gigahost`
  (https://search.opentofu.org/providers/kradalby/gigahost)
- **Terraform Registry**: `registry.terraform.io/kradalby/gigahost`

## Configuration

```hcl
provider "gigahost" {
  token = var.gigahost_token
  # Or use username + password:
  # username = var.gigahost_username
  # password = var.gigahost_password
}
```

Credentials may also be set via environment variables:
`GIGAHOST_TOKEN`, `GIGAHOST_USERNAME`, `GIGAHOST_PASSWORD`,
`GIGAHOST_BASE_URL`.

## Examples

See [examples/](./examples/) for working OpenTofu / Terraform
configurations.

A minimal example:

```hcl
resource "gigahost_dns_zone" "example" {
  name = "example.no"
}

resource "gigahost_dns_record" "www" {
  zone_id = gigahost_dns_zone.example.id
  name    = "www"
  type    = "A"
  value   = "185.125.168.166"
  ttl     = 3600
}

resource "gigahost_dns_redirect" "root" {
  zone_id    = gigahost_dns_zone.example.id
  source     = "@"
  target_url = "https://www.example.com"
}
```

## Development

This provider is a thin shim over
[`github.com/kradalby/gigahost-go/tfprovider`](https://github.com/kradalby/gigahost-go/tree/main/tfprovider),
where all resources and data sources are implemented.

The canonical development happens in the `gigahost-go` repository; this
one exists primarily to satisfy the Terraform Registry's expectation of
a dedicated `terraform-provider-*` repository (the OpenTofu Registry
follows the same convention).

### Running locally

```console
$ nix build .#terraform-provider-gigahost   # from the gigahost-go root
$ cd examples/resources/gigahost_dns_zone
$ tofu init
$ tofu plan
```

## License

MPL-2.0. See [LICENSE](./LICENSE).
