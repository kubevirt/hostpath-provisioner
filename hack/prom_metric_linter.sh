set -e

linter_image_tag="v0.0.3"

PROJECT_ROOT="$(readlink -e "$(dirname "${BASH_SOURCE[0]}")"/../)"
export METRICS_COLLECTOR_PATH="${METRICS_COLLECTOR_PATH:-${PROJECT_ROOT}/tools/prom-metrics-collector}"

if [[ ! -d "$METRICS_COLLECTOR_PATH" ]]; then
    echo "Invalid METRICS_COLLECTOR_PATH: $METRICS_COLLECTOR_PATH is not a valid directory path"
    exit 1
fi

# Parse command-line arguments
while [[ $# -gt 0 ]]; do
    case "$1" in
    --operator-name=*)
        operator_name="${1#*=}"
        shift
        ;;
    --sub-operator-name=*)
        sub_operator_name="${1#*=}"
        shift
        ;;
    *)
        echo "Invalid argument: $1"
        exit 1
        ;;
    esac
done

# Get the metrics list
go build -o _out/prom-metrics-collector "$METRICS_COLLECTOR_PATH/..."
json_output=$(_out/prom-metrics-collector 2>/dev/null)

# Select container runtime
source hack/common.sh

# Run the linter by using the prom-metrics-linter Docker container
errors=$($OCI_BIN run --rm -i "quay.io/kubevirt/prom-metrics-linter:$linter_image_tag" \
    --metric-families="$json_output" \
    --operator-name="$operator_name" \
    --sub-operator-name="$sub_operator_name" 2>/dev/null)

# Check if there were any errors, if yes print and fail
if [[ $errors != "" ]]; then
  echo "$errors"
  exit 1
fi