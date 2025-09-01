terraform {
  required_version = ">= 1.0"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 5.0"
    }
    rhcs = {
      version = ">= 1.6.2"
      source  = "terraform-redhat/rhcs"
    }
  }
}
