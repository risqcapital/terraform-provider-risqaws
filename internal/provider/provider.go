// Copyright (c) Risq Research Ltd.
// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/ephemeral"
	"github.com/hashicorp/terraform-plugin-framework/function"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

// Ensure RisqAwsProvider satisfies various provider interfaces.
var _ provider.Provider = &RisqAwsProvider{}
var _ provider.ProviderWithFunctions = &RisqAwsProvider{}
var _ provider.ProviderWithEphemeralResources = &RisqAwsProvider{}

// RisqAwsProvider defines the provider implementation.
type RisqAwsProvider struct {
	// version is set to the provider version on release, "dev" when the
	// provider is built and ran locally, and "test" when running acceptance
	// testing.
	version string
}

// RisqAwsProviderModel describes the provider data model.
type RisqAwsProviderModel struct{}

func (p *RisqAwsProvider) Metadata(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "risqaws"
	resp.Version = p.version
}

func (p *RisqAwsProvider) Schema(ctx context.Context, req provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{}
}

func (p *RisqAwsProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var data RisqAwsProviderModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to load configuration",
			fmt.Sprintf("Configuration could not be loaded: %v", err),
		)
		return
	}

	resp.DataSourceData = cfg
	resp.ResourceData = cfg
}

func (p *RisqAwsProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewSsmCommandResource,
		NewGlueCatalogResource,
	}
}

func (p *RisqAwsProvider) EphemeralResources(ctx context.Context) []func() ephemeral.EphemeralResource {
	return []func() ephemeral.EphemeralResource{}
}

func (p *RisqAwsProvider) DataSources(ctx context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{}
}

func (p *RisqAwsProvider) Functions(ctx context.Context) []func() function.Function {
	return []func() function.Function{
		NewJSONEncodeFunction,
	}
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &RisqAwsProvider{
			version: version,
		}
	}
}
