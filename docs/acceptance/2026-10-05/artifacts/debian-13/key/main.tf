
provider "gigahost" {}
resource "gigahost_server" "test" {
  type     = "performance"
  size     = "2c-4gb-40gb"
  region   = "sfj"
  os       = "debian-13"
  hostname = "tf-acc-os-matrix-502725"
  ssh_keys = ["2952"]
}
