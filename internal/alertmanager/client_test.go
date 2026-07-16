package alertmanager

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/truepace-io-oss/observability-mcp-server/internal/datasources"
)

func TestAlertsAndSilenceLifecycle(t *testing.T) {
	var created PostableSilence
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2/alerts":
			_, _ = w.Write([]byte(`[{"labels":{"alertname":"HighCPU"},"status":{"state":"active"}}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v2/silences":
			_ = json.NewDecoder(r.Body).Decode(&created)
			_, _ = w.Write([]byte(`{"silenceID":"sil-123"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v2/silence/sil-123":
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	c := New(datasources.NewForTest("am", "alertmanager", srv.URL, false))

	alerts, err := c.Alerts(context.Background(), true, false, false, nil)
	if err != nil || len(alerts) != 1 || alerts[0].Labels["alertname"] != "HighCPU" {
		t.Fatalf("alerts err=%v got=%+v", err, alerts)
	}

	id, err := c.CreateSilence(context.Background(), PostableSilence{
		Matchers:  []Matcher{{Name: "alertname", Value: "HighCPU"}},
		CreatedBy: "test", Comment: "maint",
	})
	if err != nil || id != "sil-123" {
		t.Fatalf("create err=%v id=%q", err, id)
	}
	if created.CreatedBy != "test" {
		t.Fatalf("body not posted: %+v", created)
	}
	if err := c.DeleteSilence(context.Background(), "sil-123"); err != nil {
		t.Fatalf("delete err=%v", err)
	}
}
