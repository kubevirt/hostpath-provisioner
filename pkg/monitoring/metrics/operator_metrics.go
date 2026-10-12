package metrics

import (
	ioprometheusclient "github.com/prometheus/client_model/go"
	"github.com/rhobs/operator-observability-toolkit/pkg/operatormetrics"
)

var (
	operatorMetrics = []operatormetrics.Metric{
		poolPathSharedWithOs,
		hppVolQuotaBytes,
		hppVolUsedBytes,
	}

	poolPathSharedWithOs = operatormetrics.NewGauge(
		operatormetrics.MetricOpts{
			Name: "kubevirt_hpp_pool_path_shared_with_os",
			Help: "HPP pool path sharing a filesystem with OS, fix to prevent HPP PVs from causing disk pressure and affecting node operation",
		},
	)

	hppVolQuotaBytes = operatormetrics.NewGaugeVec(
		operatormetrics.MetricOpts{
			Name: "kubevirt_hpp_volume_quota_bytes",
			Help: "HPP volume quota bytes set by the PVC request limit field",
		},
		[]string{"volume"},
	)

	hppVolUsedBytes = operatormetrics.NewGaugeVec(
		operatormetrics.MetricOpts{
			Name: "kubevirt_hpp_volume_used_bytes",
			Help: "HPP per volume usage bytes",
		},
		[]string{"volume"},
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

func SetHPPVolQuotaBytes(volumeID string, value int64) {
	hppVolQuotaBytes.WithLabelValues(volumeID).Set(float64(value))
}

func GetHPPVolQuotaBytes(volumeID string) float64 {
	dto := &ioprometheusclient.Metric{}
	hppVolQuotaBytes.WithLabelValues(volumeID).Write(dto)
	return dto.GetGauge().GetValue()
}

// func SetHPPVolUsedBytes(volumeID string, value int64) {
// 	hppVolUsedBytes.WithLabelValues(volumeID).Set(float64(value))
// }

// func GetHPPVolUsedBytes(volumeID string) float64 {
// 	dto := &ioprometheusclient.Metric{}
// 	hppVolUsedBytes.WithLabelValues(volumeID).Write(dto)
// 	return dto.GetGauge().GetValue()
// }

// DeleteHPPVolumeMetrics removes all per-volume metric series for volumeID.
func DeleteHPPVolumeMetrics(volumeID string) {
	hppVolQuotaBytes.DeleteLabelValues(volumeID)
	hppVolUsedBytes.DeleteLabelValues(volumeID)
}
