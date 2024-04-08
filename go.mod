module kubevirt.io/hostpath-provisioner

go 1.20

require (
	github.com/container-storage-interface/spec v1.9.0
	github.com/golang/protobuf v1.5.4
	github.com/kubernetes-csi/csi-lib-utils v0.16.0
	github.com/kubernetes-csi/csi-test/v4 v4.4.0
	github.com/kubevirt/monitoring/pkg/metrics/parser v0.0.0-20230710120526-cc1644c90b64
	github.com/machadovilaca/operator-observability v0.0.18
	github.com/onsi/gomega v1.28.0
	github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring v0.71.2
	github.com/prometheus/client_golang v1.19.0
	github.com/prometheus/client_model v0.6.0
	golang.org/x/net v0.24.0
	golang.org/x/sys v0.19.0
	golang.org/x/time v0.5.0
	google.golang.org/grpc v1.63.0
	k8s.io/api v0.28.4
	k8s.io/apiextensions-apiserver v0.28.4
	k8s.io/apimachinery v0.28.4
	k8s.io/client-go v12.0.0+incompatible
	k8s.io/klog/v2 v2.120.1
	k8s.io/kubernetes v1.15.0-alpha.0
	k8s.io/utils v0.0.0-20231127182322-b307cd553661
	kubevirt.io/hostpath-provisioner-operator v0.18.0
	sigs.k8s.io/controller-runtime v0.16.3
	sigs.k8s.io/sig-storage-lib-external-provisioner/v6 v6.3.0
)

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.2.0 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/emicklei/go-restful/v3 v3.11.0 // indirect
	github.com/evanphx/json-patch/v5 v5.6.0 // indirect
	github.com/fsnotify/fsnotify v1.6.0 // indirect
	github.com/go-logr/logr v1.4.1 // indirect
	github.com/go-openapi/jsonpointer v0.19.6 // indirect
	github.com/go-openapi/jsonreference v0.20.2 // indirect
	github.com/go-openapi/swag v0.22.3 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/google/gnostic-models v0.6.8 // indirect
	github.com/google/go-cmp v0.6.0 // indirect
	github.com/google/gofuzz v1.2.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/imdario/mergo v0.3.12 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/miekg/dns v1.1.29 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/nxadm/tail v1.4.8 // indirect
	github.com/onsi/ginkgo v1.16.5 // indirect
	github.com/openshift/custom-resource-status v1.1.2 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/prometheus/common v0.48.0 // indirect
	github.com/prometheus/procfs v0.12.0 // indirect
	github.com/spf13/pflag v1.0.5 // indirect
	golang.org/x/crypto v0.22.0 // indirect
	golang.org/x/exp v0.0.0-20220827204233-334a2380cb91 // indirect
	golang.org/x/oauth2 v0.17.0 // indirect
	golang.org/x/term v0.19.0 // indirect
	golang.org/x/text v0.14.0 // indirect
	golang.org/x/xerrors v0.0.0-20220907171357-04be3eba64a2 // indirect
	gomodules.xyz/jsonpatch/v2 v2.4.0 // indirect
	google.golang.org/appengine v1.6.8 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240227224415-6ceb2ff114de // indirect
	google.golang.org/protobuf v1.33.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/tomb.v1 v1.0.0-20141024135613-dd632973f1e7 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	k8s.io/component-base v0.28.4 // indirect
	k8s.io/kube-openapi v0.0.0-20230717233707-2695361300d9 // indirect
	sigs.k8s.io/json v0.0.0-20221116044647-bc3834ca7abd // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.4.1 // indirect
	sigs.k8s.io/yaml v1.4.0 // indirect
)

replace (
	github.com/gogo/protobuf => github.com/gogo/protobuf v1.3.2
	github.com/onsi/ginkgo => github.com/onsi/ginkgo v1.12.1
	github.com/onsi/gomega => github.com/onsi/gomega v1.10.1
	gopkg.in/yaml.v2 => gopkg.in/yaml.v2 v2.4.0
	k8s.io/client-go => k8s.io/client-go v0.28.3

)

replace github.com/spf13/pflag => github.com/spf13/pflag v1.0.5

replace github.com/docker/docker => github.com/moby/moby v0.7.3-0.20190826074503-38ab9da00309 // Required by Helm

replace github.com/prometheus/prometheus => github.com/prometheus/prometheus v0.0.0-20190424153033-d3245f150225
