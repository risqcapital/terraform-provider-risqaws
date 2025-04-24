terraform {
  required_providers {
    risqaws = {
      source = "github.com/risqcapital/risq-aws"
    }
    aws = {
      source  = "hashicorp/aws"
      version = "5.81.0"
    }
  }
}

provider "risqaws" {}
provider "aws" {}

resource "aws_ssm_document" "this" {
  name = "bptest"
  content = jsonencode({
    schemaVersion = "2.2"
    description   = "BPTest"
    parameters = {
      name = {
        type        = "String"
        description = "Name"
        default     = "World"
      }
    }
    mainSteps = [
      {
        precondition = {
          StringEquals = [
            "platformType",
            "Linux"
          ]
        }
        action = "aws:runShellScript",
        name   = "Test",
        inputs = {
          runCommand = [
            "echo Hello {{ name }} && exit 1"
          ]
        }
      }
    ]
  })
  document_type = "Command"
}

resource "risqaws_ssm_command" "this" {
  document_name    = aws_ssm_document.this.name
  document_version = aws_ssm_document.this.latest_version
  targets {
    key    = "InstanceIds"
    values = ["i-0baf61e6a1c05e296", "i-0aa018cc76cb0628c"]
  }
  parameters = {
    "name" = "test"
  }

  lifecycle {
    replace_triggered_by = [
      aws_ssm_document.this.latest_version,
    ]
  }
}