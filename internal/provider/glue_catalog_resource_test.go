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
	testAccRegion    string
)

func TestAccGlueCatalogResource(t *testing.T) {
	// Native catalog creation is not supported by AWS Glue CreateCatalog API without specific configurations (e.g. Federated or Redshift).
	// Skipping this basic test for now as it fails with "InvalidInputException: Create glue native catalog is not supported."
	t.Skip("Skipping basic native catalog test as it is not supported by AWS")
}

func TestAccGlueCatalogResource_S3Tables(t *testing.T) {
	rName := "s3tablescatalog"
	resourceName := "risqaws_glue_catalog.test"

	resourceConfig := testAccGlueCatalogResourceConfig_S3Tables(rName)
	t.Logf("Resource under test: %s", resourceConfig)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: resourceConfig,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "name", rName),
					resource.TestCheckResourceAttr(resourceName, "federated_catalog.connection_name", "aws:s3tables"),
					resource.TestCheckResourceAttr(resourceName, "federated_catalog.connection_type", "aws:s3tables"),
					resource.TestCheckResourceAttrSet(resourceName, "federated_catalog.identifier"),
					resource.TestCheckResourceAttr(resourceName, "allow_full_table_external_data_access", "True"),
				),
			},
		},
	})
}

func testAccGlueCatalogResourceConfig_S3Tables(name string) string {
	return fmt.Sprintf(`
resource "risqaws_glue_catalog" "test" {
  name = %[1]q
  federated_catalog = {
    identifier      = "arn:aws:s3tables:%[2]s:%[3]s:bucket/*"
    connection_name = "aws:s3tables"
    connection_type = "aws:s3tables"
  }
  create_table_default_permissions      = []
  create_database_default_permissions   = []
  allow_full_table_external_data_access = "True"
}
`, name, testAccRegion, testAccAccountID)
}

func testAccPreCheck(t *testing.T) {
	// Skip if not running acceptance tests
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC not set, skipping acceptance test")
	}

}

func TestMain(m *testing.M) {
	// Write code here to run before tests
	// Load AWS config and get account ID and region
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatalf("failed to load AWS config: %v", err)
	}

	// Get region from config
	testAccRegion = cfg.Region
	if testAccRegion == "" {
		// Try to get from environment variable
		testAccRegion = os.Getenv("AWS_REGION")
		if testAccRegion == "" {
			testAccRegion = os.Getenv("AWS_DEFAULT_REGION")
		}
		if testAccRegion == "" {
			log.Fatal("AWS region not configured")
		}
	}

	log.Printf("Using AWS region: %s\n", testAccRegion)

	// Get account ID using STS
	stsClient := sts.NewFromConfig(cfg)
	identity, err := stsClient.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		log.Fatalf("failed to get caller identity: %v", err)
	}

	if identity.Account == nil {
		log.Fatal("failed to determine AWS account ID")
	}

	testAccAccountID = *identity.Account
	log.Printf("Using AWS account ID: %s\n", testAccAccountID)

	// Run tests
	exitVal := m.Run()

	// Write code here to run after tests

	// Exit with exit value from tests
	os.Exit(exitVal)
}
