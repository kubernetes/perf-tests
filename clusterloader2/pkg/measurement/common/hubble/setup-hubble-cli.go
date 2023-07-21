package hubble

import (
	"embed"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"k8s.io/perf-tests/clusterloader2/pkg/framework"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
)

const (
	setupHubbleCliMeasurementName = "SetupHubbleCli"

	hubbleCliPath = "manifests/hubble-cli.yaml"
)

var (
	hubbleCliSchema = schema.GroupVersionKind{
		Kind:    "Deployment",
		Version: "apps/v1",
	}

	//go:embed manifests
	manifestsFS embed.FS
)

func init() {
	klog.Info("Registering Setup Hubble CLI Measurement")
	if err := measurement.Register(setupHubbleCliMeasurementName, createSetupHubbleCliMeasurement); err != nil {
		klog.Fatalf("Cannot register %s: %v", setupHubbleCliMeasurementName, err)
	}
}

func createSetupHubbleCliMeasurement() measurement.Measurement {
	return &setupHubbleCliMeasurement{}
}

type setupHubbleCliMeasurement struct {
	framework *framework.Framework
	k8sClient kubernetes.Interface
}

func (shcm *setupHubbleCliMeasurement) Execute(config *measurement.Config) ([]measurement.Summary, error) {
	return nil, shcm.createObjects(config)
}

func (shcm *setupHubbleCliMeasurement) initialize(config *measurement.Config) {
	shcm.framework = config.ClusterFramework
	shcm.k8sClient = config.ClusterFramework.GetClientSets().GetClient()
}

func (shcm *setupHubbleCliMeasurement) createObjects(config *measurement.Config) error {
	shcm.initialize(config)
	if err := shcm.framework.ApplyTemplatedManifests(manifestsFS, hubbleCliPath, nil); err != nil {
		return fmt.Errorf("error while creating Hubble-cli pod: %w", err)
	}
	return nil
}

func (shcm *setupHubbleCliMeasurement) Dispose() {
	if shcm.framework == nil {
		klog.V(1).Infof("%s measurement wasn't started, skipping the Dispose() step", shcm.String())
		return
	}
	if err := shcm.framework.DeleteObject(hubbleCliSchema, "kube-system", "hubble-cli"); err != nil {
		klog.Errorf("Failed to delete Hubble-cli pod: %v", err)
	}
}

func (shcm *setupHubbleCliMeasurement) String() string {
	return setupHubbleCliMeasurementName
}
