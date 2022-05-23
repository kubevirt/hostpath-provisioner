# Introduction

Let's start with the relationship between several related projects:

* **Kubernetes** is a container orchestration system, and is used to run
  containers on a cluster
* **hostpath-provisioner (hpp)** is a storage provisioner provides filesystem based local storage to kubernetes. The hpp is a fully compatible CSI driver.

As an add-on to Kubernetes, hpp shares some philosophy and design choices:

* Mostly written in golang
* Often related to distributed microservice architectures
* Declarative and Reactive (Operator pattern) approach

This short page shall help to get started with the projects and topics
surrounding them.  If you notice a strong similarity with the [KubeVirt contribution guidelines](https://github.com/kubevirt/kubevirt/blob/main/CONTRIBUTING.md) it's because we have taken inspiration from their success.


## Contributing to hostpath provisioner

### Our workflow

Contributing to the hostpath provisioner should be as simple as possible. Have a question? Want
to discuss something? Want to contribute something? Just open an
[Issue](https://github.com/kubevirt/hostpath-provisioner/issues) or a [Pull
Request](https://github.com/kubevirt/hostpath-provisioner/pulls).  For discussion, we use the [KubeVirt Google Group](https://groups.google.com/forum/#!forum/kubevirt-dev).

If you spot a bug or want to change something pretty simple, just go
ahead and open an Issue and/or a Pull Request, including your changes
at [kubevirt/hostpath-provisioner](https://github.com/kubevirt/hostpath-provisioner).

For bigger changes, please create a tracker Issue, describing what you want to
do. Then either as the first commit in a Pull Request, or as an independent
Pull Request, provide an **informal** design proposal of your intended changes.

### Getting started

To make yourself comfortable with the code, you might want to work on some
Issues marked with one or more of the following labels
[help wanted](https://github.com/kubevirt/hostpath-provisioner/labels/help%20wanted),
[good first issue](https://github.com/kubevirt/hostpath-provisioner/labels/good%20first%20issue),
or [bug](https://github.com/kubevirt/hostpath-provisioner/labels/kind%2Fbug).
Any help is greatly appreciated.

### Testing

**Untested features do not exist**. To ensure that what we code really works,
relevant flows should be covered via unit tests and functional tests. So when
thinking about a contribution, also think about testability. All tests can be
run local without the need of CI.

### Getting your code reviewed/merged

Maintainers are here to help you enabling your use-case in a reasonable amount
of time. The maintainers will try to review your code and give you productive
feedback in a reasonable amount of time. However, if you are blocked on a
review, or your Pull Request does not get the attention you think it deserves,
reach out for us via Comments in your Issues, or ping us on
[Slack](https://kubernetes.slack.com/messages/kubevirt-dev).

Maintainers are:

* @awels

### PR Checklist

Before your PR can be merged it must meet the following criteria:
* [README.md](README.md) has been updated if core functionality is affected.
* Complex features need standalone documentation in a tracker issue that can be closed by a PR.

## Projects & Communities

### [CDI](https://github.com/kubevirt/containerized-data-importer)

* Getting started
  * [Developer Guide](https://github.com/kubevirt/containerized-data-importer/blob/main/hack/README.md)
  * [Other Documentation](https://github.com/kubevirt/containerized-data-importer/tree/main/doc)

### [KubeVirt](https://github.com/kubevirt/kubevirt)

* Getting started
  * [Developer Guide](hthttps://github.com/kubevirt/kubevirt/blob/main/docs/getting-started.md)
  * [Documentation](https://github.com/kubevirt/user-guide)

### [Kubernetes](http://kubernetes.io/)

* Getting started
  * [http://kubernetesbyexample.com](http://kubernetesbyexample.com)
  * [Hello Minikube - Kubernetes](https://kubernetes.io/docs/tutorials/stateless-application/hello-minikube/)
  * [User Guide - Kubernetes](https://kubernetes.io/docs/user-guide/)
* Details
  * [Declarative Management of Kubernetes Objects Using Configuration Files - Kubernetes](https://kubernetes.io/docs/concepts/tools/kubectl/object-management-using-declarative-config/)
  * [Kubernetes Architecture](https://github.com/kubernetes/community/blob/master/contributors/design-proposals/architecture/architecture.md)

## Additional Topics

* Golang
  * [Documentation - The Go Programming Language](https://golang.org/doc/)
  * [Getting Started - The Go Programming Language](https://golang.org/doc/install)
* Patterns
  * [Introducing Operators: Putting Operational Knowledge into Software](https://coreos.com/blog/introducing-operators.html)
  * [Microservices](https://martinfowler.com/articles/microservices.html) nice
    content by Martin Fowler
* Testing
  * [Ginkgo - A Golang BDD Testing Framework](https://onsi.github.io/ginkgo/)
