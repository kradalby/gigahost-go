module github.com/kradalby/terraform-provider-gigahost

go 1.26

require (
	github.com/hashicorp/terraform-plugin-framework v1.19.0
	github.com/kradalby/gigahost-go v0.0.0-00010101000000-000000000000
)

require (
	github.com/fatih/color v1.18.0 // indirect
	github.com/go-json-experiment/json v0.0.0-20260214004413-d219187c3433 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/hashicorp/go-hclog v1.6.3 // indirect
	github.com/hashicorp/go-plugin v1.7.0 // indirect
	github.com/hashicorp/go-uuid v1.0.3 // indirect
	github.com/hashicorp/terraform-plugin-framework-timeouts v0.7.0 // indirect
	github.com/hashicorp/terraform-plugin-framework-validators v0.19.0 // indirect
	github.com/hashicorp/terraform-plugin-go v0.31.0 // indirect
	github.com/hashicorp/terraform-plugin-log v0.10.0 // indirect
	github.com/hashicorp/terraform-registry-address v0.4.0 // indirect
	github.com/hashicorp/terraform-svchost v0.2.1 // indirect
	github.com/hashicorp/yamux v0.1.2 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mitchellh/go-testing-interface v1.14.1 // indirect
	github.com/oklog/run v1.2.0 // indirect
	github.com/stretchr/testify v1.11.1 // indirect
	github.com/vmihailenco/msgpack/v5 v5.4.1 // indirect
	github.com/vmihailenco/tagparser/v2 v2.0.0 // indirect
	golang.org/x/net v0.55.0 // indirect
	golang.org/x/sys v0.46.0 // indirect
	golang.org/x/text v0.38.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20251202230838-ff82c1b0f217 // indirect
	google.golang.org/grpc v1.79.3 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

// While both modules live in this repository they are developed
// together via the top-level go.work. The replace directive below
// makes the shim buildable even outside the workspace (for example in
// CI or when running `go build` directly in this subdirectory). When
// this directory is copied out to its own public repository, remove
// the replace directive and Go's module resolution will use the
// published tag of gigahost-go.
replace github.com/kradalby/gigahost-go => ../
