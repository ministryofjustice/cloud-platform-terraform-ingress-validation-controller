variable "replica_count" {
  type        = string
  description = "Number of replicas set in deployment"
}

variable "controller_name" {
  type        = string
  description = "Will be used as the ingress controller name and the class annotation"
}

variable "enable_modsec" {
  description = "Enable https://github.com/SpiderLabs/ModSecurity-nginx"
  type        = bool
  default     = false
}

variable "enable_owasp" {
  description = "Use default ruleset from https://github.com/SpiderLabs/owasp-modsecurity-crs/"
  type        = bool
  default     = false
}

variable "memory_limits" {
  description = "value for resources:limits memory value"
  default     = "2Gi"
  type        = string
}

variable "memory_requests" {
  description = "value for resources:requests memory value"
  default     = "512Mi"
  type        = string
}

variable "cluster" {
  description = " cluster name used for opensearch indices"
  type        = string
  default     = ""
}

variable "enable_anti_affinity" {
  description = "prevent controllers from being deployed to the same node, useful in live as controllers are extremely resource heavy"
  type        = bool
  default     = false
}

variable "validator_registry" {
  description = "The registry for the validator image"
  default     = "754256621582.dkr.ecr.eu-west-2.amazonaws.com"
}

variable "validator_image" {
  description = "The name of the validator image"
  default     = ""
}

variable "validator_tag" {
  description = "The tag of the validator image"
  default     = ""
}

variable "validator_digest" {
  description = "The digest of the validator image"
  default     = ""
}

variable "default_tags" {
  description = "List of default_tags for resources"
  type        = map(string)
  default = {
    business-unit = "Platforms"
    owner         = "Cloud Platform: platforms@digital.justice.gov.uk"
    source-code   = "github.com/ministryofjustice/cloud-platform-terraform-ingress-controller"
  }
}