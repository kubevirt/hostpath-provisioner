# kubevirt.io/hostpath-provisioner

Yet another hostpath provisioner.  This is a slightly modified version of the example kubernetes hostpath provisioner.  The goal is to provide a simple to deploy light weight and portable provisioner to use with kubevirt.  Like the standard hostpat provisioner the kubevirt.io/hostpath-provisioner dynamically creates bind-mounts on a node; it is therefore unable to do things like migrate a pod from one node to another with a pvc.
