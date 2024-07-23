package collector

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var (
	emptyLineReg = regexp.MustCompile(`[\t\r\n]+`)
	hashLineReg  = regexp.MustCompile(`#.*`)
)

func mustParseJson(data string) any {
	var payload any
	if err := json.Unmarshal([]byte(data), &payload); err != nil {
		panic(err)
	}
	return payload
}

type testMetricsData struct {
	labelValue []string
	value      float64
}

func testMetricFamily(name string, mt dto.MetricType, metricsData ...testMetricsData) *dto.MetricFamily {
	mf := &dto.MetricFamily{
		Name: &name,
		Type: &mt,
	}
	for _, md := range metricsData {
		mf.Metric = append(mf.Metric, testMetric(mt, md.labelValue, md.value))
	}
	return mf
}

func testMetric(mt dto.MetricType, labelValue []string, value float64) *dto.Metric {
	m := &dto.Metric{}
	for _, lv := range labelValue {
		var lv = strings.Split(lv, "=")
		m.Label = append(m.Label, &dto.LabelPair{
			Name:  &lv[0],
			Value: &lv[1],
		})
	}

	switch mt {
	case dto.MetricType_COUNTER:
		m.Counter = &dto.Counter{Value: &value}
	case dto.MetricType_GAUGE:
		m.Gauge = &dto.Gauge{Value: &value}
	default:
		m.Untyped = &dto.Untyped{Value: &value}
	}

	return m
}

func metricFamiliesDiff(got, want []*dto.MetricFamily) string {
	return cmp.Diff(got, want,
		cmpopts.IgnoreUnexported(
			dto.MetricFamily{},
			dto.Metric{},
			dto.LabelPair{},
			dto.Counter{},
			dto.Gauge{},
			timestamppb.Timestamp{},
		),
		cmpopts.IgnoreFields(dto.MetricFamily{}, "Help"),
		cmpopts.IgnoreFields(dto.Metric{}, "Counter.CreatedTimestamp"),
	)
}

func metricsToText(gathering []*dto.MetricFamily, filterComment bool) string {
	out := &bytes.Buffer{}
	for _, mf := range gathering {
		if _, err := expfmt.MetricFamilyToText(out, mf); err != nil {
			panic(err)
		}
	}
	text := out.String()
	if !filterComment {
		return text
	}
	text = hashLineReg.ReplaceAllString(text, "")
	text = emptyLineReg.ReplaceAllString(strings.TrimSpace(text), "\n")
	return text
}

