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

func NewPrometheusScraper(ch chan<- prometheus.Metric) *prometheusScraper {
	return &prometheusScraper{ch: ch}
}

type prometheusScraper struct {
	ch chan<- prometheus.Metric
}

func (ps *prometheusScraper) Report(socketFile string) {
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

func (ps *prometheusScraper) describeVec(descCh chan *prometheus.Desc, vec prometheus.Collector) {
	go vec.Describe(descCh)
	desc := <-descCh
	ps.newMetric(desc)
}

func (ps *prometheusScraper) newMetric(desc *prometheus.Desc) {
	mv, err := prometheus.NewConstMetric(desc, prometheus.UntypedValue, 1024, "")
	if err != nil {
		panic(err)
	}
	ps.ch <- mv
}
