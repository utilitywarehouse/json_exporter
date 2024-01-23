package collector

import (
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
)

var cmu collectorMetricsUpdater

// collectorMetricsUpdater allows for mocking out the functionality of collectorMetrics when testing.
type collectorMetricsUpdater interface {
	updateCollectorSuccess(string, bool)
	updateCollectorDuration(string, float64, bool)
}

// collectorMetrics implements instrumentation of metrics for collectors
// count is a Counter vector to increment the number of successful and failed collection attempts for each collector.
// duration is a Summary vector that keeps track of the duration for collections per payload.
type collectorMetrics struct {
	count    *prometheus.CounterVec
	duration *prometheus.HistogramVec
}

func initMetrics(reg *prometheus.Registry, exporterNamespace string) {
	sm := new(collectorMetrics)

	sm.count = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: exporterNamespace,
		Name:      "collection_count",
		Help:      "Success metric for every collection",
	},
		[]string{
			// Name of the collector
			"collector",
			// Result: true if the collector was successful, false otherwise
			"success",
		},
	)

	sm.duration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: exporterNamespace,
		Name:      "collection_duration_seconds",
		Help:      "Duration of a collection",
	},
		[]string{
			// Name of the collector
			"collector",
			// Result: true if the collector was successful, false otherwise
			"success",
		},
	)

	reg.MustRegister(sm.count, sm.duration)

	cmu = sm
}

func (sm *collectorMetrics) updateCollectorSuccess(collector string, success bool) {
	sm.count.With(prometheus.Labels{
		"collector": collector,
		"success":   strconv.FormatBool(success),
	}).Inc()
}

func (sm *collectorMetrics) updateCollectorDuration(collector string, duration float64, success bool) {
	sm.duration.With(prometheus.Labels{
		"collector": collector,
		"success":   strconv.FormatBool(success),
	}).Observe(duration)
}
