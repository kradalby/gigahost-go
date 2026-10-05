
provider "gigahost" {}
resource "gigahost_server" "test" {
  type     = "performance"
  size     = "2c-4gb-40gb"
  region   = "sfj"
  os       = "debian-12"
  hostname = "tf-acc-os-matrix-926727"
  ssh_keys = []
}
