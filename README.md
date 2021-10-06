# kubevirt.io.hostpath-provisioner

A special multi-node version of the kubernetes hostpath provisioner. It is available in both legacy (deprecated) and CSI version. The CSI version is inspired by the [kubernetes CSI host path driver](https://github.com/kubernetes-csi/csi-driver-host-path). The kubevirt hostpath CSI driver aims to be a production ready driver. It does not support all the features the kubernetes CSI hostpath driver supports, but that driver is basically a mock test driver.

## Overview CSI provisioner

The CSI implementation is inspired by the [kubernetes CSI host path driver](https://github.com/kubernetes-csi/csi-driver-host-path) however the goal of that implementation is to be a test driver for the kubernetes test suite, as such it implements features to allow the test suite to test if kubernetes properly implements certain CSI features. The goal of this hostpath CSI driver is to implement a production ready hostpath based CSI driver. This means it will not implement block storage or the attachment/publish logic of CSI as hostpath based volumes are by definition already on the node and filesystem based.

### Differences with legacy provisioner
- Support for `kubevirt.io/provisionOnNode` as described below has been removed. Kubernetes is in full control of placing where the volumes are created based on WaitForFirstConsumer logic. Immediate binding mode is no longer supported. CSI Topology is used by kubernetes to determine which node to create a volume on.
- [CSI Storage capacity](https://kubernetes.io/docs/concepts/storage/storage-capacity/) is properly reported to kubernetes so it can determine if there is enough space available on the node to place the workload based on the size request of the PVCs. This requires kubernetes v1.19 or newer.
- Existing PVs created by the legacy provisioner use the hostpath source in the PV spec, while the CSI driver uses the CSI source in the PV spec. This makes them incompatible and will need to be migrated to the new format. The CSI driver will use the csiHandle as the directory name it creates, so we can re-use existing volumes if we create new PVs with the appropriate csiHandle.
- CSI abilities not available to the legacy provisioner.
  - [Volume snapshotting](https://kubernetes.io/docs/concepts/storage/volume-snapshots/) (not implemeneted yet)
  - [CSI cloning](https://kubernetes.io/docs/concepts/storage/volume-pvc-datasource/) (not implemented yet)

### Deployment

The CSI driver is deployed as a daemonset and the pods of the daemonset contain 5 containers:
1. KubeVirt Hostpath CSI driver
2. [CSI external health monitor](https://github.com/kubernetes-csi/external-health-monitor)
3. [External CSI provisioner](https://github.com/kubernetes-csi/external-provisioner)
4. [CSI liveness probe](https://github.com/kubernetes-csi/livenessprobe)
5. [CSI node driver registrar](https://github.com/kubernetes-csi/node-driver-registrar)

Besides the daemonset you will need to create a [storage class](https://github.com/kubevirt/hostpath-provisioner-operator/blob/main/deploy/storageclass-wffc-csi.yaml) that uses WaitForFirstConsumer binding mode, and uses the correct provisioner name. The provisioner name should be `kubevirt.io.hostpath-provisioner` where the legacy provisioner name was `kubevirt.io/hostpath-provisioner`.

You will also need to deploy the appropriate RBAC for the service account used by the daemonset. All of the needed yaml files can be found in the [deploy/csi](deploy/csi) directory. We suggest you deploy them to a specific namespace dedicated to the hostpath csi driver.

Alternatively you can use the [hostpath-provisioner-operator](https://github.com/kubevirt/hostpath-provisioner-operator) to deploy the csi driver. Instructions on how to deploy are available there.

*WARNING* If you select a directory that shares space with your Operating System, you can potentially exhaust the space on that partition and your node will become non-functional. It is recommended you create a separate partition and point the hostpath provisioner there so it will not interfere with your Operating System.

### Deployment in OpenShift
In order to deploy the CSI driver in OpenShift you will need to supply the correct [SecurityContextConstraints](deploy/kubevirt-hostpath-security-constraints-csi.yaml). There is no need to relabel the directory you are creating the volumes in. The CSI driver will take care of that.
## Overview legacy provisioner

This is a special version of the kubernetes hostpath provisioner, it's a slightly modified version of the sig storage [example hostpath provisioner](https://github.com/kubernetes-sigs/sig-storage-lib-external-provisioner/tree/master/examples/hostpath-provisioner).

### Differences

The main differences between this provisioner and the standard hostpath provisioner you may already be familiar with are:
1. Ability to specify the base directory to use on the node(s) for the volume - `PV_DIR`
2. This provisioner is a "node aware" provisioner, in order to provision a claim using this provisioner you must include a node attribute on the claim `kubevirt.io/provisionOnNode: node-01`
3. Or if you do not want to specify the node on the claim, you can specify `volumeBindingMode: WaitForFirstConsumer` in the storage class. Then the PV will be created only when the first Pod using this PVC is scheduled. The PV will be created on the node that the Pod is scheduled on.

_In cases where multiple PVCs are to be used with a Pod it is not recommended to mix the WaitForFirstConsumer binding mode with the provisionOnNode annotation. All of a Pod's PVCs should carry the annotation or none should. Mixing modes can result in PVCs being allocated from different nodes leaving your Pod unschedulable._

### Deployment

The provisioner is deployed as a daemonset, and instance of the provisioner is deployed to each of the worker nodes in the kubernetes cluster. We then disable the use of leader election so that any provisioning request is issues to all of the provisioners in the cluster. Each provisioner then evaluates the provision request based on the Node attribute by filtering out any requests that don't match the Node name for the provisioner pod. In case of `WaitForFirstConsumer` binding mode, the provision request is ignored by all the provisioners until a consumer (Pod) is scheduled. Then, an annotation `volume.kubernetes.io/selected-node` containing the node name where the pod is scheduled on, will be added to the PVC. The provisioners will check if the annotation matches the node it runs on, and only if there is a match the PV will be created.

*WARNING* If you select a directory that shares space with your Operating System, you can potentially exhaust the space on that partition and your node will become non-functional. It is recommended you create a separate partition and point the hostpath provisioner there so it will not interfere with your Operating System.

### Deployment in OpenShift
In order to deploy this provisioner in OpenShift you will need to supply the correct [SecurityContextConstraints](deploy/kubevirt-hostpath-security-constraints.yaml). You will also have to create the appropriate selinux rules to allow the pod to write to the path on the host. Our examples use /var/hpvolumes as the path on the host, if you have modified the path change it for this command as well.

```bash
$ sudo chcon -t container_file_t -R /var/hpvolumes
```

### Systemd
If you are running worker nodes that are running systemd, we have provided a [service file](deploy/systemd/hostpath-provisioner.service) that you can install in /etc/systemd/system/hostpath-provisioner.service to have it set the SElinux labeling at start-up
