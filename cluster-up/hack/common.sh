#!/usr/bin/env bash

if [ -z "$KUBEVIRTCI_PATH" ]; then
    KUBEVIRTCI_PATH="$(
        cd "$(dirname "$BASH_SOURCE[0]")/../"
        echo "$(pwd)/"
    )"
fi


if [ -z "$KUBEVIRTCI_CONFIG_PATH" ]; then
    KUBEVIRTCI_CONFIG_PATH="$(
        cd "$(dirname "$BASH_SOURCE[0]")/../../"
        echo "$(pwd)/_ci-configs"
    )"
fi

# operator image tags are only set in the release dowload manifest
OPERATOR_REPO="https://github.com/kubevirt/hostpath-provisioner-operator"

function resolve_operator_url() {
    if [ -n "${OPERATOR_VERSION}" ]; then
        echo "${OPERATOR_REPO}/releases/download/${OPERATOR_VERSION}"
        return
    fi
    # PULL_BASE_REF is set for prow jobs, get correct tag if targetting release branch
    if [ -n "${PULL_BASE_REF}" ] && [[ "${PULL_BASE_REF}" == release-* ]]; then
        local tag=$(git describe --tags --abbrev=0 2>/dev/null)
        if [ -z "${tag}" ]; then
        echo "ERROR: on release branch but no git tags found" >&2
        exit 1
        fi
        echo "${OPERATOR_REPO}/releases/download/${tag}"
        return
    fi
    # for local enviornments or when prow is targetting main, fall back to using main
    # NOTE: if you are working off a release branch, this could cause issues
    # instead you should explicitly set the OPERATOR_VERSION
    echo "https://raw.githubusercontent.com/kubevirt/hostpath-provisioner-operator/main/deploy"
}



KUBEVIRTCI_CLUSTER_PATH=${KUBEVIRTCI_CLUSTER_PATH:-${KUBEVIRTCI_PATH}/cluster}
KUBEVIRT_PROVIDER=${KUBEVIRT_PROVIDER:-k8s-1.32}
KUBEVIRT_NUM_NODES=${KUBEVIRT_NUM_NODES:-1}
KUBEVIRT_NUM_NUMA_NODES=${KUBEVIRT_NUM_NUMA_NODES:-1}
KUBEVIRT_NUM_VCPU=${KUBEVIRT_NUM_VCPU:-6}
KUBEVIRT_MEMORY_SIZE=${KUBEVIRT_MEMORY_SIZE:-5120M}
KUBEVIRT_NUM_SECONDARY_NICS=${KUBEVIRT_NUM_SECONDARY_NICS:-0}
KUBEVIRT_DEPLOY_ISTIO=${KUBEVIRT_DEPLOY_ISTIO:-false}
KUBEVIRT_PSA=${KUBEVIRT_PSA:-false}
KUBEVIRT_SINGLE_STACK=${KUBEVIRT_SINGLE_STACK:-false}
KUBEVIRT_FLANNEL=${KUBEVIRT_FLANNEL:-false}
KUBEVIRT_NO_ETCD_FSYNC=${KUBEVIRT_NO_ETCD_FSYNC:-false}
KUBEVIRT_ENABLE_AUDIT=${KUBEVIRT_ENABLE_AUDIT:-false}
KUBEVIRT_DEPLOY_NFS_CSI=${KUBEVIRT_DEPLOY_NFS_CSI:-false}
KUBEVIRT_DEPLOY_PROMETHEUS=${KUBEVIRT_DEPLOY_PROMETHEUS:-false}
KUBEVIRT_DEPLOY_PROMETHEUS_ALERTMANAGER=${KUBEVIRT_DEPLOY_PROMETHEUS_ALERTMANAGER-false}
KUBEVIRT_DEPLOY_GRAFANA=${KUBEVIRT_DEPLOY_GRAFANA:-false}
KUBEVIRT_CGROUPV2=${KUBEVIRT_CGROUPV2:-true}
KUBEVIRT_DEPLOY_CDI=${KUBEVIRT_DEPLOY_CDI:-false}
KUBEVIRT_DEPLOY_AAQ=${KUBEVIRT_DEPLOY_AAQ:-false}
KUBEVIRT_CUSTOM_AAQ_VERSION=${KUBEVIRT_CUSTOM_AAQ_VERSION}
KUBEVIRT_CUSTOM_CDI_VERSION=${KUBEVIRT_CUSTOM_CDI_VERSION}
KUBEVIRT_SWAP_ON=${KUBEVIRT_SWAP_ON:-false}
KUBEVIRT_KSM_ON=${KUBEVIRT_KSM_ON:-false}
KUBEVIRT_UNLIMITEDSWAP=${KUBEVIRT_UNLIMITEDSWAP:-false}
KUBVIRT_WITH_CNAO_SKIP_CONFIG=${KUBVIRT_WITH_CNAO_SKIP_CONFIG:-false}

# If on a developer setup, expose ocp on 8443, so that the openshift web console can be used (the port is important because of auth redirects)
# http and https ports are accessed by testing framework and should not be randomized
if [ -z "${JOB_NAME}" ]; then
    KUBEVIRT_PROVIDER_EXTRA_ARGS="${KUBEVIRT_PROVIDER_EXTRA_ARGS} --ocp-port 8443"
fi

provider_prefix=${KUBEVIRT_PROVIDER}
job_prefix=${JOB_NAME:-kubevirt}${EXECUTOR_NUMBER}

mkdir -p $KUBEVIRTCI_CONFIG_PATH/$KUBEVIRT_PROVIDER
KUBEVIRTCI_TAG=2509040743-fa04ec09
