// Copyright (c) Risq Research Ltd.
// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/glue"
	gluetypes "github.com/aws/aws-sdk-go-v2/service/glue/types"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &GlueCatalogResource{}

func NewGlueCatalogResource() resource.Resource {
	return &GlueCatalogResource{}
}

// GlueCatalogResource defines the resource implementation.
type GlueCatalogResource struct {
	config aws.Config
}

// GlueCatalogResourceModel describes the resource data model.
type GlueCatalogResourceModel struct {
	Name                             types.String `tfsdk:"name"`
	Description                      types.String `tfsdk:"description"`
	CatalogArn                       types.String `tfsdk:"catalog_arn"`
	Tags                             types.Map    `tfsdk:"tags"`
	Parameters                       types.Map    `tfsdk:"parameters"`
	FederatedCatalog                 types.Object `tfsdk:"federated_catalog"`
	TargetRedshiftCatalog            types.Object `tfsdk:"target_redshift_catalog"`
	AllowFullTableExternalDataAccess types.Bool   `tfsdk:"allow_full_table_external_data_access"`
	CreateDatabaseDefaultPermissions types.List   `tfsdk:"create_database_default_permissions"`
	CreateTableDefaultPermissions    types.List   `tfsdk:"create_table_default_permissions"`
	CatalogProperties                types.Object `tfsdk:"catalog_properties"`
	Region                           types.String `tfsdk:"region"`
	ResourceArn                      types.String `tfsdk:"resource_arn"`
}

// FederatedCatalogModel describes the federated catalog nested object.
type FederatedCatalogModel struct {
	ConnectionName types.String `tfsdk:"connection_name"`
	ConnectionType types.String `tfsdk:"connection_type"`
	Identifier     types.String `tfsdk:"identifier"`
}

// TargetRedshiftCatalogModel describes the target redshift catalog nested object.
type TargetRedshiftCatalogModel struct {
	CatalogArn types.String `tfsdk:"catalog_arn"`
}

// PrincipalPermissionsModel describes the principal permissions nested object.
type PrincipalPermissionsModel struct {
	Principal   types.Object `tfsdk:"principal"`
	Permissions types.List   `tfsdk:"permissions"`
}

// DataLakePrincipalModel describes the data lake principal nested object.
type DataLakePrincipalModel struct {
	DataLakePrincipalIdentifier types.String `tfsdk:"data_lake_principal_identifier"`
}

// CatalogPropertiesModel describes the catalog properties nested object.
type CatalogPropertiesModel struct {
	CustomProperties types.Map `tfsdk:"custom_properties"`
}

func (r *GlueCatalogResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_glue_catalog"
}

