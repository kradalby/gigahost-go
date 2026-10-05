
provider "gigahost" {}
resource "gigahost_server" "test" {
  type     = "performance"
  size     = "2c-4gb-40gb"
  region   = "sfj"
  os       = "almalinux-10"
  hostname = "tf-acc-os-matrix-904376"
  ssh_keys = []
}
