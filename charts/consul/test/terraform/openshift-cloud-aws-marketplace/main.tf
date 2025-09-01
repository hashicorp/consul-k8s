locals {
  account_role_prefix  = "${var.cluster_name}-account"
  operator_role_prefix = "${var.cluster_name}-operator"
}

module "rosa" {
  source = "github.com/terraform-redhat/terraform-rhcs-rosa-classic"
  cluster_name           = var.cluster_name
  openshift_version      = var.openshift_version
  create_account_roles   = false
  create_operator_roles  = true
  create_oidc            = var.create_oidc
  account_role_prefix    = module.account_iam_resources.account_role_prefix
  operator_role_prefix   = var.operator_role_prefix
  oidc_config_id         = var.oidc_config_id
  oidc_endpoint_url      = var.oidc_endpoint_url

  machine_cidr           = module.vpc.cidr_block
  aws_subnet_ids         = concat(module.vpc.public_subnets, module.vpc.private_subnets)
  aws_availability_zones = module.vpc.availability_zones
  multi_az               = length(module.vpc.availability_zones) > 1
  path                   = module.account_iam_resources.path
  replicas               = 3
  govcloud               = false
}

############################
# VPC
############################
module "vpc" {
  source = "github.com/terraform-redhat/terraform-rhcs-rosa-classic/modules/vpc"

  name_prefix              = var.cluster_name
  availability_zones_count = 3
}

### This can be split out into dedicated IAM module ###

module "account_iam_resources" {
  source = "github.com/terraform-redhat/terraform-rhcs-rosa-classic/modules/account-iam-resources"
  
  account_role_prefix = local.account_role_prefix
  openshift_version   = var.openshift_version
  # path                = "/tf-example/"
}

module "operator_policies" {
  source = "github.com/terraform-redhat/terraform-rhcs-rosa-classic/modules/operator-policies"

  account_role_prefix = local.account_role_prefix
  openshift_version   = var.openshift_version
  path                = module.account_iam_resources.path
}

# module "operator_roles" {
#   source = "github.com/terraform-redhat/terraform-rhcs-rosa-classic/modules/operator-roles"

#   operator_role_prefix = local.operator_role_prefix
#   account_role_prefix  = module.operator_policies.account_role_prefix
#   path                 = module.account_iam_resources.path
#   oidc_endpoint_url    = "oidc.op1.openshiftapps.com/2j7dm2lchqq6mvchteg0gc1tmht0pn4k"
# }

# module "oidc_config_and_provider" {
#   source = "github.com/terraform-redhat/terraform-rhcs-rosa-classic/modules/oidc-config-and-provider"

#   managed = true
# }