func (r *GlueCatalogResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "AWS Glue Catalog Resource",

		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				MarkdownDescription: "The name of the catalog to create.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"catalog_arn": schema.StringAttribute{
				MarkdownDescription: "The ARN of the Redshift catalog.",
				Optional:            true,
			},
			"tags": schema.MapAttribute{
				MarkdownDescription: "A map of tags to assign to the catalog.",
				Optional:            true,
				ElementType:         types.StringType,
			},
			"parameters": schema.MapAttribute{
				MarkdownDescription: "Parameters for the catalog.",
				Optional:            true,
				ElementType:         types.StringType,
			},
			"federated_catalog": schema.SingleNestedAttribute{
				MarkdownDescription: "A FederatedCatalog object that references the data source.",
				Optional:            true,
				Attributes: map[string]schema.Attribute{
					"connection_name": schema.StringAttribute{
						MarkdownDescription: "The name of the connection to the external data source.",
						Optional:            true,
					},
					"connection_type": schema.StringAttribute{
						MarkdownDescription: "The type of connection to the external data source.",
						Optional:            true,
					},
					"identifier": schema.StringAttribute{
						MarkdownDescription: "A unique identifier for the federated catalog.",
						Optional:            true,
					},
				},
			},
			"target_redshift_catalog": schema.SingleNestedAttribute{
				MarkdownDescription: "A TargetRedshiftCatalog object that references a Redshift database.",
				Optional:            true,
				Attributes: map[string]schema.Attribute{
					"catalog_arn": schema.StringAttribute{
						MarkdownDescription: "The Amazon Resource Name (ARN) of the Redshift catalog.",
						Required:            true,
					},
				},
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "A description of the catalog.",
				Optional:            true,
			},
			"allow_full_table_external_data_access": schema.BoolAttribute{
				MarkdownDescription: "Allows third-party engines to access data in Amazon S3 locations that are registered with Lake Formation. Valid values: `true` or `false`.",
				Optional:            true,
			},
			"create_database_default_permissions": schema.ListNestedAttribute{
				MarkdownDescription: "An array of PrincipalPermissions objects. Creates a set of default permissions on the database(s) for principals. Used by AWS Lake Formation. Typically should be explicitly set as an empty list.",
				Optional:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"principal": schema.SingleNestedAttribute{
							MarkdownDescription: "The principal who is granted permissions.",
							Optional:            true,
							Attributes: map[string]schema.Attribute{
								"data_lake_principal_identifier": schema.StringAttribute{
									MarkdownDescription: "An identifier for the AWS Lake Formation principal.",
									Optional:            true,
								},
							},
						},
						"permissions": schema.ListAttribute{
							MarkdownDescription: "The permissions that are granted to the principal.",
							Optional:            true,
							ElementType:         types.StringType,
						},
					},
				},
			},
			"create_table_default_permissions": schema.ListNestedAttribute{
				MarkdownDescription: "An array of PrincipalPermissions objects. Creates a set of default permissions on the table(s) for principals. Used by AWS Lake Formation. Typically should be explicitly set as an empty list.",
				Optional:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"principal": schema.SingleNestedAttribute{
							MarkdownDescription: "The principal who is granted permissions.",
							Optional:            true,
							Attributes: map[string]schema.Attribute{
								"data_lake_principal_identifier": schema.StringAttribute{
									MarkdownDescription: "An identifier for the AWS Lake Formation principal.",
									Optional:            true,
								},
							},
						},
						"permissions": schema.ListAttribute{
							MarkdownDescription: "The permissions that are granted to the principal.",
							Optional:            true,
							ElementType:         types.StringType,
						},
					},
				},
			},
			"catalog_properties": schema.SingleNestedAttribute{
				MarkdownDescription: "A structure that specifies data lake access properties and other custom properties.",
				Optional:            true,
				Attributes: map[string]schema.Attribute{
					"custom_properties": schema.MapAttribute{
						MarkdownDescription: "Additional key-value properties for the catalog, such as column statistics optimizations.",
						Optional:            true,
						ElementType:         types.StringType,
					},
				},
			},
			"region": schema.StringAttribute{
				MarkdownDescription: "The AWS region where the catalog will be created. If not specified, the provider's default region will be used.",
				Optional:            true,
			},
			"resource_arn": schema.StringAttribute{
				MarkdownDescription: "The ARN of the catalog.",
				Computed:            true,
			},
		},
	}
}

func (r *GlueCatalogResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	// Prevent panic if the provider has not been configured.
	if req.ProviderData == nil {
		return
	}

	config, ok := req.ProviderData.(aws.Config)

	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *aws.Config, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)

		return
	}

	// Store the config for later use
	r.config = config
}

// getGlueClient returns a Glue client configured for the specified region.
// If region is empty, it uses the default region from the config.
func (r *GlueCatalogResource) getGlueClient(region string) *glue.Client {
	if region != "" {
		// Create a new config with the specified region
		cfg := r.config.Copy()
		cfg.Region = region
		return glue.NewFromConfig(cfg)
	}
	return glue.NewFromConfig(r.config)
}

