package webhook

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
)

type WebHookHandler struct {
	*WebHook
	log        *slog.Logger
	collectors map[string]chan any
}

func New(
	configPath string,
	reg *prometheus.Registry,
	log *slog.Logger,
	collectorInputs map[string]chan any,
	exporterNamespace string,
) (map[string]*WebHookHandler, error) {

	initMetrics(reg, exporterNamespace)

	webhooks, err := loadConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("unable to load webhook config err:%w", err)
	}

	handlers := make(map[string]*WebHookHandler)
	for id, wh := range webhooks {
		h, err := webHookHandler(log, wh, collectorInputs)
		if err != nil {
			return nil, fmt.Errorf("unable to create webhook id:%s err:%w", id, err)
		}
		handlers[id] = h
	}
	return handlers, nil
}

func webHookHandler(log *slog.Logger, wh *WebHook, collectorInputs map[string]chan any) (*WebHookHandler, error) {
	var err error
	h := &WebHookHandler{
		WebHook: wh,
		log:     log.With("webhook", wh.id),
	}

	h.collectors = make(map[string]chan any)

	for i := range wh.Collectors {
		h.collectors[wh.Collectors[i].ID] = collectorInputs[wh.Collectors[i].ID]
		wh.Collectors[i].transformCode, err = parseAndCompileJQExp(wh.Collectors[i].Transform)
	}

	if err != nil {
		return nil, fmt.Errorf("unable to parse transform code err:%w", err)
	}

	// defaults
	if h.Response.Code == 0 {
		h.Response.Code = 200
	}
	return h, nil
}

func (wh *WebHookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer func() {
		io.Copy(io.Discard, r.Body)
		r.Body.Close()
	}()

	// only process expected method
	if r.Method != wh.Method {
		wh.log.Info("invalid request received", "received", r.Method, "expected", wh.Method)
		w.WriteHeader(http.StatusBadRequest)
		pcRequests.WithLabelValues(wh.id, "400").Inc()
		return
	}

	// verify headers
	if !isAuthHeadersMatching(r.Header, wh.Auth.Headers) {
		wh.log.Info("Unauthorised request received")
		w.WriteHeader(http.StatusUnauthorized)
		pcRequests.WithLabelValues(wh.id, "401").Inc()
		return
	}

	var payload any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		wh.log.Error("unable to parse json body", "err", err)
		w.WriteHeader(http.StatusBadRequest)
		pcRequests.WithLabelValues(wh.id, "400").Inc()
		return
	}

	// run transform code and send result to collector
	for _, c := range wh.Collectors {
		iter := c.transformCode.RunWithContext(r.Context(), payload)
		for {
			object, ok := iter.Next()
			if !ok {
				break
			}

			if err, ok := object.(error); ok {
				wh.log.Error("unable to transform", "err", err)
				// todo: should we send 500 to server?
				break
			}
			wh.collectors[c.ID] <- object
		}
	}

	for _, h := range wh.Response.Headers {
		w.Header().Set(h.Name, h.GetValue())
	}
	w.WriteHeader(wh.Response.Code)
	w.Write([]byte(wh.Response.Message))
	pcRequests.WithLabelValues(wh.id, strconv.Itoa(wh.Response.Code)).Inc()
}

func isAuthHeadersMatching(reqHeaders http.Header, expected []Header) bool {
	verified := true

	for _, eh := range expected {
		if reqHeaders.Get(eh.Name) != eh.GetValue() {
			verified = false
		}
	}

	return verified
}
