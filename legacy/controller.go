package main

import (
	"net/http"
	"sync"
)

// NGINXController describes a NGINX Ingress controller.
type NGINXController struct {
	cfg *NginxConfiguration

	recorder record.EventRecorder

	syncQueue *task.Queue

	syncStatus status.Syncer

	syncRateLimiter flowcontrol.RateLimiter

	workersReloading bool

	// stopLock is used to enforce that only a single call to Stop send at
	// a given time. We allow stopping through an HTTP endpoint and
	// allowing concurrent stoppers leads to stack traces.
	stopLock *sync.Mutex

	stopCh   chan struct{}
	updateCh *channels.RingChannel

	// ngxErrCh is used to detect errors with the NGINX processes
	ngxErrCh chan error

	// runningConfig contains the running configuration in the Backend
	runningConfig *Configuration

	t ngx_template.Writer

	resolver []net.IP

	isIPV6Enabled bool

	isShuttingDown bool

	Proxy *tcpproxy.TCPProxy

	store store.Storer

	metricCollector metric.Collector

	validationWebhookServer *http.Server

	command NginxExecTester
}

// Configuration contains all the settings required by an Ingress controller
type NginxConfiguration struct {
	APIServerHost string
	RootCAFile    string

	KubeConfigFile string

	Client clientset.Interface

	ResyncPeriod time.Duration

	ConfigMapName  string
	DefaultService string

	Namespace string

	WatchNamespaceSelector labels.Selector

	// +optional
	TCPConfigMapName string
	// +optional
	UDPConfigMapName string

	DefaultSSLCertificate string

	// +optional
	PublishService       string
	PublishStatusAddress string

	UpdateStatus           bool
	UseNodeInternalIP      bool
	ElectionID             string
	ElectionTTL            time.Duration
	UpdateStatusOnShutdown bool

	HealthCheckHost string
	ListenPorts     *ngx_config.ListenPorts

	DisableServiceExternalName bool

	EnableSSLPassthrough bool

	DisableLeaderElection bool

	EnableProfiling bool

	EnableMetrics           bool
	MetricsPerHost          bool
	MetricsPerUndefinedHost bool
	MetricsBuckets          *collectors.HistogramBuckets
	MetricsBucketFactor     float64
	MetricsMaxBuckets       uint32
	ReportStatusClasses     bool
	ExcludeSocketMetrics    []string

	FakeCertificate *ingress.SSLCert

	SyncRateLimit float32

	DisableCatchAll bool

	IngressClassConfiguration *ingressclass.Configuration

	ValidationWebhook         string
	ValidationWebhookCertPath string
	ValidationWebhookKeyPath  string
	DisableFullValidationTest bool

	GlobalExternalAuth  *ngx_config.GlobalExternalAuth
	MaxmindEditionFiles *[]string

	MonitorMaxBatchSize int

	PostShutdownGracePeriod int
	ShutdownGracePeriod     int

	InternalLoggerAddress string
	IsChroot              bool
	DeepInspector         bool

	DynamicConfigurationRetries int

	DisableSyncEvents bool

	EnableTopologyAwareRouting bool
}
