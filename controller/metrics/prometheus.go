/*
Copyright 2021 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"log"
	"net/http"
)

// Handler creates a new prometheus handler to receive scrap requests
func Handler(MaxRequestsInFlight int) http.Handler {
	return promhttp.InstrumentMetricHandler(
		prometheus.DefaultRegisterer,
		promhttp.HandlerFor(
			prometheus.DefaultGatherer,
			promhttp.HandlerOpts{
				MaxRequestsInFlight: MaxRequestsInFlight,
			}),
	)
}

// NewPrometheusScraper returns a new struct of the prometheus scrapper
func NewPrometheusScraper(ch chan<- prometheus.Metric) *PrometheusScraper {
	return &PrometheusScraper{ch: ch}
}

// PrometheusScraper struct containing the resources to scrap prometheus metrics
type PrometheusScraper struct {
	ch chan<- prometheus.Metric
}

// Report adds CDI metrics to PrometheusScraper
func (ps *PrometheusScraper) Report(socketFile string) {
	defer func() {
		if err := recover(); err != nil {
			log.Panicf("collector goroutine panicked for VM %s: %s", socketFile, err)
		}
	}()

	descCh := make(chan *prometheus.Desc)

	ps.describeVec(descCh, PersistentVolumeClaimProvisionTotal)
	ps.describeVec(descCh, PersistentVolumeClaimProvisionFailedTotal)
	ps.describeVec(descCh, PersistentVolumeClaimProvisionDurationSeconds)
	ps.describeVec(descCh, PersistentVolumeDeleteTotal)
	ps.describeVec(descCh, PersistentVolumeDeleteFailedTotal)
	ps.describeVec(descCh, PersistentVolumeDeleteDurationSeconds)
}

func (ps *PrometheusScraper) describeVec(descCh chan *prometheus.Desc, vec prometheus.Collector) {
	go vec.Describe(descCh)
	desc := <-descCh
	ps.newMetric(desc)
}

func (ps *PrometheusScraper) newMetric(desc *prometheus.Desc) {
	mv, err := prometheus.NewConstMetric(desc, prometheus.UntypedValue, 1024, "")
	if err != nil {
		panic(err)
	}
	ps.ch <- mv
}
