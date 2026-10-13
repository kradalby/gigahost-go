
provider "gigahost" {}
resource "gigahost_server" "test" {
  type     = "performance"
  size     = "2c-4gb-40gb"
  region   = "sfj"
  os       = "fedora-43-server"
  hostname = "tf-acc-os-matrix-290105"
  ssh_keys = []
  timeouts = { create = "45m" }
}
