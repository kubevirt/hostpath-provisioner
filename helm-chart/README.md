# kubevirt-hostpath-provisioner

![Version: 0.1.0](https://img.shields.io/badge/Version-0.1.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.18.0](https://img.shields.io/badge/AppVersion-0.18.0-informational?style=flat-square)

A Helm chart for Kubernetes

## Usage

Helm must be installed to use the charts. Please refer to Helm's documentation to get started.

```bash
helm install kubevirt-hostpath-provisioner /path/to/kubevirt-hostpath-provisioner-0.1.0.tgz --namespace kubevirt-hostpath-provisioner
```

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| affinity | object | `{}` |  |
| image.pullPolicy | string | `"IfNotPresent"` |  |
| image.repository | string | `"quay.io/kubevirt/hostpath-provisioner"` |  |
| image.tag | string | `"latest"` |  |
| imagePullSecrets | list | `[]` |  |
| kubevirt.namePrefix | string | `"\"false\""` | change to '"true"', to have the name of the pvc be part of the directory |
| kubevirt.nodeSelector | object | `{}` | add a nodeSelector to the volume |
| kubevirt.reclaimPolicy | string | `"Delete"` | reclaimpolicy for pv |
| kubevirt.volumePath | string | `"/media/kubevirt-hpp"` | path to the volume on the host |
| nodeSelector | object | `{}` |  |
| podAnnotations | object | `{}` |  |
| podLabels | object | `{}` |  |
| podSecurityContext | object | `{}` |  |
| rbac.create | bool | `true` |  |
| resources | object | `{}` |  |
| securityContext | object | `{}` |  |
| serviceAccount.annotations | object | `{}` |  |
| serviceAccount.automount | bool | `true` |  |
| serviceAccount.create | bool | `true` |  |
| serviceAccount.name | string | `"kubevirt-hostpath-provisioner-admin"` | The name of the service account to use |
| tolerations | list | `[]` |  |
| volumeMounts | list | `[]` |  |
| volumes | list | `[]` |  |

----------------------------------------------