
provider "gigahost" {}
resource "gigahost_server" "test" {
  type     = "performance"
  size     = "2c-4gb-40gb"
  region   = "sfj"
  os       = "ubuntu-24.04"
  hostname = "tf-acc-os-matrix-390005"
  ssh_keys = []
  timeouts = { create = "45m" }
}
