/*
Copyright 2021 The hostpath provisioner Authors.

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

package main

import (
	"io/ioutil"
	"os"
	"testing"

	"golang.org/x/sys/unix"

	v1 "k8s.io/api/core/v1"
	storage "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func getKubevirtNodeAnnotation(value string) map[string]string {
	annotation := make(map[string]string)
	if value != "" {
		annotation["kubevirt.io/provisionOnNode"] = value
	}
	return annotation
}

func getSelectedNodeAnnotation(value string) map[string]string {
	annotation := make(map[string]string)
	if value != "" {
		annotation["volume.kubernetes.io/selected-node"] = value
	}
	return annotation
}

func Test_isCorrectNode(t *testing.T) {
	type args struct {
		annotations map[string]string
		nodeName    string
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "passes with correct node annotation",
			args: args{
				annotations: getKubevirtNodeAnnotation("test-node"),
				nodeName:    "test-node",
			},
			want: true,
		},
		{
			name: "skips with incorrect node annotation",
			args: args{
				annotations: getKubevirtNodeAnnotation("test-node"),
				nodeName:    "wrong-node",
			},
			want: false,
		},
		{
			name: "skips with no node annotation",
			args: args{
				annotations: getKubevirtNodeAnnotation(""),
				nodeName:    "test-node",
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isCorrectNode(tt.args.annotations, tt.args.nodeName, "kubevirt.io/provisionOnNode"); got != tt.want {
				t.Errorf("isCorrectNode() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_isCorrectNodeByBindingMode(t *testing.T) {
	multiAnnotation := getKubevirtNodeAnnotation("test-node")
	multiAnnotation["volume.kubernetes.io/selected-node"] = "other-nodex"
	type args struct {
		annotations map[string]string
		nodeName    string
		bindingMode storage.VolumeBindingMode
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "passes with correct node annotation, binding immediate",
			args: args{
				annotations: getKubevirtNodeAnnotation("test-node"),
				nodeName:    "test-node",
				bindingMode: storage.VolumeBindingImmediate,
			},
			want: true,
		},
		{
			name: "skips with incorrect kubevirt node annotation",
			args: args{
				annotations: getKubevirtNodeAnnotation("test-node"),
				nodeName:    "wrong-node",
				bindingMode: storage.VolumeBindingImmediate,
			},
			want: false,
		},
		{
			name: "skips with no kubevirt node annotation",
			args: args{
				annotations: getKubevirtNodeAnnotation(""),
				nodeName:    "test-node",
				bindingMode: storage.VolumeBindingImmediate,
			},
			want: false,
		},
		{
			name: "passes with correct selected node annotation, binding waitForFirst",
			args: args{
				annotations: getSelectedNodeAnnotation("test-node"),
				nodeName:    "test-node",
				bindingMode: storage.VolumeBindingWaitForFirstConsumer,
			},
			want: true,
		},
		{
			name: "passes with correct kubevirt node annotation, binding waitForFirst",
			args: args{
				annotations: getKubevirtNodeAnnotation("test-node"),
				nodeName:    "test-node",
				bindingMode: storage.VolumeBindingWaitForFirstConsumer,
			},
			want: true,
		},
		{
			name: "skips with no selected node annotation binding waitForFirst",
			args: args{
				annotations: getSelectedNodeAnnotation(""),
				nodeName:    "test-node",
				bindingMode: storage.VolumeBindingWaitForFirstConsumer,
			},
			want: false,
		},
		{
			name: "passes with precendence to kubevirt annotation over kubernetes annotation",
			args: args{
				annotations: multiAnnotation,
				nodeName:    "test-node",
				bindingMode: storage.VolumeBindingWaitForFirstConsumer,
			},
			want: true,
		},
		{
			name: "passes with precendence to kubevirt annotation over kubernetes annotation",
			args: args{
				annotations: multiAnnotation,
				nodeName:    "other-node",
				bindingMode: storage.VolumeBindingWaitForFirstConsumer,
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isCorrectNodeByBindingMode(tt.args.annotations, tt.args.nodeName, tt.args.bindingMode); got != tt.want {
				t.Errorf("isCorrectNodeByBindingMode() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_Delete(t *testing.T) {
	type args struct {
		identity string
		nodeName string
	}
	testProvisioner := &hostPathProvisioner{
		nodeName: "testNode",
		identity: "testId",
	}

	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "Delete not matching identity",
			args: args{
				identity: "anotherId",
				nodeName: "testNode",
			},
			wantErr: true,
		},
		{
			name: "Delete not matching node",
			args: args{
				identity: "testId",
				nodeName: "anotherNode",
			},
			wantErr: true,
		},
		{
			name: "Delete matching",
			args: args{
				identity: "testId",
				nodeName: "testNode",
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		file, err := ioutil.TempFile("", "test")
		if err != nil {
			t.Errorf("Unable to create temporary directory, error = %v", err)
		}
		defer os.Remove(file.Name())
		pv := createPv(tt.args.identity, tt.args.nodeName, file.Name())
		t.Run(tt.name, func(t *testing.T) {
			err := testProvisioner.Delete(pv)
			if (err != nil) != tt.wantErr || (err == nil) == tt.wantErr {
				t.Errorf("Delete, error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}

func Test_calculatePvCapacity(t *testing.T) {
	type args struct {
		path string
	}
	// Get total size of wherever we are running.
	capacity, _ := getTotalCapacity(".")
	// Do the round down same as the calculation so we can compare
	constQuantity := roundDownCapacityPretty(capacity)

	tests := []struct {
		name    string
		args    args
		want    int64
		wantErr bool
	}{
		{
			name: "Reports correct size for known directory",
			args: args{
				path: ".",
			},
			want:    constQuantity,
			wantErr: false,
		},
		{
			name: "Reports error for invalid directory",
			args: args{
				path: "/doesntexist",
			},
			want:    constQuantity,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := calculatePvCapacity(tt.args.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("calculatePvCapacity() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err == nil && got.CmpInt64(tt.want) != 0 {
				t.Errorf("calculatePvCapacity() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_roundDownCapacityPretty(t *testing.T) {
	type args struct {
		size int64
	}

	tests := []struct {
		name    string
		args    args
		want    int64
		wantErr bool
	}{
		{
			name: "Rounds Gigs properly",
			args: args{
				size: int64(2 * GiB),
			},
			want:    int64(2 * GiB),
			wantErr: false,
		},
		{
			name: "Rounds Gigs properly with minor add",
			args: args{
				size: int64((2 * GiB) + 2),
			},
			want:    int64(2 * GiB),
			wantErr: false,
		},
		{
			name: "Not large enough for GiB, rounded down to smaller MiB",
			args: args{
				size: int64((2 * GiB) - 2),
			},
			want:    int64(2047 * MiB),
			wantErr: false,
		},
		{
			name: "Large GiB, rounded down to one smaller GiB",
			args: args{
				size: int64((20 * GiB) - 2),
			},
			want:    int64(19 * GiB),
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := roundDownCapacityPretty(tt.args.size)
			if got != tt.want {
				t.Errorf("calculatePvCapacity() = %d, want %d", got, tt.want)
			}
		})
	}
}

func getTotalCapacity(path string) (int64, error) {
	statfs := &unix.Statfs_t{}
	err := unix.Statfs(path, statfs)
	if err != nil {
		return int64(-1), err
	}

	// Capacity is total block count * block size
	return int64(statfs.Blocks) * statfs.Bsize, nil
}

func createPv(identity, nodeName, dirPath string) *v1.PersistentVolume {
	return &v1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pv",
			Annotations: map[string]string{
				"hostPathProvisionerIdentity": identity,
				"kubevirt.io/provisionOnNode": nodeName,
			},
		},
		Spec: v1.PersistentVolumeSpec{
			PersistentVolumeReclaimPolicy: v1.PersistentVolumeReclaimDelete,
			AccessModes: []v1.PersistentVolumeAccessMode{
				v1.ReadWriteOnce,
			},
			Capacity: v1.ResourceList{
				v1.ResourceName(v1.ResourceStorage): resource.MustParse("2Gi"),
			},
			PersistentVolumeSource: v1.PersistentVolumeSource{
				HostPath: &v1.HostPathVolumeSource{
					Path: dirPath,
				},
			},
			NodeAffinity: &v1.VolumeNodeAffinity{
				Required: &v1.NodeSelector{
					NodeSelectorTerms: []v1.NodeSelectorTerm{
						{
							MatchExpressions: []v1.NodeSelectorRequirement{
								{
									Key:      "kubernetes.io/hostname",
									Operator: v1.NodeSelectorOpIn,
									Values: []string{
										nodeName,
									},
								},
							},
						},
					},
				},
			},
		},
	}
}
