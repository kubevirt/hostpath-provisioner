# kubevirt.io/hostpath-provisioner

A special multi-node version of the kubernetes hostpath provisioner.

## Overview

This is a special version of the kubernetes hostpath provisioner, it's a slightly modified version of the sig storage [example hostpath prvisioner](https://github.com/kubernetes-sigs/sig-storage-lib-external-provisioner/tree/master/examples/hostpath-provisioner).

## Differences

The main differences between this provisioner and the standard hostpath provisioner you may already be familiar with are:
1. Ability to specify the base directory to use on the node(s) for the volume - `PV_DIR`
2. This provisioner is a "node aware" provisioner, in order to provision a claim using this provisioner you must include a node attribute on the claim `kubevirt.io/provisionOnNode: node-01`

## Deployment

The provisioner is deployed as a daemonset, and instance of the provisioner is deployed to each of the worker nodes in the kubernetes cluster.  We then disable the use of leader election so that any provisioning request is issues to all of the provsioners in the cluster.  Each provisioner then evaluates the provision request based on the Node attribute by filtering out any requests that don't match the Node name for the provisioner pod.
