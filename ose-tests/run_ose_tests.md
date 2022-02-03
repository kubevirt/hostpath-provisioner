# Running Open Shift CSI certification tests

### Prerequisites

Have a running Open Shift cluster. And store a kubeconfig file in this directory named `kubeconfig.yaml` This will be passed as the KUBECONFIG parameter in the command below. The `storageclass.yaml` and `csi_cert_os.yaml` are needed to configure which tests to run and what storage class to use.

Pull the ose-tests container from the Red Hat registry.

### Command
Run the below command to execute the tests against your Open Shift cluster.

```bash
podman run -v `pwd`:/data:z --rm -it registry.redhat.io/openshift4/ose-tests sh -c "KUBECONFIG=/data/kubeconfig.yaml TEST_CSI_DRIVER_FILES=/data/csi_cert_os.yaml /usr/bin/openshift-tests run openshift/csi --junit-dir /data/results"
```

The results will be stored in the results directory.

