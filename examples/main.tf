provider "aws" {
  region = "eu-west-2"
}

module "ingress_validator" {
  source = "../"

  replica_count        = "3"
  controller_name      = "default"
  enable_anti_affinity = true
  enable_modsec        = true
  enable_owasp         = true
  memory_requests      = "512Mi"
  memory_limits        = "2Gi"
  cluster              = "my-eks-cluster"
  validator_registry   = "1234.dkr.ecr.eu-west-2.amazonaws.com"
  validator_image      = "team/reg"
  validator_tag        = "tag"
  validator_digest     = "sha256:blah"

}