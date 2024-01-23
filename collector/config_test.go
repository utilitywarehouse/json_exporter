package collector

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func Test_setDefaults(t *testing.T) {
	type args struct {
		m Metric
	}
	tests := []struct {
		name string
		args args
		want Metric
	}{
		{
			"",
			args{Metric{Name: "value_count"}},
			Metric{
				Name:   "value_count",
				Help:   "json_exporter metric:value_count",
				Path:   "",
				Filter: "",
				Value:  "1",
				Labels: nil,
				Type:   "counter",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := setDefaults(tt.args.m)

			if diff := cmp.Diff(got, tt.want); diff != "" {
				t.Errorf("setDefaults mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_validateConfig(t *testing.T) {
	type args struct {
		config Config
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			"valid",
			args{
				Config{
					Collectors: map[string]*Collector{
						"test1": {
							Namespace: "ns1",
							Metrics:   []*Metric{{Name: "metric_1"}, {Name: "metric_2"}},
						},
						"test2": {
							Namespace: "ns2",
							Metrics:   []*Metric{{Name: "metric_1"}, {Name: "metric_2"}},
						},
					},
				},
			},
			false,
		},
		{
			"same namespace between collector",
			args{
				Config{
					Collectors: map[string]*Collector{
						"test1": {
							Namespace: "ns",
							Metrics:   []*Metric{{Name: "metric_1"}, {Name: "metric_2"}},
						},
						"test2": {
							Namespace: "ns",
							Metrics:   []*Metric{{Name: "metric_1"}, {Name: "metric_2"}},
						},
					},
				},
			},
			true,
		},
		{
			"duplicate metrics name",
			args{
				Config{
					Collectors: map[string]*Collector{
						"test1": {
							Namespace: "ns1",
							Metrics:   []*Metric{{Name: "metric"}, {Name: "metric"}},
						},
						"test2": {
							Namespace: "ns2",
							Metrics:   []*Metric{{Name: "metric_1"}, {Name: "metric_2"}},
						},
					},
				},
			},
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateConfig(tt.args.config); (err != nil) != tt.wantErr {
				t.Errorf("validateConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
