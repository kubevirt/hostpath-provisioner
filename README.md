# kubevirt.io/hostpath-provisioner

A special multi-node version of the kubernetes hostpath provisioner.

## Overview

This is a special version of the kubernetes hostpath provisioner, it's a slightly modified version of the sig storage [example hostpath provisioner](https://github.com/kubernetes-sigs/sig-storage-lib-external-provisioner/tree/master/examples/hostpath-provisioner).

## Differences

The main differences between this provisioner and the standard hostpath provisioner you may already be familiar with are:
1. Ability to specify the base directory to use on the node(s) for the volume - `PV_DIR`
2. This provisioner is a "node aware" provisioner, in order to provision a claim using this provisioner you must include a node attribute on the claim `kubevirt.io/provisionOnNode: node-01`
3. Or if you do not want to specify the node on the claim, you can specify `volumeBindingMode: WaitForFirstConsumer` in the storage class. Then the PV will be created only when the first Pod using this PVC is scheduled. The PV will be created on the node that the Pod is scheduled on.
Still, the annotation `kubevirt.io/provisionOnNode` can be used in this mode, though it will not wait for the first consumer.

_In cases where multiple PVCs are to be used with a Pod it is not recommended to mix the WaitForFirstConsumer binding mode with the provisionOnNode annotation. All of a Pod's PVCs should carry the annotation or none should. Mixing modes can result in PVCs being allocated from different nodes leaving your Pod unschedulable._

## Deployment

The provisioner is deployed as a daemonset, and instance of the provisioner is deployed to each of the worker nodes in the kubernetes cluster. We then disable the use of leader election so that any provisioning request is issues to all of the provisioners in the cluster. Each provisioner then evaluates the provision request based on the Node attribute by filtering out any requests that don't match the Node name for the provisioner pod. In case of `WaitForFirstConsumer` binding mode, the provision request is ignored by all the provisioners until a consumer (Pod) is scheduled. Then, an annotation `volume.kubernetes.io/selected-node` containing the node name where the pod is scheduled on, will be added to the PVC. The provisioners will check if the annotation matches the node it runs on, and only if there is a match the PV will be created.

*WARNING* If you select a directory that shares space with your Operating System, you can potentially exhaust the space on that partition and your node will become non-functional. It is recommended you create a separate partition and point the hostpath provisioner there so it will not interfere with your Operating System

### Deployment in OpenShift
In order to deploy this provisioner in OpenShift you will need to supply the correct SecurityContextConstraints. A minimal needed one is supplied in the [deploy](./deploy) directory. You will also have to create the appropriate selinux rules to allow the pod to write to the path on the host. Our examples use /var/hpvolumes as the path on the host, if you have modified the path change it for this command as well.

```bash
$ sudo chcon -t container_file_t -R /var/hpvolumes
```

### Systemd
If you are running worker nodes that are running systemd, we have provided a [service file](deploy/systemd/hostpath-provisioner.service) that you can install in /etc/systemd/system/hostpath-provisioner.service to have it set the SElinux labeling at start-up
