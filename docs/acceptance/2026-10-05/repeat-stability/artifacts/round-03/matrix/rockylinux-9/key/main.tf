
provider "gigahost" {}
resource "gigahost_server" "test" {
  type     = "performance"
  size     = "2c-4gb-40gb"
  region   = "sfj"
  os       = "rockylinux-9"
  hostname = "tf-acc-os-matrix-293003"
  ssh_keys = ["2954"]
  timeouts = { create = "45m" }
}
