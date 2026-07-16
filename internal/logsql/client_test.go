package logsql

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/truepace-io-oss/observability-mcp-server/internal/datasources"
)

func TestQueryNDJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/select/logsql/query" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_ = r.ParseForm()
		if r.Form.Get("query") != "level:error" {
			t.Errorf("query = %s", r.Form.Get("query"))
		}
		_, _ = w.Write([]byte("{\"_time\":\"2026-01-01T00:00:00Z\",\"_msg\":\"boom\"}\n{\"_time\":\"2026-01-01T00:00:01Z\",\"_msg\":\"bang\"}\n"))
	}))
	t.Cleanup(srv.Close)
	c := New(datasources.NewForTest("logs", "victorialogs", srv.URL, false))

	recs, err := c.Query(context.Background(), "level:error", "", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 || recs[0]["_msg"] != "boom" {
		t.Fatalf("bad records: %+v", recs)
	}
}

func TestQueryLimitCap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for i := 0; i < 50; i++ {
			_, _ = w.Write([]byte("{\"_msg\":\"x\"}\n"))
		}
	}))
	t.Cleanup(srv.Close)
	c := New(datasources.NewForTest("logs", "victorialogs", srv.URL, false))
	recs, err := c.Query(context.Background(), "*", "", "", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 5 {
		t.Fatalf("limit not enforced: got %d", len(recs))
	}
}
