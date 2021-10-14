/*
Copyright 2018 The Kubernetes Authors.

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
	"errors"
	"flag"
	"os"
	"path"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"

	"github.com/golang/glog"
	"kubevirt.io/hostpath-provisioner/controller"

	v1 "k8s.io/api/core/v1"
	storage "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	defaultProvisionerName = "kubevirt.io/hostpath-provisioner"
	annStorageProvisioner  = "volume.beta.kubernetes.io/storage-provisioner"
)

var provisionerName string

type hostPathProvisioner struct {
	pvDir           string
	identity        string
	nodeName        string
	useNamingPrefix bool
}

// Common allocation units
const (
	KiB int64 = 1024
	MiB int64 = 1024 * KiB
	GiB int64 = 1024 * MiB
	TiB int64 = 1024 * GiB
)

var provisionerID string

// NewHostPathProvisioner creates a new hostpath provisioner
func NewHostPathProvisioner() controller.Provisioner {
	useNamingPrefix := false
	nodeName := os.Getenv("NODE_NAME")
	if nodeName == "" {
		glog.Fatal("env variable NODE_NAME must be set so that this provisioner can identify itself")
	}

	// note that the pvDir variable informs us *where* the provisioner should be writing backing files to
	// this needs to match the path speciied in the volumes.hostPath spec of the deployment
	pvDir := os.Getenv("PV_DIR")
	if pvDir == "" {
		glog.Fatal("env variable PV_DIR must be set so that this provisioner knows where to place its data")
	}
	if strings.ToLower(os.Getenv("USE_NAMING_PREFIX")) == "true" {
		useNamingPrefix = true
	}
	glog.Infof("initiating kubevirt/hostpath-provisioner on node: %s\n", nodeName)
	provisionerName = "kubevirt.io/hostpath-provisioner"
	return &hostPathProvisioner{
		pvDir:           pvDir,
		identity:        provisionerName,
		nodeName:        nodeName,
		useNamingPrefix: useNamingPrefix,
	}
}

var _ controller.Provisioner = &hostPathProvisioner{}

func isCorrectNodeByBindingMode(annotations map[string]string, nodeName string, bindingMode storage.VolumeBindingMode) bool {
	glog.Infof("isCorrectNodeByBindingMode mode: %s", string(bindingMode))
	if _, ok := annotations["kubevirt.io/provisionOnNode"]; ok {
		if isCorrectNode(annotations, nodeName, "kubevirt.io/provisionOnNode") {
			annotations[annStorageProvisioner] = defaultProvisionerName
			return true
		}
		return false
	} else if bindingMode == storage.VolumeBindingWaitForFirstConsumer {
		return isCorrectNode(annotations, nodeName, "volume.kubernetes.io/selected-node")
	}
	return false
}

func isCorrectNode(annotations map[string]string, nodeName string, annotationName string) bool {
	if val, ok := annotations[annotationName]; ok {
		glog.Infof("claim included %s annotation: %s\n", annotationName, val)
		if val == nodeName {
			glog.Infof("matched %s: %s with this node: %s\n", annotationName, val, nodeName)
			return true
		}
		glog.Infof("no match for %s: %s with this node: %s\n", annotationName, val, nodeName)
		return false
	}
	glog.Infof("missing %s annotation, skipping operations for pvc", annotationName)
	return false
}

func (p *hostPathProvisioner) ShouldProvision(pvc *v1.PersistentVolumeClaim, bindingMode *storage.VolumeBindingMode) bool {
	shouldProvision := isCorrectNodeByBindingMode(pvc.GetAnnotations(), p.nodeName, *bindingMode)

	if shouldProvision {
		pvCapacity, err := calculatePvCapacity(p.pvDir)
		if pvCapacity != nil && pvCapacity.Cmp(pvc.Spec.Resources.Requests[v1.ResourceName(v1.ResourceStorage)]) < 0 {
			glog.Error("PVC request size larger than total possible PV size")
			shouldProvision = false
		} else if err != nil {
			glog.Errorf("Unable to determine pvCapacity %v", err)
			shouldProvision = false
		}
	}
	return shouldProvision
}

// Provision creates a storage asset and returns a PV object representing it.
func (p *hostPathProvisioner) Provision(options controller.ProvisionOptions) (*v1.PersistentVolume, error) {
	vPath := path.Join(p.pvDir, options.PVName)
	pvCapacity, err := calculatePvCapacity(p.pvDir)
	if p.useNamingPrefix {
		vPath = path.Join(p.pvDir, options.PVC.Name+"-"+options.PVName)
	}

	if pvCapacity != nil {
		glog.Infof("creating backing directory: %v", vPath)

		if err := os.MkdirAll(vPath, 0777); err != nil {
			return nil, err
		}

		pv := &v1.PersistentVolume{
			ObjectMeta: metav1.ObjectMeta{
				Name: options.PVName,
				Annotations: map[string]string{
					"hostPathProvisionerIdentity": p.identity,
					"kubevirt.io/provisionOnNode": p.nodeName,
				},
			},
			Spec: v1.PersistentVolumeSpec{
				PersistentVolumeReclaimPolicy: v1.PersistentVolumeReclaimDelete,
				AccessModes: []v1.PersistentVolumeAccessMode{
					v1.ReadWriteOnce,
				},
				Capacity: v1.ResourceList{
					v1.ResourceName(v1.ResourceStorage): *pvCapacity,
				},
				PersistentVolumeSource: v1.PersistentVolumeSource{
					HostPath: &v1.HostPathVolumeSource{
						Path: vPath,
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
											p.nodeName,
										},
									},
								},
							},
						},
					},
				},
			},
		}
		return pv, nil
	}
	return nil, err
}

// Delete removes the storage asset that was created by Provision represented
// by the given PV.
func (p *hostPathProvisioner) Delete(volume *v1.PersistentVolume) error {
	ann, ok := volume.Annotations["hostPathProvisionerIdentity"]
	if !ok {
		return errors.New("identity annotation not found on PV")
	}
	if ann != p.identity {
		return &controller.IgnoredError{Reason: "identity annotation on PV does not match ours"}
	}
	if !isCorrectNode(volume.Annotations, p.nodeName, "kubevirt.io/provisionOnNode") {
		return &controller.IgnoredError{Reason: "identity annotation on pvc does not match ours, not deleting PV"}
	}

	path := volume.Spec.PersistentVolumeSource.HostPath.Path
	glog.Infof("removing backing directory: %v", path)
	if err := os.RemoveAll(path); err != nil {
		return err
	}

	return nil
}

func calculatePvCapacity(path string) (*resource.Quantity, error) {
	statfs := &unix.Statfs_t{}
	err := unix.Statfs(path, statfs)
	if err != nil {
		return nil, err
	}
	// Capacity is total block count * block size
	quantity := resource.NewQuantity(int64(roundDownCapacityPretty(int64(statfs.Blocks)*int64(statfs.Bsize))), resource.BinarySI)
	return quantity, nil
}

// Round down the capacity to an easy to read value. Blatantly stolen from here: https://github.com/kubernetes-incubator/external-storage/blob/master/local-volume/provisioner/pkg/discovery/discovery.go#L339
func roundDownCapacityPretty(capacityBytes int64) int64 {

	easyToReadUnitsBytes := []int64{GiB, MiB}

	// Round down to the nearest easy to read unit
	// such that there are at least 10 units at that size.
	for _, easyToReadUnitBytes := range easyToReadUnitsBytes {
		// Round down the capacity to the nearest unit.
		size := capacityBytes / easyToReadUnitBytes
		if size >= 10 {
			return size * easyToReadUnitBytes
		}
	}
	return capacityBytes
}

func main() {
	syscall.Umask(0)

	flag.Parse()
	flag.Set("logtostderr", "true")

	// Create an InClusterConfig and use it to create a client for the controller
	// to use to communicate with Kubernetes
	config, err := rest.InClusterConfig()
	if err != nil {
		glog.Fatalf("Failed to create config: %v", err)
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		glog.Fatalf("Failed to create client: %v", err)
	}

	// The controller needs to know what the server version is because out-of-tree
	// provisioners aren't officially supported until 1.5
	serverVersion, err := clientset.Discovery().ServerVersion()
	if err != nil {
		glog.Fatalf("Error getting server version: %v", err)
	}

	// Create the provisioner: it implements the Provisioner interface expected by
	// the controller
	hostPathProvisioner := NewHostPathProvisioner()

	glog.Infof("creating provisioner controller with name: %s\n", provisionerName)
	// Start the provision controller which will dynamically provision hostPath
	// PVs
	pc := controller.NewProvisionController(clientset, provisionerName, hostPathProvisioner, serverVersion.GitVersion)
	pc.Run(wait.NeverStop)
}
