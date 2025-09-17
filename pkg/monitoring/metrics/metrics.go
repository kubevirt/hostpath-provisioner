package metrics

import "github.com/rhobs/operator-observability-toolkit/pkg/operatormetrics"

func SetupMetrics() error {
	return operatormetrics.RegisterMetrics(
		operatorMetrics,
	)
}

func ListMetrics() []operatormetrics.Metric {
	return operatormetrics.ListMetrics()
}
