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
	_ datasource.DataSource              = (*operatingSystemsDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*operatingSystemsDataSource)(nil)
)

// operatingSystemsDataSource lists the operating systems available for
// gigahost_server.os (deploy and in-place reinstall) as one flat list.
type operatingSystemsDataSource struct {
	client *gigahost.Client
}

type operatingSystemModel struct {
	Slug          types.String `tfsdk:"slug"`
	Name          types.String `tfsdk:"name"`
	Codename      types.String `tfsdk:"codename"`
	Distribution  types.String `tfsdk:"distribution"`
	Arch          types.String `tfsdk:"arch"`
	MinRAMGB      types.Int64  `tfsdk:"min_ram_gb"`
	DedicatedOnly types.Bool   `tfsdk:"dedicated_only"`
}

type operatingSystemsModel struct {
	Distribution     types.String           `tfsdk:"distribution"`
	OperatingSystems []operatingSystemModel `tfsdk:"operating_systems"`
}

// NewOperatingSystemsDataSource constructs the data source.
func NewOperatingSystemsDataSource() datasource.DataSource { return &operatingSystemsDataSource{} }

// Metadata sets the data source type name.
func (d *operatingSystemsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_operating_systems"
}

// Schema returns the Terraform schema.
func (d *operatingSystemsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists the operating systems available for server deploy and " +
			"in-place reinstall as one flat list. Use the `slug` values for " +
			"`gigahost_server.os`. Same data as `gigahost deploy os`. For picking exactly one " +
			"OS, prefer `gigahost_operating_system`.",
		Attributes: map[string]schema.Attribute{
			"distribution": schema.StringAttribute{
				MarkdownDescription: "Only OSes of distributions whose name contains this " +
					"string (case-insensitive), e.g. `\"debian\"`.",
				Optional: true,
			},
			"operating_systems": schema.ListNestedAttribute{
				MarkdownDescription: "Installable operating systems.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"slug": schema.StringAttribute{
							MarkdownDescription: "OS slug for `gigahost_server.os`, e.g. `debian-12`.",
							Computed:            true,
						},
						"name":     schema.StringAttribute{Computed: true},
						"codename": schema.StringAttribute{Computed: true},
						"distribution": schema.StringAttribute{
							MarkdownDescription: "Parent distribution, e.g. `debian`.",
							Computed:            true,
						},
						"arch": schema.StringAttribute{Computed: true},
						"min_ram_gb": schema.Int64Attribute{
							MarkdownDescription: "Minimum RAM required, in GB.",
							Computed:            true,
						},
						"dedicated_only": schema.BoolAttribute{
							MarkdownDescription: "Only installable on dedicated servers.",
							Computed:            true,
						},
					},
				},
			},
		},
	}
}

// Configure receives the shared Gigahost client.
func (d *operatingSystemsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

// Read fetches all operating systems across distributions.
func (d *operatingSystemsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config operatingSystemsModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)

	if resp.Diagnostics.HasError() {
		return
	}

	all, err := d.client.Reinstall.ListAllOperatingSystems(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to list operating systems", err.Error())

		return
	}

	filter := strings.ToLower(config.Distribution.ValueString())

	out := operatingSystemsModel{
		Distribution:     config.Distribution,
		OperatingSystems: []operatingSystemModel{},
	}

	for _, o := range all {
		if filter != "" &&
			!strings.Contains(strings.ToLower(o.Distribution.Name), filter) &&
			!strings.Contains(strings.ToLower(o.Distribution.Value), filter) {
			continue
		}

		out.OperatingSystems = append(out.OperatingSystems, operatingSystemModel{
			Slug:          types.StringValue(o.Slug),
			Name:          types.StringValue(o.OS.Name),
			Codename:      types.StringValue(o.OS.Distribution),
			Distribution:  types.StringValue(strings.ToLower(o.Distribution.Value)),
			Arch:          types.StringValue(o.OS.Arch),
			MinRAMGB:      types.Int64Value(int64(o.OS.MinRAM)),
			DedicatedOnly: types.BoolValue(o.OS.DedicatedOnly),
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, out)...)
}
