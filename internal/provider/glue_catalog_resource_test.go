package provider

import (
	"context"
	"fmt"
	"log"
	"os"
	"testing"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

var (
	testAccAccountID string
)

func TestAccGlueCatalogResource(t *testing.T) {
	// Native catalog creation is not supported by AWS Glue CreateCatalog API without specific configurations (e.g. Federated or Redshift).
	// Skipping this basic test for now as it fails with "InvalidInputException: Create glue native catalog is not supported."
	t.Skip("Skipping basic native catalog test as it is not supported by AWS")
}

func TestAccGlueCatalogResource_S3Tables(t *testing.T) {
	rName := "s3tablescatalog"
	resourceName := "risqaws_glue_catalog.test"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccGlueCatalogResourceConfig_S3Tables(rName, "us-east-1"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "name", rName),
					resource.TestCheckResourceAttr(resourceName, "region", "us-east-1"),
					resource.TestCheckResourceAttr(resourceName, "federated_catalog.connection_name", "aws:s3tables"),
					resource.TestCheckResourceAttr(resourceName, "federated_catalog.connection_type", "aws:s3tables"),
					resource.TestCheckResourceAttrSet(resourceName, "federated_catalog.identifier"),
					resource.TestCheckResourceAttr(resourceName, "allow_full_table_external_data_access", "true"),
				),
			},
		},
	})
}

func testAccGlueCatalogResourceConfig_S3Tables(name, region string) string {
	return fmt.Sprintf(`
resource "risqaws_glue_catalog" "test" {
  name = %[1]q
  region = %[2]q
  federated_catalog = {
    identifier      = "arn:aws:s3tables:%[2]s:%[3]s:bucket/*"
    connection_name = "aws:s3tables"
    connection_type = "aws:s3tables"
  }
  create_table_default_permissions      = []
  create_database_default_permissions   = []
  allow_full_table_external_data_access = true
}
`, name, region, testAccAccountID)
}

func testAccPreCheck(t *testing.T) {
	// Skip if not running acceptance tests
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC not set, skipping acceptance test")
	}
	if os.Getenv("CI") != "" {
		t.Skip("CI set, skipping acceptance test (not providing AWS credentials in CI)")
	}
}

func TestMain(m *testing.M) {
	if os.Getenv("CI") == "" {
		initAwsConfig()
	}

	exitVal := m.Run()
	os.Exit(exitVal)
}

func initAwsConfig() {
	// Load AWS config and get account ID
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatalf("failed to load AWS config: %v", err)
	}

	// Get account ID using STS
	stsClient := sts.NewFromConfig(cfg, func(options *sts.Options) {
		options.Region = "us-east-1"
	})
	identity, err := stsClient.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		log.Fatalf("failed to get caller identity: %v", err)
	}

	if identity.Account == nil {
		log.Fatal("failed to determine AWS account ID")
	}

	testAccAccountID = *identity.Account
	log.Printf("Using AWS account ID: %s\n", testAccAccountID)
}
