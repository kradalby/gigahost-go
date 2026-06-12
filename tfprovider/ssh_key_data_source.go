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
	_ datasource.DataSource              = (*sshKeyDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*sshKeyDataSource)(nil)
)

// sshKeyDataSource looks up one account SSH key by name, for referencing
// keys created outside Terraform in gigahost_server.ssh_keys.
type sshKeyDataSource struct {
	client *gigahost.Client
}

type sshKeyModel struct {
	Name      types.String `tfsdk:"name"`
	ID        types.String `tfsdk:"id"`
	PublicKey types.String `tfsdk:"public_key"`
	AddedAt   types.String `tfsdk:"added_at"`
}

// NewSSHKeyDataSource constructs the data source.
func NewSSHKeyDataSource() datasource.DataSource { return &sshKeyDataSource{} }

// Metadata sets the data source type name.
func (d *sshKeyDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ssh_key"
}

// Schema returns the Terraform schema.
func (d *sshKeyDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up one account SSH key by name — for wiring keys created " +
			"outside Terraform into `gigahost_server.ssh_keys` via `id`.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				MarkdownDescription: "Key name as shown by `gigahost account ssh-keys list`.",
				Required:            true,
			},
			"id": schema.StringAttribute{
				MarkdownDescription: "Key ID for `gigahost_server.ssh_keys`.",
				Computed:            true,
			},
			"public_key": schema.StringAttribute{Computed: true},
			"added_at":   schema.StringAttribute{Computed: true},
		},
	}
}

// Configure receives the shared Gigahost client.
func (d *sshKeyDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

// Read resolves the key by name against the account's stored keys.
func (d *sshKeyDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config sshKeyModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)

	if resp.Diagnostics.HasError() {
		return
	}

	account, err := d.client.Account.Get(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read account", err.Error())

		return
	}

	name := config.Name.ValueString()

	var found *gigahost.SSHKey

	for i := range account.SSHKeys {
		if account.SSHKeys[i].Name == name {
			if found != nil {
				resp.Diagnostics.AddError(
					"Ambiguous SSH key name",
					fmt.Sprintf("several keys are named %q; rename them or reference by resource", name),
				)

				return
			}

			found = &account.SSHKeys[i]
		}
	}

	if found == nil {
		names := make([]string, 0, len(account.SSHKeys))
		for _, k := range account.SSHKeys {
			names = append(names, k.Name)
		}

		resp.Diagnostics.AddError(
			"SSH key not found",
			fmt.Sprintf("no key named %q; known keys: %v", name, names),
		)

		return
	}

	config.ID = types.StringValue(found.ID)
	config.PublicKey = types.StringValue(found.Data)
	config.AddedAt = types.StringValue(found.AddedAt.UTC().Format(time.RFC3339))

	resp.Diagnostics.Append(resp.State.Set(ctx, config)...)
}
