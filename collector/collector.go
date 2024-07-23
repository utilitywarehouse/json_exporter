package collector

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/itchyny/gojq"
	"github.com/prometheus/client_golang/prometheus"
)

type JSONCollector struct {
	id      string
	Input   chan any
	log     *slog.Logger
	metrics []*jsonMetric
}

type jsonMetric struct {
	name   string
	path   *gojq.Code
	filter *gojq.Code
	value  *gojq.Code
	labels []jsonLabel

	metricType MetricType
	operation  MetricOperation

	pCounterVec *prometheus.CounterVec
	pGaugeVec   *prometheus.GaugeVec
}

type jsonLabel struct {
	name  string
	value *gojq.Code
}

func New(configPath string, reg *prometheus.Registry, log *slog.Logger, exporterNamespace string) (map[string]*JSONCollector, error) {
	jsonCollectors := make(map[string]*JSONCollector)

	collectors, err := loadCollectors(configPath)
	if err != nil {
		return nil, fmt.Errorf("unable to load collectors err:%w", err)
	}

	for id, collector := range collectors {
		js, err := jsonCollector(collector, reg, log.With("collector", id))
		if err != nil {
			return nil, fmt.Errorf("unable to create collector err:%w", err)
		}
		jsonCollectors[id] = js
	}

	initMetrics(reg, exporterNamespace)

	return jsonCollectors, nil
}

func jsonCollector(collector *Collector, reg *prometheus.Registry, log *slog.Logger) (*JSONCollector, error) {
	jsonCollector := JSONCollector{
		id:    collector.id,
		log:   log,
		Input: make(chan any),
	}

	var defaultLabels []jsonLabel

	// parse default labels
	for _, lc := range collector.DefaultLabels {
		l := jsonLabel{name: lc.Name}
		code, err := parseAndCompileJQExp(lc.Value)
		if err != nil {
			return nil, fmt.Errorf("unable to parse default label expression name:%s err:%w", lc.Name, err)
		}
		l.value = code

		defaultLabels = append(defaultLabels, l)
	}

	for _, m := range collector.Metrics {
		jm, err := newJsonMetric(m, reg, collector.Namespace, defaultLabels)
		if err != nil {
			return nil, fmt.Errorf("unable to create json metrics err:%w", err)
		}
		jsonCollector.metrics = append(jsonCollector.metrics, jm)
	}
	return &jsonCollector, nil
}

func newJsonMetric(metric *Metric, reg *prometheus.Registry, ns string, defaultLabels []jsonLabel) (*jsonMetric, error) {
	var err error

	if metric == nil {
		return nil, fmt.Errorf("metric is required")
	}

	*metric = setDefaults(*metric)

	jm := &jsonMetric{name: metric.Name, metricType: metric.Type, operation: metric.Operation}

	jm.path, err = parseAndCompileJQExp(metric.Path)
	if err != nil {
		return nil, fmt.Errorf("unable to parse path expression metric:%s err:%w", metric.Name, err)
	}

	jm.filter, err = parseAndCompileJQExp(metric.Filter)
	if err != nil {
		return nil, fmt.Errorf("unable to parse filter expression metric:%s err:%w", metric.Name, err)
	}

	jm.value, err = parseAndCompileJQExp(metric.Value)
	if err != nil {
		return nil, fmt.Errorf("unable to parse value expression metric:%s err:%w", metric.Name, err)
	}

	jm.labels = append(jm.labels, defaultLabels...)

	// parse metric labels
	for _, mcl := range metric.Labels {
		l := jsonLabel{name: mcl.Name}
		code, err := parseAndCompileJQExp(mcl.Value)
		if err != nil {
			return nil, fmt.Errorf("unable to parse label expression metric:%s name:%s err:%w",
				metric.Name, mcl.Name, err)
		}
		l.value = code

		jm.labels = append(jm.labels, l)
	}

	switch metric.Type {

	case CounterMetric:
		jm.pCounterVec = prometheus.NewCounterVec(
			prometheus.CounterOpts{Namespace: ns, Name: metric.Name, Help: metric.Help},
			getLabelNames(jm.labels),
		)
		reg.MustRegister(jm.pCounterVec)

	case GaugeMetric:
		jm.pGaugeVec = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{Namespace: ns, Name: metric.Name, Help: metric.Help},
			getLabelNames(jm.labels),
		)
		reg.MustRegister(jm.pGaugeVec)

	default:
		return nil, fmt.Errorf("unknown metric type")
	}

	return jm, nil
}

