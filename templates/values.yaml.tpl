nameOverride: ${name_override}
controller:
## enableAnnotationValidations defaults to false in 4.10.4, however bringing into template for future ref
  enableAnnotationValidations: true
  image:
    registry: ${validator_registry}
    image: ${validator_image}
    tag: ${validator_tag}
    digest: ${validator_digest}
    chroot: false
    terminationGracePeriod: 60
  replicaCount: ${replica_count}
  maxUnavailable: 1
  priorityClassName: system-cluster-critical
  minReadySeconds: 10

  # -- This configuration defines if Ingress Controller should allow users to set
  # their own *-snippet annotations, otherwise this is forbidden / dropped
  # when users add those annotations.
  # Global snippets in ConfigMap are still respected
  allowSnippetAnnotations: true
  
%{ if enable_modsec ~}
  extraVolumes:
    - name: logs-volume
      emptyDir: {}
    - name: logs-debug-volume
      emptyDir: {}
    - name: modsecurity-nginx-config
      configMap:
        name: ${modsec_nginx_cm_config_name}

  extraVolumeMounts:
  ## Additional volumeMounts to the controller main container.
    - name: logs-volume
      mountPath: /var/log/audit/
    - name: logs-debug-volume
      mountPath: /var/log/debug/
    - name: modsecurity-nginx-config
      mountPath: /etc/nginx/modsecurity/modsecurity.conf
      subPath: modsecurity.conf
      readOnly: true
%{ endif ~}

  updateStrategy:
    rollingUpdate:
      maxUnavailable: 1
    type: RollingUpdate

  minReadySeconds: 12

  # -- Process Ingress objects without ingressClass annotation/ingressClassName field
  # Overrides value for --watch-ingress-without-class flag of the controller binary
  # Defaults to false
  watchIngressWithoutClass: false

  # -- Disable ingress status updates since this is a validator only.
  # Without this, the validator overwrites ingress status with pod IPs
  # instead of the production controller's NLB address.
  extraArgs:
    update-status: "false"
    
  # -- Process IngressClass per name (additionally as per spec.controller).
  ingressClassByName: ${default}

  ## IngressClass resource disabled for validator
  ingressClassResource:
    enabled: false
    # -- Controller-value of the controller that is processing this ingressClass
    controllerValue: ${controller_value}
    
  # -- For backwards compatibility with ingress.class annotation, use ingressClass.
  # Algorithm is as follows, first ingressClassName is considered, if not present, controller looks for ingress.class annotation
  ingressClass: ${controller_name}

  electionID: ingress-controller-leader-${controller_name}-validator

  livenessProbe:
    initialDelaySeconds: 20
    periodSeconds: 20
    timeoutSeconds: 5

  readinessProbe:
    initialDelaySeconds: 20
    periodSeconds: 20
    timeoutSeconds: 5

  resources:
    limits:
      memory: ${memory_limits}
    requests:
      memory: ${memory_requests}

  config:
    enable-modsecurity: ${enable_modsec}
    enable-owasp-modsecurity-crs: ${enable_owasp}
    server-tokens: "false"
    generate-request-id: "true"
    annotations-risk-level: "Critical"

  metrics:
    enabled: true
    serviceMonitor:
      enabled: true
      namespace: ${metrics_namespace}
      additionalLabels:
        release: prometheus-operator

## This is a validating ingress controller, so we don't want a Service created.
  service:
    enabled: false
  
  admissionWebhooks:
    enabled: true
    annotations: {}
    enabled: true
    failurePolicy: Fail
    # timeoutSeconds: 10
    port: 8443
    certificate: "/usr/local/certificates/cert"
    key: "/usr/local/certificates/key"
    namespaceSelector: {}
    objectSelector: {}

    service:
      annotations: {}
      # clusterIP: ""
      externalIPs: []
      # loadBalancerIP: ""
      loadBalancerSourceRanges: []
      servicePort: 443
      type: ClusterIP

    patch:
      enabled: true

defaultBackend:
  enabled: false

rbac:
  create: true
%{ if enable_modsec }   
serviceAccount:
  create: true
  name: ""
  automountServiceAccountToken: true  
%{~ endif ~}