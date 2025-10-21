package metrics

import (
	"github.com/rhobs/operator-observability-toolkit/pkg/operatormetrics"
	ioprometheusclient "github.com/prometheus/client_model/go"
)

var (
	operatorMetrics = []operatormetrics.Metric{
		poolPathSharedWithOs,
	}

	poolPathSharedWithOs = operatormetrics.NewGauge(
		operatormetrics.MetricOpts{
			Name: "kubevirt_hpp_pool_path_shared_with_os",
			Help: "HPP pool path sharing a filesystem with OS, fix to prevent HPP PVs from causing disk pressure and affecting node operation",
		},
	)
)

// SetPoolPathSharedWithOs sets the poolPathSharedWithOs metric to a desired value
func SetPoolPathSharedWithOs(value int) {
	poolPathSharedWithOs.Set(float64(value))
}

func GetPoolPathSharedWithOs() float64 {
	dto := &ioprometheusclient.Metric{}
	poolPathSharedWithOs.Write(dto)
	return dto.GetGauge().GetValue()
}
