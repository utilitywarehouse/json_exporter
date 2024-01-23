package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/utilitywarehouse/json_exporter/collector"
	"github.com/utilitywarehouse/json_exporter/webhook"
)

var (
	log               *slog.Logger
	logLevel          string
	address           string
	metricPath        string
	configPath        string
	exporterNamespace string
)

func usage() {
	fmt.Fprintf(os.Stderr, "NAME:\n")
	fmt.Fprintf(os.Stderr, "\tjson_exporter\n")

	fmt.Fprintf(os.Stderr, "DESCRIPTION:\n")
	fmt.Fprintf(os.Stderr, "\tA prometheus exporter which collects metrics from JSON using jq path expression\n")

	fmt.Fprintf(os.Stderr, "OPTIONS:\n")
	fmt.Fprintf(os.Stderr, "\t--log-level                  (default: info)               [$LOG_LEVEL]\n")
	fmt.Fprintf(os.Stderr, "\t--listen-address             (default: :9000)              [$LISTEN_ADDRESS]\n")
	fmt.Fprintf(os.Stderr, "\t--metrics-path               (default: /metrics)           [$METRICS_PATH]\n")
	fmt.Fprintf(os.Stderr, "\t--exporter-config            (default: json-exporter.yaml) [$EXPORTER_CONFIG]\n")
	fmt.Fprintf(os.Stderr, "\t--exporter-metrics-namespace (default: json_exporter)      [$EXPORTER_METRICS_NAMESPACE]\n")
	os.Exit(2)
}

func checkEnvOverride() {
	if env := os.Getenv("LOG_LEVEL"); env != "" {
		logLevel = env
	}
	if env := os.Getenv("LISTEN_ADDRESS"); env != "" {
		address = env
	}
	if env := os.Getenv("METRICS_PATH"); env != "" {
		metricPath = env
	}
	if env := os.Getenv("EXPORTER_CONFIG"); env != "" {
		configPath = env
	}
	if env := os.Getenv("EXPORTER_METRICS_NAMESPACE"); env != "" {
		exporterNamespace = env
	}
}

func main() {

	flag.StringVar(&logLevel, "log-level", "info", "log level")
	flag.StringVar(&address, "listen-address", ":9000", "address the web server binds to")
	flag.StringVar(&metricPath, "metrics-path", "/metrics", "path under which to expose metrics")
	flag.StringVar(&configPath, "exporter-config", "json-exporter.yaml", "exporter config file path")
	flag.StringVar(&exporterNamespace, "exporter-metrics-namespace", "json_exporter", "exporter's metrics namespace")

	flag.Usage = usage
	flag.Parse()

	checkEnvOverride()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var slogLevel = new(slog.LevelVar)
	slogLevel.UnmarshalText([]byte(logLevel))

	log = slog.New(slog.NewTextHandler(
		os.Stderr,
		&slog.HandlerOptions{
			Level: slogLevel,
		},
	))

	log = slog.Default()

	reg := prometheus.NewRegistry()
	mux := http.NewServeMux()
	server := &http.Server{
		Addr:              address,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       5 * time.Second,
		ReadHeaderTimeout: 1 * time.Second,
		Handler:           mux,
	}

	go gracefulShutdown(cancel, server)

	// collectorInputs is a map of collector id to its input channel
	// shared with webhooks
	collectorInputs := make(map[string]chan any)

	collectors, err := collector.New(configPath, reg, log, exporterNamespace)
	if err != nil {
		log.Error("unable to load collectors", "err", err)
		os.Exit(1)
	}

	for id, collector := range collectors {
		collectorInputs[id] = collector.Input
		log.Info("starting scrapper", "collector", id)
		go collector.Start(ctx)
	}

	webhooks, err := webhook.New(configPath, reg, log, collectorInputs, exporterNamespace)
	if err != nil {
		log.Error("unable to load webhooks", "err", err)
		os.Exit(1)
	}

	// register webhook handlers
	for id, wh := range webhooks {
		log.Info("registering webhook", "id", id)
		mux.Handle(wh.Path, wh)
	}

	mux.Handle(metricPath, promhttp.HandlerFor(reg,
		promhttp.HandlerOpts{Registry: reg},
	))

	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("unable to start server", "err", err)
		os.Exit(1)
	}

}

// controlled shutdown when terminate signal received.
func gracefulShutdown(cancel context.CancelFunc, server *http.Server) {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	<-stop
	log.Info("Shutting down")
	cancel()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := server.Shutdown(ctx)
	if err != nil {
		log.Error("Failed to shutdown http server", "err", err)
	}
}
