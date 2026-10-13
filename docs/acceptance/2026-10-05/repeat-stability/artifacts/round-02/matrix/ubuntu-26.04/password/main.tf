
provider "gigahost" {}
resource "gigahost_server" "test" {
  type     = "performance"
  size     = "2c-4gb-40gb"
  region   = "sfj"
  os       = "ubuntu-26.04"
  hostname = "tf-acc-os-matrix-647663"
  ssh_keys = []
  timeouts = { create = "45m" }
}