func TestJSONCollector_process(t *testing.T) {
	log := slog.Default()

	type args struct {
		c     *Collector
		input any
	}
	tests := []struct {
		name     string
		args     args
		expected []*dto.MetricFamily
		want     bool
	}{
		{
			name: "empty-data",
			args: args{
				&Collector{
					Namespace: "test",
					Metrics:   []*Metric{{Name: "value_count", Path: ".values[]"}},
				},
				mustParseJson(`{}`),
			},
			expected: []*dto.MetricFamily{},
			want:     true,
		},
		{
			name: "data-not-matching-path",
			args: args{
				&Collector{
					Namespace: "test",
					Metrics:   []*Metric{{Name: "value_count", Path: ".values[]"}},
				},
				mustParseJson(`{"some":"random"}`),
			},
			expected: []*dto.MetricFamily{},
			want:     true,
		},
		{
			name: "data-trigger-error-path",
			args: args{
				&Collector{
					Namespace: "test",
					Metrics:   []*Metric{{Name: "value_count", Path: ".values[]"}},
				},
				mustParseJson(`
				[{"noun": "lion","population": 123,"predator": true}]`),
			},
			expected: []*dto.MetricFamily{},
			want:     false,
		},
		{
			name: "filter-not-in-data",
			args: args{
				&Collector{
					Namespace: "test",
					Metrics: []*Metric{
						{
							Name: "value_count",
							Path: ".values[]", Filter: `.notInData == "ACTIVE"`,
							Labels: []Label{{"id", ".id"}},
						},
					},
				},
				mustParseJson(`
				{
					"counter": 1234,
					"timestamp": 1657568506,
					"values": [{"id": "id-A","count": 2,"some_boolean": true,"state": "ACTIVE"}],
					"location": "mars"
				}`),
			},
			expected: []*dto.MetricFamily{},
			want:     true,
		},
		{
			name: "repeated-data",
			args: args{
				&Collector{
					Namespace: "test",
					Metrics: []*Metric{
						{ // counter will add all values
							Name: "value_count", Type: "counter",
							Path: ".values[]", Filter: `.state == "ACTIVE"`,
							Value:  ".count",
							Labels: []Label{{"id", ".id"}},
						},
						{ // gauge will set last values
							Name: "value_gauge", Type: "gauge",
							Path: ".values[]", Filter: `.state == "ACTIVE"`,
							Value:  ".count",
							Labels: []Label{{"id", ".id"}},
						},
						{ // gauge with add operations will add all values
							Name: "value_gauge_with_add", Type: "gauge",
							Path: ".values[]", Filter: `.state == "ACTIVE"`,
							Value: ".count", Operation: OperationAdd,
							Labels: []Label{{"id", ".id"}},
						},
					},
				},
				mustParseJson(`
				{
					"counter": 1234,
					"timestamp": 1657568506,
					"values": [
						{"id": "id-A","count": 2,"some_boolean": true,"state": "ACTIVE"},
						{"id": "id-B","count": 5,"some_boolean": true,"state": "INACTIVE"},
						{"id": "id-C","count": 3,"some_boolean": false,"state": "ACTIVE"},
						{"id": "id-C","count": 4,"some_boolean": false,"state": "ACTIVE"}
					],
					"location": "mars"
				}`),
			},
			expected: []*dto.MetricFamily{
				testMetricFamily(
					"test_value_count", dto.MetricType_COUNTER,
					testMetricsData{[]string{"id=id-A"}, 2},
					testMetricsData{[]string{"id=id-C"}, 7},
				),
				testMetricFamily(
					"test_value_gauge", dto.MetricType_GAUGE,
					testMetricsData{[]string{"id=id-A"}, 2},
					testMetricsData{[]string{"id=id-C"}, 4},
				),
				testMetricFamily(
					"test_value_gauge_with_add", dto.MetricType_GAUGE,
					testMetricsData{[]string{"id=id-A"}, 2},
					testMetricsData{[]string{"id=id-C"}, 7},
				),
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := prometheus.NewPedanticRegistry()
			collector, err := jsonCollector(tt.args.c, reg, log)
			if err != nil {
				t.Fatalf("JSONCollector.process() error = %v", err)
			}

			if got := collector.process(context.Background(), tt.args.input); got != tt.want {
				t.Errorf("JSONCollector.process() = %v, want %v", got, tt.want)
			}

			gathering, err := reg.Gather()
			if err != nil {
				t.Errorf("JSONCollector.process() error = %v", err)
			}

			// fmt.Printf(metricsToText(gathering, true))

			if diff := metricFamiliesDiff(gathering, tt.expected); diff != "" {
				t.Errorf("ExecuteTemplate mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestJSONCollector_process_labelValues(t *testing.T) {
	log := slog.Default()

	type args struct {
		c     *Collector
		input any
	}
	tests := []struct {
		name     string
		args     args
		expected []*dto.MetricFamily
		want     bool
	}{
		{
			name: "",
			args: args{
				&Collector{
					Namespace: "test",
					Metrics: []*Metric{
						{
							Name: "global_counter", Type: "gauge", Value: ".counter",
							Labels: []Label{{"location", `"planet-"+ .location`}},
						},
						{
							Name: "global_values", Type: "gauge", Value: ".values | length",
							Labels: []Label{{"location", `"planet-"+ .location`}},
						},
						{
							Name: "global_published", Type: "gauge", Value: `.published | .[0:19] +"Z"  | fromdateiso8601`,
							Labels: []Label{{"location", `"planet-"+ .location`}},
						},
						{
							Name: "value_active",
							Path: ".values_text[]", Filter: `.state == "ACTIVE"`,
							Labels: []Label{{"id", ".id"}},
						}, {
							Name: "value_count",
							Path: ".values_text[]", Filter: `.state == "ACTIVE"`, Value: ".count",
							Labels: []Label{{"id", ".id"}},
						}, {
							Name: "value_boolean",
							Path: ".values_text[]", Filter: `.state == "ACTIVE"`, Value: ".some_boolean",
							Labels: []Label{{"id", ".id"}},
						}, {
							Name: "value_boolean_with_count_label",
							Path: ".values_text[]", Filter: `.state == "ACTIVE"`, Value: ".some_boolean",
							Labels: []Label{{"id", ".id"}, {"count", ".count"}},
						},
					},
				},
				mustParseJson(`
				{
					"counter": 1234,
					"timestamp": 1657568506,
					"published": "2023-12-19T10:02:17.972Z",
					"values": [
						{"id": "id-A","count": 2,"some_boolean": true,"state": "ACTIVE"},
						{"id": "id-B","count": 5,"some_boolean": true,"state": "INACTIVE"},
						{"id": "id-C","count": 3,"some_boolean": false,"state": "ACTIVE"}
					],
					"values_text": [
						{"id": "id-A","count": "2","some_boolean": "true","state": "ACTIVE"},
						{"id": "id-B","count": "5","some_boolean": "true","state": "INACTIVE"},
						{"id": "id-C","count": "3.4","some_boolean": "false","state": "ACTIVE"}
					],
					"location": "mars"
				}`),
			},
			expected: []*dto.MetricFamily{
				testMetricFamily(
					"test_global_counter", dto.MetricType_GAUGE,
					testMetricsData{[]string{"location=planet-mars"}, 1234},
				),
				testMetricFamily(
					"test_global_published", dto.MetricType_GAUGE,
					testMetricsData{[]string{"location=planet-mars"}, 1702980137},
				),
				testMetricFamily(
					"test_global_values", dto.MetricType_GAUGE,
					testMetricsData{[]string{"location=planet-mars"}, 3},
				),
				testMetricFamily(
					"test_value_active", dto.MetricType_COUNTER,
					testMetricsData{[]string{"id=id-A"}, 1},
					testMetricsData{[]string{"id=id-C"}, 1},
				),
				testMetricFamily(
					"test_value_boolean", dto.MetricType_COUNTER,
					testMetricsData{[]string{"id=id-A"}, 1},
					testMetricsData{[]string{"id=id-C"}, 0},
				),
				testMetricFamily(
					"test_value_boolean_with_count_label", dto.MetricType_COUNTER,
					testMetricsData{[]string{"count=2", "id=id-A"}, 1},
					testMetricsData{[]string{"count=3.4", "id=id-C"}, 0},
				),
				testMetricFamily(
					"test_value_count", dto.MetricType_COUNTER,
					testMetricsData{[]string{"id=id-A"}, 2},
					testMetricsData{[]string{"id=id-C"}, 3.4},
				),
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := prometheus.NewPedanticRegistry()
			collector, err := jsonCollector(tt.args.c, reg, log)
			if err != nil {
				t.Fatalf("JSONCollector.process() error = %v", err)
			}

			if got := collector.process(context.Background(), tt.args.input); got != tt.want {
				t.Errorf("JSONCollector.process() = %v, want %v", got, tt.want)
			}

			gathering, err := reg.Gather()
			if err != nil {
				t.Errorf("JSONCollector.process() error = %v", err)
			}

			fmt.Println(metricsToText(gathering, true))

			if diff := metricFamiliesDiff(gathering, tt.expected); diff != "" {
				t.Errorf("ExecuteTemplate mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestJSONCollector_process_multiplePayload(t *testing.T) {
	reg := prometheus.NewPedanticRegistry()
	log := slog.Default()

	collectors, err := New("../test/config.yaml", reg, log, "test_exporter")
	if err != nil {
		t.Fatalf("JSONCollector.process() error = %v", err)
	}

	type args struct {
		collector *JSONCollector
		input     any
	}
	tests := []struct {
		name     string
		args     args
		expected []*dto.MetricFamily
		want     bool
	}{
		{
			name: "example-should-pass",
			args: args{
				collectors["example"],
				mustParseJson(`
				{
					"counter": 1234,
					"timestamp": 1657568506,
					"values": [
						{"id": "id-A","count": 1,"some_boolean": true,"state": "ACTIVE"},
						{"id": "id-B","count": 2,"some_boolean": true,"state": "INACTIVE"},
						{"id": "id-C","count": 3,"some_boolean": false,"state": "ACTIVE"}
					],
					"location": "mars"
				}
				`),
			},
			expected: []*dto.MetricFamily{
				testMetricFamily(
					"example_active_count", dto.MetricType_GAUGE,
					testMetricsData{[]string{"environment=beta"}, 2},
				),
				testMetricFamily(
					"example_global_value", dto.MetricType_GAUGE,
					testMetricsData{[]string{"environment=beta", "location=planet-mars"}, 1234},
				),
				testMetricFamily(
					"example_inactive_value_count", dto.MetricType_COUNTER,
					testMetricsData{[]string{"environment=beta"}, 2},
				),
				testMetricFamily(
					"example_value_active", dto.MetricType_COUNTER,
					testMetricsData{[]string{"environment=beta", "id=id-A"}, 1},
					testMetricsData{[]string{"environment=beta", "id=id-C"}, 1},
				),
				testMetricFamily(
					"example_value_boolean", dto.MetricType_COUNTER,
					testMetricsData{[]string{"environment=beta", "id=id-A"}, 1},
					testMetricsData{[]string{"environment=beta", "id=id-C"}, 0},
				),
				testMetricFamily(
					"example_value_count", dto.MetricType_COUNTER,
					testMetricsData{[]string{"environment=beta", "id=id-A"}, 1},
					testMetricsData{[]string{"environment=beta", "id=id-C"}, 3},
				),
			},
			want: true,
		},
		{
			name: "example-should-pass-2nd-payload",
			args: args{
				collectors["example"],
				mustParseJson(`
				{
					"counter": 1234,
					"timestamp": 1657568506,
					"values": [
						{"id": "id-A","count": 1,"some_boolean": true,"state": "ACTIVE"},
						{"id": "id-B","count": 2,"some_boolean": true,"state": "INACTIVE"},
						{"id": "id-C","count": 3,"some_boolean": false,"state": "ACTIVE"}
					],
					"location": "mars"
				}
				`),
			},
			expected: []*dto.MetricFamily{
				testMetricFamily(
					"example_active_count", dto.MetricType_GAUGE,
					testMetricsData{[]string{"environment=beta"}, 4},
				),
				testMetricFamily(
					"example_global_value", dto.MetricType_GAUGE,
					testMetricsData{[]string{"environment=beta", "location=planet-mars"}, 1234},
				),
				testMetricFamily(
					"example_inactive_value_count", dto.MetricType_COUNTER,
					testMetricsData{[]string{"environment=beta"}, 4},
				),
				testMetricFamily(
					"example_value_active", dto.MetricType_COUNTER,
					testMetricsData{[]string{"environment=beta", "id=id-A"}, 2},
					testMetricsData{[]string{"environment=beta", "id=id-C"}, 2},
				),
				testMetricFamily(
					"example_value_boolean", dto.MetricType_COUNTER,
					testMetricsData{[]string{"environment=beta", "id=id-A"}, 2},
					testMetricsData{[]string{"environment=beta", "id=id-C"}, 0},
				),
				testMetricFamily(
					"example_value_count", dto.MetricType_COUNTER,
					testMetricsData{[]string{"environment=beta", "id=id-A"}, 2},
					testMetricsData{[]string{"environment=beta", "id=id-C"}, 6},
				),
			},
			want: true,
		},
		{
			name: "diff-collector",
			args: args{
				collectors["animals"],
				mustParseJson(`
				[
					{"noun":"lion","population":123,"predator":true},
					{"noun":"deer","population":456,"predator":false},
					{"noun":"pigeon","population":789,"predator":false}
				]
				`),
			},
			expected: []*dto.MetricFamily{
				testMetricFamily(
					"animal_population", dto.MetricType_GAUGE,
					testMetricsData{[]string{"name=deer", "predator=false"}, 456},
					testMetricsData{[]string{"name=lion", "predator=true"}, 123},
					testMetricsData{[]string{"name=pigeon", "predator=false"}, 789},
				),
				testMetricFamily(
					"example_active_count", dto.MetricType_GAUGE,
					testMetricsData{[]string{"environment=beta"}, 4},
				),
				testMetricFamily(
					"example_global_value", dto.MetricType_GAUGE,
					testMetricsData{[]string{"environment=beta", "location=planet-mars"}, 1234},
				),
				testMetricFamily(
					"example_inactive_value_count", dto.MetricType_COUNTER,
					testMetricsData{[]string{"environment=beta"}, 4},
				),
				testMetricFamily(
					"example_value_active", dto.MetricType_COUNTER,
					testMetricsData{[]string{"environment=beta", "id=id-A"}, 2},
					testMetricsData{[]string{"environment=beta", "id=id-C"}, 2},
				),
				testMetricFamily(
					"example_value_boolean", dto.MetricType_COUNTER,
					testMetricsData{[]string{"environment=beta", "id=id-A"}, 2},
					testMetricsData{[]string{"environment=beta", "id=id-C"}, 0},
				),
				testMetricFamily(
					"example_value_count", dto.MetricType_COUNTER,
					testMetricsData{[]string{"environment=beta", "id=id-A"}, 2},
					testMetricsData{[]string{"environment=beta", "id=id-C"}, 6},
				),
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.args.collector.process(context.Background(), tt.args.input); got != tt.want {
				t.Errorf("JSONCollector.process() = %v, want %v", got, tt.want)
			}

			gathering, err := reg.Gather()
			if err != nil {
				t.Errorf("JSONCollector.process() error = %v", err)
			}

			// fmt.Println(metricsToText(gathering, true))

			if diff := metricFamiliesDiff(gathering, tt.expected); diff != "" {
				t.Errorf("ExecuteTemplate mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
