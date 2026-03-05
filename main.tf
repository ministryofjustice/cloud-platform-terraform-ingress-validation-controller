##########
# Locals #
##########
locals {
  tags = join(", ", [for key, value in var.default_tags : "${key}=${value}"])
}

########
# Helm #
########

resource "helm_release" "nginx_ingress_validator" {
  name       = "nginx-ingress-${var.controller_name}-validator"
  chart      = "ingress-nginx"
  namespace  = "ingress-controllers"
  repository = "https://kubernetes.github.io/ingress-nginx"
  timeout    = 600
  version    = "4.14.3"

  values = [templatefile("${path.module}/templates/values.yaml.tpl", {
    metrics_namespace           = "ingress-controllers"
    replica_count               = var.replica_count
    controller_name             = var.controller_name
    controller_value            = "k8s.io/ingress-${var.controller_name}"
    enable_modsec               = var.enable_modsec
    enable_owasp                = var.enable_owasp
    enable_anti_affinity        = var.enable_anti_affinity
    default                     = var.controller_name == "default" ? true : false
    name_override               = "ingress-${var.controller_name}-validator"
    memory_requests             = var.memory_requests
    memory_limits               = var.memory_limits
    modsec_nginx_cm_config_name = "modsecurity-nginx-validator-config-${var.controller_name}"
    default_tags                = local.tags
    validator_registry          = var.validator_registry
    validator_image             = var.validator_image
    validator_tag               = var.validator_tag
    validator_digest            = var.validator_digest
  })]

  depends_on = [
    kubernetes_config_map.modsecurity_nginx_validator_config,
  ]

  lifecycle {
    ignore_changes = [keyring]
  }
}
