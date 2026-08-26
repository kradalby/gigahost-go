package tfprovider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	gigahost "github.com/kradalby/gigahost-go/client"
)

var (
	_ datasource.DataSource              = (*isosDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*isosDataSource)(nil)
)

// isosDataSource lists the uploaded ISOs available for deployment.
type isosDataSource struct {
	client *gigahost.Client
}

type isoModel struct {
	ID        types.String `tfsdk:"id"`
	Name      types.String `tfsdk:"name"`
	SizeBytes types.Int64  `tfsdk:"size_bytes"`
}

type isosModel struct {
	ISOs []isoModel `tfsdk:"isos"`
}

// NewISOsDataSource constructs the data source.
func NewISOsDataSource() datasource.DataSource { return &isosDataSource{} }

// Metadata sets the data source type name.
func (d *isosDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_isos"
}

// Schema returns the Terraform schema.
func (d *isosDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists the account's uploaded ISOs available for deployment. " +
			"Use a `name` for `gigahost_server.iso`. Same data as `gigahost deploy isos`.",
		Attributes: map[string]schema.Attribute{
			"isos": schema.ListNestedAttribute{
				MarkdownDescription: "ISO images available to mount.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{MarkdownDescription: "Unique identifier.", Computed: true},
						"name": schema.StringAttribute{
							MarkdownDescription: "ISO name for `gigahost_server.iso`.",
							Computed:            true,
						},
						"size_bytes": schema.Int64Attribute{MarkdownDescription: "Image size in bytes.", Computed: true},
					},
				},
			},
		},
	}
}

// Configure receives the shared Gigahost client.
func (d *isosDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

// Read lists the uploaded ISOs.
func (d *isosDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	isos, err := d.client.Deploy.ListISOs(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to list ISOs", err.Error())

		return
	}

	out := isosModel{ISOs: make([]isoModel, 0, len(isos))}

	for _, iso := range isos {
		out.ISOs = append(out.ISOs, isoModel{
			ID:        types.StringValue(iso.ID),
			Name:      types.StringValue(iso.Name),
			SizeBytes: types.Int64Value(iso.Size),
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, out)...)
}
