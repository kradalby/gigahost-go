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
	_ datasource.DataSource              = (*accountDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*accountDataSource)(nil)
)

type accountDataSource struct {
	client *gigahost.Client
}

type accountSSHKeyModel struct {
	ID      types.String `tfsdk:"id"`
	Name    types.String `tfsdk:"name"`
	AddedAt types.Int64  `tfsdk:"added_at"`
}

type accountContactModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Username    types.String `tfsdk:"username"`
	AccessLevel types.String `tfsdk:"access_level"`
	TwoFA       types.Bool   `tfsdk:"two_fa"`
}

type accountModel struct {
	ID           types.String `tfsdk:"id"`
	Name         types.String `tfsdk:"name"`
	Email        types.String `tfsdk:"email"`
	BillingEmail types.String `tfsdk:"billing_email"`
	CompanyNo    types.String `tfsdk:"company_no"`
	Address      types.String `tfsdk:"address"`
	Address2     types.String `tfsdk:"address2"`
	Province     types.String `tfsdk:"province"`
	Zip          types.String `tfsdk:"zip"`
	City         types.String `tfsdk:"city"`
	Country      types.String `tfsdk:"country"`
	Phone        types.String `tfsdk:"phone"`
	IsPartner    types.Bool   `tfsdk:"is_partner"`

	// Notification preferences.
	Newsletter            types.Bool `tfsdk:"newsletter"`
	Incident              types.Bool `tfsdk:"incident"`
	BandwidthNotification types.Bool `tfsdk:"bandwidth_notification"`
	EmailOnLogin          types.Bool `tfsdk:"email_on_login"`
	NotifyServiceRenewal  types.Bool `tfsdk:"notify_service_renewal"`

	SSHKeys  []accountSSHKeyModel  `tfsdk:"ssh_keys"`
	Contacts []accountContactModel `tfsdk:"contacts"`
}

// NewAccountDataSource constructs the data source.
func NewAccountDataSource() datasource.DataSource { return &accountDataSource{} }

// Metadata sets the data source type name.
func (d *accountDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_account"
}

// Schema returns the Terraform schema.
func (d *accountDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Returns the authenticated account's profile.",
		Attributes: map[string]schema.Attribute{
			"id":            schema.StringAttribute{Computed: true},
			"name":          schema.StringAttribute{Computed: true},
			"email":         schema.StringAttribute{Computed: true},
			"billing_email": schema.StringAttribute{Computed: true},
			"company_no":    schema.StringAttribute{Computed: true},
			"address":       schema.StringAttribute{Computed: true},
			"address2":      schema.StringAttribute{Computed: true},
			"province":      schema.StringAttribute{Computed: true},
			"zip":           schema.StringAttribute{Computed: true},
			"city":          schema.StringAttribute{Computed: true},
			"country":       schema.StringAttribute{Computed: true},
			"phone":         schema.StringAttribute{Computed: true},
			"is_partner":    schema.BoolAttribute{Computed: true},

			"newsletter":             schema.BoolAttribute{Computed: true},
			"incident":               schema.BoolAttribute{Computed: true},
			"bandwidth_notification": schema.BoolAttribute{Computed: true},
			"email_on_login":         schema.BoolAttribute{Computed: true},
			"notify_service_renewal": schema.BoolAttribute{Computed: true},

			"ssh_keys": schema.ListNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":       schema.StringAttribute{Computed: true},
						"name":     schema.StringAttribute{Computed: true},
						"added_at": schema.Int64Attribute{Computed: true},
					},
				},
			},
			"contacts": schema.ListNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":           schema.StringAttribute{Computed: true},
						"name":         schema.StringAttribute{Computed: true},
						"username":     schema.StringAttribute{Computed: true},
						"access_level": schema.StringAttribute{Computed: true},
						"two_fa":       schema.BoolAttribute{Computed: true},
					},
				},
			},
		},
	}
}

// Configure receives the shared Gigahost client.
func (d *accountDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

// Read fetches the account.
func (d *accountDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	acc, err := d.client.Account.Get(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read account", err.Error())

		return
	}

	out := accountModel{
		ID:                    types.StringValue(acc.CustomerID),
		Name:                  types.StringValue(acc.Name),
		Email:                 types.StringValue(acc.Email),
		BillingEmail:          types.StringValue(acc.BillingEmail),
		CompanyNo:             types.StringValue(acc.CompanyNo),
		Address:               types.StringValue(acc.Address),
		Address2:              types.StringValue(acc.Address2),
		Province:              types.StringValue(acc.Province),
		Zip:                   types.StringValue(acc.ZipCode),
		City:                  types.StringValue(acc.City),
		Country:               types.StringValue(acc.Country),
		Phone:                 types.StringValue(acc.Phone),
		IsPartner:             types.BoolValue(acc.IsPartner),
		Newsletter:            types.BoolValue(acc.Newsletter),
		Incident:              types.BoolValue(acc.Incident),
		BandwidthNotification: types.BoolValue(acc.BandwidthNotification),
		EmailOnLogin:          types.BoolValue(acc.EmailOnLogin),
		NotifyServiceRenewal:  types.BoolValue(acc.NotifyServiceRenewal),
		SSHKeys:               make([]accountSSHKeyModel, 0, len(acc.SSHKeys)),
		Contacts:              make([]accountContactModel, 0, len(acc.Contacts)),
	}

	for _, k := range acc.SSHKeys {
		out.SSHKeys = append(out.SSHKeys, accountSSHKeyModel{
			ID:      types.StringValue(k.ID),
			Name:    types.StringValue(k.Name),
			AddedAt: types.Int64Value(k.AddedAt.Unix()),
		})
	}

	for _, c := range acc.Contacts {
		out.Contacts = append(out.Contacts, accountContactModel{
			ID:          types.StringValue(c.ID),
			Name:        types.StringValue(c.Name),
			Username:    types.StringValue(c.Username),
			AccessLevel: types.StringValue(c.AccessLevel),
			TwoFA:       types.BoolValue(c.TwoFA),
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, out)...)
}
