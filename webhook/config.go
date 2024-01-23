package webhook

import (
	"fmt"
	"os"
	"strings"

	"github.com/itchyny/gojq"
	"gopkg.in/yaml.v2"
)

type Config struct {
	WebHooks map[string]*WebHook `yaml:"webhooks"`
}

type WebHook struct {
	id     string
	Method string `yaml:"method"`
	Path   string `yaml:"path"`
	Auth   struct {
		Headers []Header `yaml:"headers"`
	} `yaml:"auth"`
	Response struct {
		Headers []Header `yaml:"headers"`
		Message string   `yaml:"message"`
		Code    int      `yaml:"code"`
	} `yaml:"response"`
	Collectors []Collector `yaml:"collectors"`
}

type Collector struct {
	ID            string `yaml:"id"`
	Transform     string `yaml:"transform"`
	transformCode *gojq.Code
}

type Header struct {
	Name         string `yaml:"name"`
	Value        string `yaml:"value"`
	ValueFromEnv string `yaml:"valueFromEnv"`
}

func (h Header) GetValue() string {
	if h.Value != "" {
		return h.Value
	}
	return os.Getenv(h.ValueFromEnv)
}

func loadConfig(configPath string) (map[string]*WebHook, error) {
	var config Config

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	for id, webhook := range config.WebHooks {
		webhook.id = id
	}

	return config.WebHooks, validateConfig(config)
}

func validateConfig(config Config) error {
	// webhook path must be unique
	paths := make(map[string]bool)
	for id, wh := range config.WebHooks {
		if wh.Path == "" {
			return fmt.Errorf("empty path not allowed webhook:%s", id)
		}
		if !strings.HasPrefix(wh.Path, "/") {
			return fmt.Errorf("path should have '/' prefix webhook:%s", id)
		}
		if _, ok := paths[wh.Path]; ok {
			return fmt.Errorf("webhooks path must be unique duplicate found webhook:%s path:%s", id, wh.Path)
		}
		paths[wh.Path] = true
	}
	return nil
}

func parseAndCompileJQExp(exp string) (*gojq.Code, error) {
	query, err := gojq.Parse(exp)
	if err != nil {
		return nil, fmt.Errorf("jq query parse error %w", err)
	}

	code, err := gojq.Compile(query)
	if err != nil {
		return nil, fmt.Errorf("jq query compile error %w", err)
	}

	return code, nil
}
