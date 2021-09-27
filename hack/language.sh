#!/bin/sh
#
# Check for less inclusive language usage.
# Allowed exceptions (hard to change) excluded with grep -v.
# Generated files excluded.
#

HPP_DIR="$(cd $(dirname $0)/../ && pwd -P)"

PHRASES='master|slave|whitelist|blacklist'

VIOLATIONS=$(git grep -iI -E $PHRASES -- \
	':!vendor' \
	':!cluster-up' \
	':!cluster-sync' \
	':!*generated*' \
	':!*swagger.json*' \
	':!hack/language.sh' \
		"${HPP_DIR}" \
		| grep -v \
			-e 'github.com/Masterminds' \
			-e 'github.com/kubernetes' \
			-e 'golang/dep' \
			-e 'kubernetes-sigs/sig-storage-lib-external-provisioner')
			# Allowed exceptions

if [ ! -z "${VIOLATIONS}" ]; then
	echo "ERROR: Found new additions of non-inclusive language ${PHRASES}"
	echo "${VIOLATIONS}"
	echo ""
	echo "Please consider different terminology if possible."
	echo "If necessary, an exception can be added to to the hack/language.sh script"
	exit 1
fi
