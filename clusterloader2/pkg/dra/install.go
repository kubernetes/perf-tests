package dra

import (
	"context"
	"errors"
	"fmt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	"log"
	"os"
	"strings"
	"time"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/cli/values"
	"helm.sh/helm/v3/pkg/registry"
	"helm.sh/helm/v3/pkg/release"
)

const (
	checkDRAReadyInterval = 30 * time.Second
)

// InstallOptions contains all options for chart installation
type InstallOptions struct {
	// Namespace to install the chart into
	Namespace string
	// ReleaseName for the Helm installation
	ReleaseName string
	// ChartURL is the full chart URL for repository-based installation
	ChartURL string
	// ChartVersion is the version of the chart to install (empty for latest)
	ChartVersion string
	// Kubeconfig is the path to kubeconfig file
	Kubeconfig string
	// CreateNamespace determines if namespace should be created if it doesn't exist
	CreateNamespace bool
	// ValuesFile is the path to custom values file
	ValuesFile string
	// EnableValidationPolicy enables ValidatingAdmissionPolicy (disabled by default)
	EnableValidationPolicy bool
	// LocalChart determines if a local chart should be used instead of repository
	LocalChart bool
	// Logger for logging messages (if nil, log package will be used)
	Logger func(string, ...interface{})
}

// DefaultInstallOptions returns the default install options
func DefaultInstallOptions() *InstallOptions {
	return &InstallOptions{
		Namespace:              "dra-example-driver",
		ReleaseName:            "dra-example-driver",
		ChartURL:               "oci://registry.k8s.io/dra-example-driver/charts/dra-example-driver",
		CreateNamespace:        true,
		EnableValidationPolicy: false,
		LocalChart:             false,
		Logger:                 log.Printf,
	}
}

// logf logs a formatted message if logger is set
func (o *InstallOptions) logf(format string, args ...interface{}) {
	if o.Logger != nil {
		o.Logger(format, args...)
	}
}

// InstallChart installs a Helm chart using the provided options
func InstallChart(opts *InstallOptions) (*release.Release, error) {
	if opts == nil {
		opts = DefaultInstallOptions()
	}

	// Enable OCI support
	os.Setenv("HELM_EXPERIMENTAL_OCI", "1")

	// Set up Helm environment
	settings := cli.New()

	// Override kubeconfig if specified
	if opts.Kubeconfig != "" {
		settings.KubeConfig = opts.Kubeconfig
	}

	// Create the action configuration
	actionConfig := new(action.Configuration)

	// Initialize Registry client
	registryClient, regErr := registry.NewClient(
		registry.ClientOptDebug(settings.Debug),
		registry.ClientOptWriter(os.Stdout),
		registry.ClientOptCredentialsFile(settings.RegistryConfig),
	)
	if regErr != nil {
		return nil, fmt.Errorf("failed to create registry client: %v", regErr)
	}

	actionConfig.RegistryClient = registryClient

	var err error
	if err = actionConfig.Init(settings.RESTClientGetter(), opts.Namespace, os.Getenv("HELM_DRIVER"), opts.logf); err != nil {
		return nil, fmt.Errorf("failed to initialize Helm action configuration: %v", err)
	}

	// Check if release exists
	histClient := action.NewHistory(actionConfig)
	histClient.Max = 1
	_, histErr := histClient.Run(opts.ReleaseName)

	// Get values from values file
	vals, err := getValues(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to get values: %v", err)
	}

	// Set validation policy enabled/disabled
	if vals == nil {
		vals = make(map[string]interface{})
	}

	webhook, ok := vals["webhook"].(map[string]interface{})
	if !ok {
		webhook = make(map[string]interface{})
		vals["webhook"] = webhook
	}

	webhook["enabled"] = opts.EnableValidationPolicy
	opts.logf("ValidatingAdmissionPolicy (webhook.enabled) is %s", map[bool]string{true: "enabled", false: "disabled"}[opts.EnableValidationPolicy])

	// Load the chart
	chartRequested, err := loadChart(opts, settings, actionConfig)
	if err != nil {
		return nil, err
	}

	// Install or upgrade the chart
	rel, err := installOrUpgradeChart(actionConfig, chartRequested, vals, opts, histErr)
	if err != nil {
		return nil, fmt.Errorf("failed to install or upgrade chart: %v", regErr)
	}

	return rel, nil
}

func (dc *Controller) waitForDRADriverToBeHealthy() error {
	return wait.PollImmediate(
		checkDRAReadyInterval,
		dc.readyTimeout,
		dc.isDRADriverReady)
}

func (dc *Controller) isDRADriverReady() (done bool, err error) {
	ds, err := dc.framework.GetClientSets().
		GetClient().
		AppsV1().
		DaemonSets(DefaultInstallOptions().Namespace).
		Get(context.Background(), "dra-example-driver-kubeletplugin", metav1.GetOptions{})
	if err != nil {
		return false, fmt.Errorf("failed to get dra-example-driver-kubeletplugin: %v", err)
	}
	ready := ds.Status.NumberReady == ds.Status.DesiredNumberScheduled
	if !ready {
		klog.V(2).Infof("dra-example-driver-kubeletplugin is not ready, "+
			"DesiredNumberScheduled: %d, NumberReady: %d", ds.Status.DesiredNumberScheduled, ds.Status.NumberReady)
	}
	return ready, nil
}

