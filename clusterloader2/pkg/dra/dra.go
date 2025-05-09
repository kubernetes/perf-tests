package dra

import (
	"time"

	"k8s.io/klog/v2"
	"k8s.io/perf-tests/clusterloader2/pkg/config"
	"k8s.io/perf-tests/clusterloader2/pkg/flags"
	"k8s.io/perf-tests/clusterloader2/pkg/framework"
	"k8s.io/perf-tests/clusterloader2/pkg/provider"
)

const (
	numK8sClients = 1
)

// InitFlags initializes prometheus flags.
func InitFlags(p *config.DRAExampleDriverConfig) {
	flags.BoolEnvVar(&p.InstallDriver, "install-dra-test-driver", "INSTALL_DRA_TEST_DRIVER", false, "Whether to install test dra-example-driver in the cluster.")
	flags.BoolEnvVar(&p.TearDownDriver, "tear-down-dra-test-driver", "TEAR_DOWN_DRA_TEST_DRIVER", true, "Whether to tear-down test dra-example-driver after tests (if set-up).")
}

// Controller is a u til for managing (install / tearing down) the prometheus stack in the cluster
type Controller struct {
	clusterLoaderConfig *config.ClusterLoaderConfig
	// provider is the cloud provider derived from the --provider flag.
	provider provider.Provider
	// framework associated with the cluster where the prometheus stack should be set up.
	// For kubemark it's the root cluster, otherwise it's the main (and only) cluster.
	framework *framework.Framework
	// timeout for waiting for Prometheus stack to become healthy
	readyTimeout time.Duration
}

// NewController creates a new instance of Controller for the given config.
func NewController(clusterLoaderConfig *config.ClusterLoaderConfig) (dc *Controller, err error) {
	dc = &Controller{
		clusterLoaderConfig: clusterLoaderConfig,
		provider:            clusterLoaderConfig.ClusterConfig.Provider,
		readyTimeout:        clusterLoaderConfig.PrometheusConfig.ReadyTimeout,
	}

	if dc.framework, err = framework.NewRootFramework(&clusterLoaderConfig.ClusterConfig, numK8sClients); err != nil {
		return nil, err
	}
	return dc, nil
}

// InstallTestDRADriver installs the dra-example-driver in the cluster.
// TODO: This method is idempotent, if the dra-example-driver already installed applying the manifests
// again will be no-op.
func (dc *Controller) InstallTestDRADriver() error {
	io := DefaultInstallOptions()
	io.Kubeconfig = dc.clusterLoaderConfig.ClusterConfig.KubeConfigPath

	klog.V(2).Infof("Installing test DRA driver")
	r, err := InstallChart(io)
	if err != nil {
		return err
	}

	klog.V(2).Infof("checking if test DRA driver %s is healthy", r.Name)
	err = dc.waitForDRADriverToBeHealthy()
	if err != nil {
		return err
	}

	return nil
}

func (dc *Controller) TearDownTestDRADriver() error {
	io := DefaultUninstallOptions()
	io.Kubeconfig = dc.clusterLoaderConfig.ClusterConfig.KubeConfigPath
	r, err := UninstallChart(io)
	if err != nil {
		return err
	}
	klog.V(2).Infof("Uninstalled test DRA driver %s\n", r.Release.Name)
	return nil
}
