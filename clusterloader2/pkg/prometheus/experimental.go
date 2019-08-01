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
	"encoding/json"
	"fmt"
	"os/exec"
	"time"

	"github.com/spf13/pflag"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog"
)

var (
	shouldSnapshotPrometheusDisk = pflag.Bool("experimental-gcp-snapshot-prometheus-disk", false, "(experimental, provider=gce|gke only) whether to snapshot Prometheus disk before Prometheus stack is torn down")
	prometheusDiskSnapshotName   = pflag.String("experimental-prometheus-disk-snapshot-name", "", "Name of the prometheus disk snapshot that will be created if snapshots are enabled. If not set, the prometheus disk name will be used.")
)

func (pc *PrometheusController) snapshotPrometheusDiskIfEnabled() error {
	if !*shouldSnapshotPrometheusDisk {
		return nil
	}
	if pc.provider != "gce" && pc.provider != "gke" && pc.provider != "kubemark" {
		return fmt.Errorf(
			"snapshotting Prometheus' disk only available for GCP providers (gce, gke, kubemark), provider is: %s", pc.provider)
	}
	return wait.Poll(
		10*time.Second,
		2*time.Minute,
		pc.trySnapshotPrometheusDisk)
}

func (pc *PrometheusController) trySnapshotPrometheusDisk() (bool, error) {
	klog.Info("Trying to snapshot Prometheus' persistent disk...")
	k8sClient := pc.framework.GetClientSets().GetClient()
	list, err := k8sClient.CoreV1().PersistentVolumes().List(metav1.ListOptions{})
	if err != nil {
		return false, err
	}
	var pdName, zone string
	for _, pv := range list.Items {
		if pv.Spec.ClaimRef.Name != "prometheus-k8s-db-prometheus-k8s-0" {
			continue
		}
		klog.Infof("Found Prometheus' PV with name: %s", pv.Name)
		pdName = pv.Spec.GCEPersistentDisk.PDName
		zone = pv.ObjectMeta.Labels["failure-domain.beta.kubernetes.io/zone"]
		klog.Infof("PD name=%s, zone=%s", pdName, zone)
	}
	if pdName == "" || zone == "" {
		klog.Warningf("missing zone or PD name, aborting")
		klog.Info("PV list was:")
		s, err := json.MarshalIndent(list, "" /*=prefix*/, "  " /*=indent*/)
		if err != nil {
			klog.Warningf("Error while marshalling response %v: %v", list, err)
			return true, err
		}
		klog.Info(string(s))
		return true, nil
	}
	snapshotName := pdName
	if *prometheusDiskSnapshotName != "" {
		if err := VerifySnapshotName(*prometheusDiskSnapshotName); err == nil {
			snapshotName = *prometheusDiskSnapshotName
		} else {
			klog.Warningf("Incorrect disk name %v: %v. Using default name: %v", *prometheusDiskSnapshotName, err, pdName)
		}
	}
	klog.Infof("Snapshotting PD '%s' into snapshot '%s' in zone '%s'", pdName, snapshotName, zone)
	cmd := exec.Command("gcloud", "compute", "disks", "snapshot", pdName, "--zone", zone, "--snapshot-names", snapshotName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		klog.Errorf("Creating disk snapshot failed: %v\nCommand output: %q", err, string(output))
	} else {
		klog.Infof("Creating disk snapshot finished with: %q", string(output))
	}
	return true, err
}
