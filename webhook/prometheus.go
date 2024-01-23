package webhook

import "github.com/prometheus/client_golang/prometheus"

var (
	pcRequests *prometheus.CounterVec
)

func initMetrics(reg *prometheus.Registry, exporterNamespace string) {

	pcRequests = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: exporterNamespace,
			Name:      "webhook_requests_total",
			Help:      "The total number of requests received",
		},
		[]string{"webhook", "status"},
	)

	reg.MustRegister(pcRequests)
}
