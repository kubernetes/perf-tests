/*
Copyright 2019 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package prometheus

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"regexp"
	"time"

	"github.com/spf13/pflag"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog"
)

var (
	csiVolumeHandlePattern = regexp.MustCompile(`projects/(?P<Project>[^/]+)/zones/(?P<Zone>[^/]+)/disks/(?P<Name>[^/]+)`)
)

type prometheusDiskMetadata struct {
	name string
	zone string
}

const (
	gcloudRetryInterval  = 20 * time.Second
	snapshotRetryTimeout = 5 * time.Minute
	deleteRetryTimeout   = 2 * time.Minute
)

var (
	shouldSnapshotPrometheusDisk        = pflag.Bool("experimental-gcp-snapshot-prometheus-disk", false, "(experimental, provider=gce|gke only) whether to snapshot Prometheus disk before Prometheus stack is torn down")
	shouldSnapshotPrometheusToReportDir = pflag.Bool("experimental-prometheus-snapshot-to-report-dir", false, "(experimental) whether to save prometheus snapshot to the report-dir")
	prometheusDiskSnapshotName          = pflag.String("experimental-prometheus-disk-snapshot-name", "", "Name of the prometheus disk snapshot that will be created if snapshots are enabled. If not set, the prometheus disk name will be used.")
)

func (pc *Controller) isEnabled() (bool, error) {
	if !*shouldSnapshotPrometheusDisk {
		return false, nil
	}
	if !pc.provider.Features().SupportSnapshotPrometheusDisk {
		return false, fmt.Errorf(
			"snapshotting Prometheus' disk only available for GCP providers (gce, gke, kubemark), provider is: %s", pc.provider.Name())
	}
	return true, nil
}

func (pc *Controller) cachePrometheusDiskMetadataIfEnabled() error {
	if enabled, err := pc.isEnabled(); !enabled {
		return err
	}
	return wait.Poll(
		10*time.Second,
		2*time.Minute,
		pc.tryRetrievePrometheusDiskMetadata)
}

func (pc *Controller) tryRetrievePrometheusDiskMetadata() (bool, error) {
	klog.V(2).Info("Retrieving Prometheus' persistent disk metadata...")
	k8sClient := pc.framework.GetClientSets().GetClient()
	list, err := k8sClient.CoreV1().PersistentVolumes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		klog.Errorf("Listing PVs failed: %v", err)
		// Poll() stops on error so returning nil
		return false, nil
	}
	var pdName, zone string
	for _, pv := range list.Items {
		if pv.Spec.ClaimRef.Name != "prometheus-k8s-db-prometheus-k8s-0" {
			continue
		}
		if pv.Status.Phase != corev1.VolumeBound {
			continue
		}
		klog.V(2).Infof("Found Prometheus' PV with name: %s", pv.Name)
		pdName, zone, err = nameAndZone(pv)
		if err != nil {
			return false, fmt.Errorf("failed to extract name and value from pv: %w", err)
		}
		klog.V(2).Infof("PD name=%s, zone=%s", pdName, zone)
	}
	if pdName == "" || zone == "" {
		klog.Warningf("missing zone or PD name, aborting")
		klog.V(2).Info("PV list was:")
		s, err := json.MarshalIndent(list, "" /*=prefix*/, "  " /*=indent*/)
		if err != nil {
			klog.Warningf("Error while marshalling response %v: %v", list, err)
			return true, err
		}
		klog.V(2).Info(string(s))
		return true, nil
	}
	pc.diskMetadata.name = pdName
	pc.diskMetadata.zone = zone
	return true, nil
}

func nameAndZone(pv corev1.PersistentVolume) (name, zone string, err error) {
	switch {
	case pv.Spec.GCEPersistentDisk != nil:
		name = pv.Spec.GCEPersistentDisk.PDName
		zone = pv.ObjectMeta.Labels["topology.kubernetes.io/zone"]
		if zone == "" {
			// Fallback to old label to make it work for old k8s versions.
			zone = pv.ObjectMeta.Labels["failure-domain.beta.kubernetes.io/zone"]
		}
		return name, zone, err
	case pv.Spec.CSI != nil:
		r := csiVolumeHandlePattern.FindStringSubmatch(pv.Spec.CSI.VolumeHandle)
		if len(r) != 4 {
			return "", "", fmt.Errorf("unexpected format of volumeHandle: %q", pv.Spec.CSI.VolumeHandle)
		}
		return r[3], r[2], nil
	}
	return "", "", fmt.Errorf("unknown pv type: %+v", pv)
}

func (pc *Controller) snapshotPrometheusDiskIfEnabledSynchronized() error {
	pc.snapshotLock.Lock()
	defer pc.snapshotLock.Unlock()
	if pc.snapshotted {
		return pc.snapshotError
	}
	pc.snapshotted = true
	pc.snapshotError = pc.snapshotPrometheusDiskIfEnabled()
	return pc.snapshotError
}
func (pc *Controller) snapshotPrometheusIfEnabled() error {
	if !*shouldSnapshotPrometheusToReportDir {
		return nil
	}

	k8sClient := pc.framework.GetClientSets().GetClient()
	restClient := pc.framework.GetRestClient()
	filePath := path.Join(pc.clusterLoaderConfig.ReportDir, "prometheus_snapshot.tar.gz")
	return makeSnapshot(k8sClient, restClient, filePath)
}