// getValues loads and merges values from the values file
func getValues(opts *InstallOptions) (map[string]interface{}, error) {
	valueOpts := &values.Options{}
	if opts.ValuesFile != "" {
		valueOpts.ValueFiles = []string{opts.ValuesFile}
	}

	return valueOpts.MergeValues(nil)
}

// loadChart loads a chart from local path, HTTP repository, or OCI registry
func loadChart(opts *InstallOptions, settings *cli.EnvSettings, actionConfig *action.Configuration) (*chart.Chart, error) {
	isOCI := strings.HasPrefix(opts.ChartURL, "oci://")
	if isOCI {
		return loadOCIChart(opts, settings, actionConfig)
	}

	return nil, errors.New("non oci helm chart is not supported")
}

// loadOCIChart loads a chart from an OCI registry
func loadOCIChart(opts *InstallOptions, settings *cli.EnvSettings, actionConfig *action.Configuration) (*chart.Chart, error) {
	opts.logf("Using OCI chart URL: %s", opts.ChartURL)

	install := action.NewInstall(actionConfig)
	install.ChartPathOptions.Version = opts.ChartVersion

	localChartPath, err := install.ChartPathOptions.LocateChart(opts.ChartURL, settings)
	if err != nil {
		return nil, fmt.Errorf("failed to locate OCI chart: %v", err)
	}

	opts.logf("Downloaded chart to: %s", localChartPath)
	return loader.Load(localChartPath)
}

// installOrUpgradeChart installs or upgrades a chart based on whether it already exists
func installOrUpgradeChart(
	actionConfig *action.Configuration,
	chartRequested *chart.Chart,
	vals map[string]interface{},
	opts *InstallOptions,
	histErr error,
) (*release.Release, error) {
	if histErr != nil {
		opts.logf("Installing chart for the first time")
		client := action.NewInstall(actionConfig)
		client.Namespace = opts.Namespace
		client.ReleaseName = opts.ReleaseName
		client.CreateNamespace = opts.CreateNamespace

		return client.Run(chartRequested, vals)
	}

	opts.logf("Upgrading existing chart")
	client := action.NewUpgrade(actionConfig)
	client.Namespace = opts.Namespace

	return client.Run(opts.ReleaseName, chartRequested, vals)
}

// UninstallOptions contains options for chart uninstallation
type UninstallOptions struct {
	// Namespace where the chart is installed
	Namespace string
	// ReleaseName for the Helm installation to uninstall
	ReleaseName string
	// Kubeconfig is the path to kubeconfig file
	Kubeconfig string
	// KeepHistory determines if release history should be kept
	KeepHistory bool
	// Logger for logging messages (if nil, log package will be used)
	Logger func(string, ...interface{})
}

// DefaultUninstallOptions returns the default uninstall options
func DefaultUninstallOptions() *UninstallOptions {
	return &UninstallOptions{
		Namespace:   "dra-example-driver",
		ReleaseName: "dra-example-driver",
		KeepHistory: false,
		Logger:      log.Printf,
	}
}

// UninstallChart uninstalls a Helm chart using the provided options
func UninstallChart(opts *UninstallOptions) (*release.UninstallReleaseResponse, error) {
	if opts == nil {
		opts = DefaultUninstallOptions()
	}

	// Enable OCI support
	os.Setenv("HELM_EXPERIMENTAL_OCI", "1")

	// Set up Helm environment
	settings := cli.New()

	// Override kubeconfig if specified
	if opts.Kubeconfig != "" {
		settings.KubeConfig = opts.Kubeconfig
	}

	// Create the action configuration
	actionConfig := new(action.Configuration)

	// Initialize Registry client
	registryClient, regErr := registry.NewClient(
		registry.ClientOptDebug(settings.Debug),
		registry.ClientOptWriter(os.Stdout),
		registry.ClientOptCredentialsFile(settings.RegistryConfig),
	)
	if regErr != nil {
		return nil, fmt.Errorf("failed to create registry client: %v", regErr)
	}

	actionConfig.RegistryClient = registryClient

	// logf function for the actionConfig
	logf := func(format string, args ...interface{}) {
		if opts.Logger != nil {
			opts.Logger(format, args...)
		}
	}

	var err error
	if err = actionConfig.Init(settings.RESTClientGetter(), opts.Namespace, os.Getenv("HELM_DRIVER"), logf); err != nil {
		return nil, fmt.Errorf("failed to initialize Helm action configuration: %v", err)
	}

	// Create uninstall client
	client := action.NewUninstall(actionConfig)
	client.KeepHistory = opts.KeepHistory

	// Uninstall the release
	logf("Uninstalling Helm release %s in namespace %s", opts.ReleaseName, opts.Namespace)
	return client.Run(opts.ReleaseName)
}
