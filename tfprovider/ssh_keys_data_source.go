package tfprovider

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	gigahost "github.com/kradalby/gigahost-go/client"
)

var (
	_ datasource.DataSource              = (*sshKeysDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*sshKeysDataSource)(nil)
)

// sshKeysDataSource lists every SSH key on the account.
type sshKeysDataSource struct {
	client *gigahost.Client
}

type sshKeyEntryModel struct {
	ID        types.String `tfsdk:"id"`
	Name      types.String `tfsdk:"name"`
	PublicKey types.String `tfsdk:"public_key"`
	AddedAt   types.String `tfsdk:"added_at"`
}

type sshKeysDataSourceModel struct {
	Keys []sshKeyEntryModel `tfsdk:"keys"`
}

// NewSSHKeysDataSource constructs the data source.
func NewSSHKeysDataSource() datasource.DataSource { return &sshKeysDataSource{} }

// Metadata sets the data source type name.
func (d *sshKeysDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ssh_keys"
}

// Schema returns the Terraform schema.
func (d *sshKeysDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Returns all SSH keys on the account — for wiring keys created outside " +
			"Terraform into `gigahost_server.ssh_keys` via `id`.",
		Attributes: map[string]schema.Attribute{
			"keys": schema.ListNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":         schema.StringAttribute{Computed: true},
						"name":       schema.StringAttribute{Computed: true},
						"public_key": schema.StringAttribute{Computed: true},
						"added_at":   schema.StringAttribute{Computed: true},
					},
				},
			},
		},
	}
}

// Configure receives the shared Gigahost client.
func (d *sshKeysDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

// Read fetches all account SSH keys.
func (d *sshKeysDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	account, err := d.client.Account.Get(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read account", err.Error())

		return
	}

	out := sshKeysDataSourceModel{Keys: make([]sshKeyEntryModel, 0, len(account.SSHKeys))}

	for i := range account.SSHKeys {
		k := account.SSHKeys[i]
		out.Keys = append(out.Keys, sshKeyEntryModel{
			ID:        types.StringValue(k.ID),
			Name:      types.StringValue(k.Name),
			PublicKey: types.StringValue(k.Data),
			AddedAt:   types.StringValue(k.AddedAt.UTC().Format(time.RFC3339)),
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, out)...)
}
