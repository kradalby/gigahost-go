
provider "gigahost" {}
resource "gigahost_server" "test" {
  type     = "performance"
  size     = "2c-4gb-40gb"
  region   = "sfj"
  os       = "debian-13"
  hostname = "tf-acc-os-matrix-763156"
  ssh_keys = ["2954"]
  timeouts = { create = "45m" }
}