// buildCatalogInput constructs a CatalogInput from the resource model.
func (r *GlueCatalogResource) buildCatalogInput(ctx context.Context, data *GlueCatalogResourceModel, diagnostics *diag.Diagnostics) *gluetypes.CatalogInput {
	catalogInput := &gluetypes.CatalogInput{}

	// Handle TargetRedshiftCatalog
	if !data.TargetRedshiftCatalog.IsNull() {
		var targetRedshift TargetRedshiftCatalogModel
		diagnostics.Append(data.TargetRedshiftCatalog.As(ctx, &targetRedshift, basetypes.ObjectAsOptions{})...)
		if diagnostics.HasError() {
			return nil
		}
		catalogInput.TargetRedshiftCatalog = &gluetypes.TargetRedshiftCatalog{
			CatalogArn: targetRedshift.CatalogArn.ValueStringPointer(),
		}
	}

	// Handle FederatedCatalog
	if !data.FederatedCatalog.IsNull() {
		var federated FederatedCatalogModel
		diagnostics.Append(data.FederatedCatalog.As(ctx, &federated, basetypes.ObjectAsOptions{})...)
		if diagnostics.HasError() {
			return nil
		}
		catalogInput.FederatedCatalog = &gluetypes.FederatedCatalog{
			ConnectionName: federated.ConnectionName.ValueStringPointer(),
			ConnectionType: federated.ConnectionType.ValueStringPointer(),
			Identifier:     federated.Identifier.ValueStringPointer(),
		}
	}

	// Handle Parameters
	if !data.Parameters.IsNull() {
		parameters := make(map[string]string)
		diagnostics.Append(data.Parameters.ElementsAs(ctx, &parameters, false)...)
		if diagnostics.HasError() {
			return nil
		}
		catalogInput.Parameters = parameters
	}

	// Handle Description
	if !data.Description.IsNull() {
		catalogInput.Description = data.Description.ValueStringPointer()
	}

	// Handle AllowFullTableExternalDataAccess
	if !data.AllowFullTableExternalDataAccess.IsNull() {
		access := gluetypes.AllowFullTableExternalDataAccessEnumFalse
		if data.AllowFullTableExternalDataAccess.ValueBool() {
			access = gluetypes.AllowFullTableExternalDataAccessEnumTrue
		}
		catalogInput.AllowFullTableExternalDataAccess = access
	}

	// Handle CreateDatabaseDefaultPermissions
	if !data.CreateDatabaseDefaultPermissions.IsNull() {
		var dbPermissions []PrincipalPermissionsModel
		diagnostics.Append(data.CreateDatabaseDefaultPermissions.ElementsAs(ctx, &dbPermissions, false)...)
		if diagnostics.HasError() {
			return nil
		}
		catalogInput.CreateDatabaseDefaultPermissions = convertPrincipalPermissionsToSDK(ctx, dbPermissions, diagnostics)
		if diagnostics.HasError() {
			return nil
		}
	}

	// Handle CreateTableDefaultPermissions
	if !data.CreateTableDefaultPermissions.IsNull() {
		var tablePermissions []PrincipalPermissionsModel
		diagnostics.Append(data.CreateTableDefaultPermissions.ElementsAs(ctx, &tablePermissions, false)...)
		if diagnostics.HasError() {
			return nil
		}
		catalogInput.CreateTableDefaultPermissions = convertPrincipalPermissionsToSDK(ctx, tablePermissions, diagnostics)
		if diagnostics.HasError() {
			return nil
		}
	}

	// Handle CatalogProperties
	if !data.CatalogProperties.IsNull() {
		var catalogProps CatalogPropertiesModel
		diagnostics.Append(data.CatalogProperties.As(ctx, &catalogProps, basetypes.ObjectAsOptions{})...)
		if diagnostics.HasError() {
			return nil
		}
		if !catalogProps.CustomProperties.IsNull() {
			customProps := make(map[string]string)
			diagnostics.Append(catalogProps.CustomProperties.ElementsAs(ctx, &customProps, false)...)
			if diagnostics.HasError() {
				return nil
			}
			catalogInput.CatalogProperties = &gluetypes.CatalogProperties{
				CustomProperties: customProps,
			}
		}
	}

	return catalogInput
}