// Start runs a continuous loop that starts a new collection when a input payload comes into the queue channel.
func (jc *JSONCollector) Start(ctx context.Context) {
	wg := &sync.WaitGroup{}

	for {
		select {
		case <-ctx.Done():
			// wait for all run to finish
			wg.Wait()
			return

		case input := <-jc.Input:
			wg.Add(1)
			go func(input any) {
				defer wg.Done()
				// create new context for worker
				wCtx, wCancel := context.WithTimeout(ctx, time.Minute)
				defer wCancel()

				start := time.Now()

				success := jc.process(wCtx, input)

				cmu.updateCollectorSuccess(jc.id, success)
				cmu.updateCollectorDuration(jc.id, time.Since(start).Seconds(), success)

				if success {
					jc.log.Debug("metrics collection completed successfully")
				} else {
					jc.log.Error("metrics collection completed with error")
				}
			}(input)
		}
	}
}

func (jc *JSONCollector) process(ctx context.Context, input any) bool {
	success := true
	for _, metric := range jc.metrics {
		iter := metric.path.RunWithContext(ctx, input)
		for {
			object, ok := iter.Next()
			if !ok {
				break
			}

			if err, ok := object.(error); ok {
				if err.Error() == "cannot iterate over: null" {
					break
				}
				// todo: should we filter this out?
				// if strings.Contains(err.Error(), "expected an object but got: array") {
				// 	break
				// }

				jc.log.Error("unable to get json objects", "metric", metric.name, "err", err)
				success = false
				break
			}

			if err := metric.collect(ctx, object); err != nil {
				jc.log.Error("unable to collect", "metric", metric.name, "err", err)
				success = false
				break
			}
		}
	}
	return success
}

func (jm *jsonMetric) collect(ctx context.Context, input any) error {
	filter, err := extractFirstValue(ctx, jm.filter, input)
	if err != nil {
		return fmt.Errorf("unable to get filter value err:%w", err)
	}

	if filter == false {
		return nil
	}

	labels, err := jm.extractLabels(ctx, input)
	if err != nil {
		return err
	}

	value, err := extractFirstValue(ctx, jm.value, input)
	if err != nil {
		return fmt.Errorf("unable to get value err:%w", err)
	}

	v, err := sanitizeValue(value)
	if err != nil {
		return fmt.Errorf("unable to sanitize value err:%w", err)
	}

	jm.updateValue(labels, v)

	return nil
}

func (jm *jsonMetric) extractLabels(ctx context.Context, input any) (prometheus.Labels, error) {
	pLabels := prometheus.Labels{}

	for _, label := range jm.labels {
		v, err := extractFirstValue(ctx, label.value, input)
		if err != nil {
			return nil, fmt.Errorf("unable to get label value label:%s err:%w", label.name, err)
		}
		pLabels[label.name] = fmt.Sprint(v)
	}

	return pLabels, nil
}

func (jm *jsonMetric) updateValue(labels prometheus.Labels, v float64) {
	switch jm.metricType {

	case CounterMetric:
		jm.pCounterVec.With(labels).Add(v)

	case GaugeMetric:
		switch jm.operation {
		case OperationAdd:
			jm.pGaugeVec.With(labels).Add(v)
		default:
			jm.pGaugeVec.With(labels).Set(v)
		}
	}
}

func extractFirstValue(ctx context.Context, code *gojq.Code, input any) (any, error) {
	iter := code.RunWithContext(ctx, input)
	v, ok := iter.Next()
	if !ok {
		return nil, nil
	}
	if err, ok := v.(error); ok {
		return nil, fmt.Errorf("unable to get value err:%w", err)
	}
	return v, nil
}
