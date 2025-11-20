terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 6, < 7"
    }
    risqaws = {
      source = "risqcapital/risqaws"
    }
  }

  # 1.7: test mocks, removed block
  # 1.6: tests, expressions in import blocks
  # 1.5, check blocks, import blocks
  # 1.4: terraform_data resource
  # 1.3: optional attributes
  # 1.2: preconditions, postconditions, replace_triggered_by
  # 1.1: nullable attributes
  required_version = ">= 1.3, < 2"
}
