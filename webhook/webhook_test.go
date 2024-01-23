package webhook

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/prometheus/client_golang/prometheus"
)

func TestWebHookHandler_ServeHTTP(t *testing.T) {
	reg := prometheus.NewPedanticRegistry()
	log := slog.Default()

	os.Setenv("TEST_SHARED_WEB_HOOK_KEY", "test-shared-key")

	collectorInputs := make(map[string]chan any)
	collectorInputs["example"] = make(chan any)
	collectorInputs["animals"] = make(chan any)

	go func() {
		for {
			select {
			case input := <-collectorInputs["example"]:
				fmt.Println(input)
			case input := <-collectorInputs["animals"]:
				t.Errorf("input received on wrong channel input %s", input)
			}
		}
	}()
	time.Sleep(time.Second)

	webhooks, err := New("../test/config.yaml", reg, log, collectorInputs, "test_exporter")
	if err != nil {
		t.Fatal(err)
	}

	type req struct {
		method      string
		path        string
		headerKey   string
		headerValue string
		body        string
	}

	tests := []struct {
		name      string
		req       req
		respSatus int
		exptBody  string
	}{
		{
			"valid",
			req{"POST", "/webhook/example", "Authorization", "test-shared-key", `{"something":"some"}`},
			200, "ok",
		},
		{
			"wrong-key",
			req{"POST", "/webhook/example", "Authorization", "random-key", "{}"},
			401, "",
		},
		{
			"wrong-method",
			req{"GET", "/webhook/example", "Authorization", "random-key", "{}"},
			400, "",
		},
		{
			"invalid-payload",
			req{"POST", "/webhook/example", "Authorization", "test-shared-key", `not-json`},
			400, "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			req, err := http.NewRequest(tt.req.method, tt.req.path, strings.NewReader(tt.req.body))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set(tt.req.headerKey, tt.req.headerValue)
			rr := httptest.NewRecorder()
			webhooks["example"].ServeHTTP(rr, req)

			// Check the status code and body is what we expect.
			if status := rr.Code; status != tt.respSatus {
				t.Errorf("ServeHTTP() handler returned wrong status code: got %v want %v",
					status, tt.respSatus)
			}
			if rr.Body.String() != tt.exptBody {
				t.Errorf("ServeHTTP() handler returned unexpected body: got %v want %v",
					rr.Body.String(), tt.exptBody)
			}
		})
	}
}

func TestWebHookHandler_ServeHTTP_Transform(t *testing.T) {
	reg := prometheus.NewPedanticRegistry()
	log := slog.Default()

	initMetrics(reg, "test_json")

	os.Setenv("TEST_SHARED_WEB_HOOK_KEY", "test-shared-key")

	collectorInputs := make(map[string]chan any)
	collectorInputs["example"] = make(chan any)

	type args struct {
		transform string
		body      string
	}

	tests := []struct {
		name   string
		args   args
		output any
	}{
		{
			"no-change",
			args{".", `{"something":"some"}`},
			map[string]any{"something": "some"},
		},
		{
			"nested-array",
			args{
				".values", `
			{
				"counter": 1234,
				"timestamp": 1657568506,
				"values": [
					{"id": "id-A","count": 2},
					{"id": "id-B","count": 5},
					{"id": "id-C","count": 3}
				],
				"location": "mars"
			}`},
			[]any{
				map[string]any{"count": float64(2), "id": "id-A"},
				map[string]any{"count": float64(5), "id": "id-B"},
				map[string]any{"count": float64(3), "id": "id-C"},
			},
		},
		{
			"nested-object",
			args{
				".values[0]",
				`{
				"counter": 1234,
				"timestamp": 1657568506,
				"values": [
					{"id": "id-A","count": 2},
					{"id": "id-B","count": 5},
					{"id": "id-C","count": 3}
				],
				"location": "mars"
			}`},
			map[string]any{"count": float64(2), "id": "id-A"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			wh := &WebHook{
				id:         "example",
				Method:     "POST",
				Path:       "/",
				Collectors: []Collector{{ID: "example", Transform: tt.args.transform}},
			}

			webhook, err := webHookHandler(log, wh, collectorInputs)
			if err != nil {
				t.Fatal(err)
			}

			req, err := http.NewRequest("POST", "/", strings.NewReader(tt.args.body))
			if err != nil {
				t.Fatal(err)
			}
			rr := httptest.NewRecorder()

			go webhook.ServeHTTP(rr, req)

			input := <-collectorInputs["example"]

			if diff := cmp.Diff(input, tt.output); diff != "" {
				t.Errorf("TestWebHookHandler_ServeHTTP_Transform transform mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_isAuthHeadersMatching(t *testing.T) {
	type args struct {
		reqHeaders http.Header
		expected   []Header
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			"no-header",
			args{reqHeaders: map[string][]string{}, expected: nil},
			true,
		},
		{
			"1-header-missing",
			args{reqHeaders: map[string][]string{}, expected: []Header{{Name: "Auth", Value: "secret"}}},
			false,
		},
		{
			"1-header-value-doesnt-match",
			args{reqHeaders: map[string][]string{"auth": {"some-thing-else"}}, expected: []Header{{Name: "Auth", Value: "secret"}}},
			false,
		},
		{
			"1-header-key-doesnt-match",
			args{reqHeaders: map[string][]string{"authorization": {"secret"}}, expected: []Header{{Name: "Auth", Value: "secret"}}},
			false,
		},
		{
			"1-header-matching",
			args{reqHeaders: map[string][]string{"Auth": {"secret"}}, expected: []Header{{Name: "Auth", Value: "secret"}}},
			true,
		},
		{
			"2-headers-missing",
			args{reqHeaders: map[string][]string{}, expected: []Header{{Name: "Auth", Value: "secret"}, {Name: "Token", Value: "1234"}}},
			false,
		}, {
			"2-headers-missing-1-matching",
			args{reqHeaders: map[string][]string{"Auth": {"secret"}}, expected: []Header{{Name: "Auth", Value: "secret"}, {Name: "Token", Value: "1234"}}},
			false,
		}, {
			"2-headers-missing-1-matching",
			args{reqHeaders: map[string][]string{"Token": {"1234"}}, expected: []Header{{Name: "Auth", Value: "secret"}, {Name: "Token", Value: "1234"}}},
			false,
		}, {
			"2-headers-missing-1-matching",
			args{reqHeaders: map[string][]string{"Token": {"1234"}, "Auth": {"secret"}}, expected: []Header{{Name: "Auth", Value: "secret"}, {Name: "Token", Value: "1234"}}},
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isAuthHeadersMatching(tt.args.reqHeaders, tt.args.expected); got != tt.want {
				t.Errorf("isAuthHeadersMatching() = %v, want %v", got, tt.want)
			}
		})
	}
}
