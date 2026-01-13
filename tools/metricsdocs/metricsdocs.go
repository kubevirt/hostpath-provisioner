package main

import (
	"fmt"

	"github.com/rhobs/operator-observability-toolkit/pkg/docs"

	"kubevirt.io/hostpath-provisioner/pkg/monitoring/metrics"
)

const title = `Hostpath Provisioner Metrics`

func main() {
	err := metrics.SetupMetrics()
	if err != nil {
		panic(err)
	}

	metricsList := metrics.ListMetrics()

	docsString := docs.BuildMetricsDocs(title, metricsList, nil)
	fmt.Print(docsString)
}
