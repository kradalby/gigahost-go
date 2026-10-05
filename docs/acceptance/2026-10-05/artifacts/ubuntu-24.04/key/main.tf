
provider "gigahost" {}
resource "gigahost_server" "test" {
  type     = "performance"
  size     = "2c-4gb-40gb"
  region   = "sfj"
  os       = "ubuntu-24.04"
  hostname = "tf-acc-os-matrix-270105"
  ssh_keys = ["2952"]
}
