package tfprovider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	gigahost "github.com/kradalby/gigahost-go/client"
)

var (
	_ datasource.DataSource              = (*operatingSystemDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*operatingSystemDataSource)(nil)
)

// operatingSystemDataSource looks up exactly one installable operating
// system by slug or distribution+release.
type operatingSystemDataSource struct {
	client *gigahost.Client
}

type operatingSystemSingularModel struct {
	Slug         types.String `tfsdk:"slug"`
	Distribution types.String `tfsdk:"distribution"`
	Release      types.String `tfsdk:"release"`

	Name            types.String `tfsdk:"name"`
	Codename        types.String `tfsdk:"codename"`
	Arch            types.String `tfsdk:"arch"`
	MinRAMGB        types.Int64  `tfsdk:"min_ram_gb"`
	DedicatedOnly   types.Bool   `tfsdk:"dedicated_only"`
	CustomPartition types.Bool   `tfsdk:"custom_partition"`
}

// NewOperatingSystemDataSource constructs the data source.
func NewOperatingSystemDataSource() datasource.DataSource { return &operatingSystemDataSource{} }

// Metadata sets the data source type name.
func (d *operatingSystemDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_operating_system"
}

// Schema returns the Terraform schema.
func (d *operatingSystemDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up exactly one installable operating system. Give either " +
			"`slug` (e.g. `debian-12`, also accepts the codename `bookworm` or the full display " +
			"name), or `distribution` + `release`. The lookup errors when zero or several OSes " +
			"match, listing the valid slugs. List OSes with `gigahost deploy os`.",
		Attributes: map[string]schema.Attribute{
			"slug": schema.StringAttribute{
				MarkdownDescription: "OS slug for `gigahost_server.os`, e.g. `debian-12`. " +
					"Filled with the canonical slug when resolved via `distribution`.",
				Optional: true,
				Computed: true,
			},
			"distribution": schema.StringAttribute{
				MarkdownDescription: "Distribution to search, e.g. `debian`. " +
					"Filled with the resolved distribution.",
				Optional: true,
				Computed: true,
			},
			"release": schema.StringAttribute{
				MarkdownDescription: "Release within the distribution, e.g. `12` or `24.04`.",
				Optional:            true,
			},
			"name":             schema.StringAttribute{MarkdownDescription: "Operating system name, e.g. `Debian 12 64-bit`.", Computed: true},
			"codename":         schema.StringAttribute{MarkdownDescription: "Distribution codename, e.g. `bookworm`.", Computed: true},
			"arch":             schema.StringAttribute{MarkdownDescription: "CPU architecture the image targets.", Computed: true},
			"min_ram_gb":       schema.Int64Attribute{MarkdownDescription: "Minimum memory in GB this image requires.", Computed: true},
			"dedicated_only":   schema.BoolAttribute{MarkdownDescription: "True when the image can only be installed on dedicated hardware.", Computed: true},
			"custom_partition": schema.BoolAttribute{MarkdownDescription: "True when the image supports custom partitioning.", Computed: true},
		},
	}
}

// Configure receives the shared Gigahost client.
func (d *operatingSystemDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*gigahost.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("got %T", req.ProviderData))

		return
	}

	d.client = client
}

// Read resolves the OS against the live distribution list.
func (d *operatingSystemDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config operatingSystemSingularModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)

	if resp.Diagnostics.HasError() {
		return
	}

	input := config.Slug.ValueString()
	if input == "" {
		dist := config.Distribution.ValueString()
		if dist == "" {
			resp.Diagnostics.AddError(
				"Missing OS selector",
				"set either slug (e.g. \"debian-12\") or distribution (+ release)",
			)

			return
		}

		input = dist
		if rel := config.Release.ValueString(); rel != "" {
			input = dist + "-" + rel
		}
	}

	os, err := d.client.Reinstall.ResolveOS(ctx, input)
	if err != nil {
		resp.Diagnostics.AddError("No matching operating system", err.Error())

		return
	}

	config.Slug = types.StringValue(os.Slug)
	config.Distribution = types.StringValue(strings.ToLower(os.Distribution.Value))
	config.Name = types.StringValue(os.OS.Name)
	config.Codename = types.StringValue(os.OS.Distribution)
	config.Arch = types.StringValue(os.OS.Arch)
	config.MinRAMGB = types.Int64Value(int64(os.OS.MinRAM))
	config.DedicatedOnly = types.BoolValue(os.OS.DedicatedOnly)
	config.CustomPartition = types.BoolValue(os.OS.CustomPartition)

	resp.Diagnostics.Append(resp.State.Set(ctx, config)...)
}
