/*
Copyright 2021 The Kubernetes Authors.

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
	"testing"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func Test_nameAndZone(t *testing.T) {
	tests := []struct {
		name     string
		pv       corev1.PersistentVolume
		wantName string
		wantZone string
		wantErr  bool
	}{
		{
			name: "gce",
			pv: corev1.PersistentVolume{
				ObjectMeta: v1.ObjectMeta{
					Labels: map[string]string{
						"topology.kubernetes.io/zone": "us-zone-9",
					},
				},
				Spec: corev1.PersistentVolumeSpec{
					PersistentVolumeSource: corev1.PersistentVolumeSource{
						GCEPersistentDisk: &corev1.GCEPersistentDiskVolumeSource{
							PDName: "name-1",
						},
					},
				},
			},
			wantName: "name-1",
			wantZone: "us-zone-9",
		},
		{
			name: "gce legacy label",
			pv: corev1.PersistentVolume{
				ObjectMeta: v1.ObjectMeta{
					Labels: map[string]string{
						"failure-domain.beta.kubernetes.io/zone": "us-zone-9",
					},
				},
				Spec: corev1.PersistentVolumeSpec{
					PersistentVolumeSource: corev1.PersistentVolumeSource{
						GCEPersistentDisk: &corev1.GCEPersistentDiskVolumeSource{
							PDName: "name-1",
						},
					},
				},
			},
			wantName: "name-1",
			wantZone: "us-zone-9",
		},
		{
			name: "csi",
			pv: corev1.PersistentVolume{
				Spec: corev1.PersistentVolumeSpec{
					PersistentVolumeSource: corev1.PersistentVolumeSource{
						CSI: &corev1.CSIPersistentVolumeSource{
							VolumeHandle: "projects/project-1234/zones/us-zone-9/disks/name-1",
						},
					},
				},
			},
			wantName: "name-1",
			wantZone: "us-zone-9",
		},
		{
			name: "csi bad volumehandle",
			pv: corev1.PersistentVolume{
				Spec: corev1.PersistentVolumeSpec{
					PersistentVolumeSource: corev1.PersistentVolumeSource{
						CSI: &corev1.CSIPersistentVolumeSource{
							VolumeHandle: "blah",
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "unknown",
			pv: corev1.PersistentVolume{
				Spec: corev1.PersistentVolumeSpec{
					PersistentVolumeSource: corev1.PersistentVolumeSource{
						Glusterfs: &corev1.GlusterfsPersistentVolumeSource{},
					},
				},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotName, gotZone, err := nameAndZone(tt.pv)
			if (err != nil) != tt.wantErr {
				t.Errorf("nameAndZone() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if gotName != tt.wantName {
				t.Errorf("nameAndZone() gotName = %v, want %v", gotName, tt.wantName)
			}
			if gotZone != tt.wantZone {
				t.Errorf("nameAndZone() gotZone = %v, want %v", gotZone, tt.wantZone)
			}
		})
	}
}
