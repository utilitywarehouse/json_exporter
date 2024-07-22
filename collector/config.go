package collector

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v2"
)

type MetricType string

const (
	CounterMetric MetricType = "counter"
	GaugeMetric   MetricType = "gauge"
)

type MetricOperation string

const (
	OperationAdd MetricOperation = "add"
	OperationSet MetricOperation = "set"
)

type Config struct {
	Collectors map[string]*Collector `yaml:"collectors"`
}

type Collector struct {
	id            string
	Namespace     string    `yaml:"namespace"`
	DefaultLabels []Label   `yaml:"defaultLabels"`
	Metrics       []*Metric `yaml:"metrics"`
}

type Metric struct {
	Name      string          `yaml:"name"`
	Help      string          `yaml:"help"`
	Path      string          `yaml:"path"`
	Filter    string          `yaml:"filter"`
	Operation MetricOperation `yaml:"operation"`
	Value     string          `yaml:"value"`
	Labels    []Label         `yaml:"labels"`
	Type      MetricType      `yaml:"type"`
}

type Label struct {
	Name  string `yaml:"name"`
	Value string `yaml:"value"`
}

func loadCollectors(configPath string) (map[string]*Collector, error) {
	var config Config
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return config.Collectors, validateConfig(config)
}

func setDefaults(m Metric) Metric {
	if m.Help == "" {
		m.Help = fmt.Sprintf("json_exporter metric:%s", m.Name)
	}

	if m.Type == "" {
		m.Type = CounterMetric
	}

	if m.Value == "" {
		m.Value = "1"
	}
	return m
}

func validateConfig(config Config) error {
	// metrics name must be unique per collector
	names := make(map[string]bool)
	for name, c := range config.Collectors {
		for _, m := range c.Metrics {
			if _, ok := names[c.Namespace+"_"+m.Name]; ok {
				return fmt.Errorf("metrics name must be unique duplicate names found collector:%s namespace:%s metric:%s",
					name, c.Namespace, m.Name)
			}
			names[c.Namespace+"_"+m.Name] = true
		}
	}

	return nil
}
