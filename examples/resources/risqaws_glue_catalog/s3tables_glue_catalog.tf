data "aws_region" "current" {}
data "aws_caller_identity" "this" {}

locals {
  account_id        = data.aws_caller_identity.this.account_id
  region            = data.aws_region.current.id
  s3tables_base_arn = "arn:aws:s3tables:${local.region}:${local.account_id}:bucket"
  s3tables_arn      = "${local.s3tables_base_arn}/*"
}

resource "risqaws_glue_catalog" "s3tables" {
  name = "s3tablescatalog"
  federated_catalog = {
    identifier      = local.s3tables_arn
    connection_name = "aws:s3tables"
    connection_type = "aws:s3tables"
  }
  create_table_default_permissions      = []
  create_database_default_permissions   = []
  allow_full_table_external_data_access = "True"
}