func (pc *Controller) snapshotPrometheusDiskIfEnabled() error {
	if enabled, err := pc.isEnabled(); !enabled {
		return err
	}
	if pc.diskMetadata.name == "" || pc.diskMetadata.zone == "" {
		klog.Errorf("Missing zone or PD name, aborting snapshot")
		klog.V(2).Infof("PD name=%s, zone=%s", pc.diskMetadata.name, pc.diskMetadata.zone)
		return fmt.Errorf("missing zone or PD name, aborting snapshot")
	}
	// Select snapshot name
	snapshotName := pc.diskMetadata.name
	if *prometheusDiskSnapshotName != "" {
		if err := VerifySnapshotName(*prometheusDiskSnapshotName); err == nil {
			snapshotName = *prometheusDiskSnapshotName
		} else {
			klog.Warningf("Incorrect disk name %v: %v. Using default name: %v", *prometheusDiskSnapshotName, err, snapshotName)
		}
	}
	// Snapshot Prometheus disk
	return wait.Poll(
		gcloudRetryInterval,
		snapshotRetryTimeout,
		func() (bool, error) {
			err := pc.trySnapshotPrometheusDisk(pc.diskMetadata.name, snapshotName, pc.diskMetadata.zone)
			if err != nil {
				klog.Errorf("Trying to snapshot prometheus disk failed: %v", err)
			}
			// Poll() stops on error so returning nil
			return err == nil, nil
		})
}

func (pc *Controller) trySnapshotPrometheusDisk(pdName, snapshotName, zone string) error {
	klog.V(2).Info("Trying to snapshot Prometheus' persistent disk...")
	project := pc.clusterLoaderConfig.PrometheusConfig.SnapshotProject
	if project == "" {
		// This should never happen when run from kubetest with a GCE/GKE Kubernetes
		// provider - kubetest always propagates PROJECT env var in such situations.
		return fmt.Errorf("unknown project - please set --experimental-snapshot-project flag")
	}
	klog.V(2).Infof("Snapshotting PD %q into snapshot %q in project %q in zone %q", pdName, snapshotName, project, zone)
	cmd := exec.Command("gcloud", "compute", "disks", "snapshot", pdName, "--project", project, "--zone", zone, "--snapshot-names", snapshotName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("creating disk snapshot failed: %v\nCommand output: %q", err, string(output))
	}
	klog.V(2).Infof("Creating disk snapshot finished with: %q", string(output))
	return nil
}

func (pc *Controller) deletePrometheusDiskIfEnabled() error {
	if enabled, err := pc.isEnabled(); !enabled {
		return err
	}
	if pc.diskMetadata.name == "" || pc.diskMetadata.zone == "" {
		klog.Errorf("Missing zone or PD name, aborting deletion")
		klog.V(2).Infof("PD name=%s, zone=%s", pc.diskMetadata.name, pc.diskMetadata.zone)
		return fmt.Errorf("missing zone or PD name, aborting deletion")
	}
	// Delete Prometheus disk
	return wait.Poll(
		gcloudRetryInterval,
		deleteRetryTimeout,
		func() (bool, error) {
			err := pc.tryDeletePrometheusDisk(pc.diskMetadata.name, pc.diskMetadata.zone)
			if err != nil {
				klog.Errorf("Trying to delete prometheus disk failed: %v", err)
			}
			// Poll() stops on error so returning nil
			return err == nil, nil
		})
}

func (pc *Controller) tryDeletePrometheusDisk(pdName, zone string) error {
	klog.V(2).Info("Trying to delete Prometheus' persistent disk...")
	project := pc.clusterLoaderConfig.PrometheusConfig.SnapshotProject
	if project == "" {
		// This should never happen when run from kubetest with a GCE/GKE Kubernetes
		// provider - kubetest always propagates PROJECT env var in such situations.
		return fmt.Errorf("unknown project - please set --experimental-snapshot-project flag")
	}
	klog.V(2).Infof("Deleting PD %q in project %q in zone %q", pdName, project, zone)
	cmd := exec.Command("gcloud", "compute", "disks", "delete", pdName, "--project", project, "--zone", zone)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("deleting disk failed: %v\nCommand output: %q", err, string(output))
	}
	klog.V(2).Infof("Deleting disk finished with: %q", string(output))
	return nil
}

func (pc *Controller) EnableTearDownPrometheusStackOnInterrupt() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		// Block until a signal is received.
		s := <-c
		klog.V(2).Infof("Received signal: %v", s)
		if err := pc.snapshotPrometheusDiskIfEnabledSynchronized(); err != nil {
			klog.Warningf("Error while snapshotting prometheus disk: %v", err)
		}
		os.Exit(1)
	}()
}