func (r *GlueCatalogResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data GlueCatalogResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Build the CatalogInput
	catalogInput := r.buildCatalogInput(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	// Prepare tags
	var tags map[string]string
	if !data.Tags.IsNull() {
		resp.Diagnostics.Append(data.Tags.ElementsAs(ctx, &tags, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	// Create the catalog
	createInput := &glue.CreateCatalogInput{
		Name:         data.Name.ValueStringPointer(),
		CatalogInput: catalogInput,
		Tags:         tags,
	}

	tflog.Debug(ctx, "Creating Glue Catalog", map[string]interface{}{
		"name": data.Name.ValueString(),
	})

	// Create the catalog
	glueClient := r.getGlueClient(data.Region.ValueString())
	_, err := glueClient.CreateCatalog(ctx, createInput)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create Glue Catalog, got error: %s", err))
		return
	}

	// Read the catalog to get the computed values
	r.readCatalog(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *GlueCatalogResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data GlueCatalogResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	r.readCatalog(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *GlueCatalogResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data GlueCatalogResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Build the CatalogInput
	catalogInput := r.buildCatalogInput(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	// Update the catalog
	updateInput := &glue.UpdateCatalogInput{
		CatalogId:    data.Name.ValueStringPointer(),
		CatalogInput: catalogInput,
	}

	tflog.Debug(ctx, "Updating Glue Catalog", map[string]interface{}{
		"name": data.Name.ValueString(),
	})

	// Update the catalog
	glueClient := r.getGlueClient(data.Region.ValueString())
	_, err := glueClient.UpdateCatalog(ctx, updateInput)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to update Glue Catalog, got error: %s", err))
		return
	}

	// Read the catalog to get the updated values
	r.readCatalog(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *GlueCatalogResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data GlueCatalogResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Deleting Glue Catalog", map[string]interface{}{
		"name": data.Name.ValueString(),
	})

	// Delete the catalog
	glueClient := r.getGlueClient(data.Region.ValueString())
	_, err := glueClient.DeleteCatalog(ctx, &glue.DeleteCatalogInput{
		CatalogId: data.Name.ValueStringPointer(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete Glue Catalog, got error: %s", err))
		return
	}
}

func (r *GlueCatalogResource) readCatalog(ctx context.Context, data *GlueCatalogResourceModel, diagnostics *diag.Diagnostics) {
	glueClient := r.getGlueClient(data.Region.ValueString())
	getCatalogOutput, err := glueClient.GetCatalog(ctx, &glue.GetCatalogInput{
		CatalogId: data.Name.ValueStringPointer(),
	})
	if err != nil {
		diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read Glue Catalog, got error: %s", err))
		return
	}

	catalog := getCatalogOutput.Catalog
	if catalog == nil {
		diagnostics.AddError("Client Error", "Catalog object is nil in GetCatalog response")
		return
	}

	// Update computed fields
	if catalog.ResourceArn != nil {
		data.ResourceArn = types.StringValue(*catalog.ResourceArn)
	}

}

func convertPrincipalPermissionsToSDK(ctx context.Context, permissions []PrincipalPermissionsModel, diagnostics *diag.Diagnostics) []gluetypes.PrincipalPermissions {
	if len(permissions) == 0 {
		return []gluetypes.PrincipalPermissions{}
	}

	result := make([]gluetypes.PrincipalPermissions, len(permissions))
	for i, perm := range permissions {
		pp := gluetypes.PrincipalPermissions{}

		if !perm.Principal.IsNull() {
			var principal DataLakePrincipalModel
			diagnostics.Append(perm.Principal.As(ctx, &principal, basetypes.ObjectAsOptions{})...)
			if diagnostics.HasError() {
				return nil
			}
			pp.Principal = &gluetypes.DataLakePrincipal{
				DataLakePrincipalIdentifier: principal.DataLakePrincipalIdentifier.ValueStringPointer(),
			}
		}

		if !perm.Permissions.IsNull() {
			var perms []types.String
			diagnostics.Append(perm.Permissions.ElementsAs(ctx, &perms, false)...)
			if diagnostics.HasError() {
				return nil
			}
			pp.Permissions = make([]gluetypes.Permission, len(perms))
			for j, p := range perms {
				pp.Permissions[j] = gluetypes.Permission(p.ValueString())
			}
		}

		result[i] = pp
	}

	return result
}
