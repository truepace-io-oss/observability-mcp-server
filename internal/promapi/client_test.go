package promapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/truepace-io-oss/observability-mcp-server/internal/datasources"
)

func newClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return New(datasources.NewForTest("vm", "victoriametrics", srv.URL, false))
}

func TestQueryVector(t *testing.T) {
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/query" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.URL.Query().Get("query") != "up" {
			t.Errorf("query = %s", r.URL.Query().Get("query"))
		}
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[{"metric":{"__name__":"up","job":"x"},"value":[1700000000,"1"]}]}}`))
	})
	res, err := c.Query(context.Background(), "up", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Type != model.ValVector || len(res.Vector) != 1 {
		t.Fatalf("bad result: %+v", res)
	}
	if res.Vector[0].Value != 1 {
		t.Fatalf("value = %v", res.Vector[0].Value)
	}
}

func TestQueryAPIError(t *testing.T) {
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"error","errorType":"bad_data","error":"parse error"}`))
	})
	if _, err := c.Query(context.Background(), "((", ""); err == nil {
		t.Fatal("expected error")
	}
}

func TestLabelsAndRules(t *testing.T) {
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/labels":
			_, _ = w.Write([]byte(`{"status":"success","data":["__name__","job","instance"]}`))
		case "/api/v1/rules":
			_, _ = w.Write([]byte(`{"status":"success","data":{"groups":[{"name":"g","file":"f","rules":[{"name":"HighCPU","query":"cpu>1","type":"alerting","state":"firing"}]}]}}`))
		default:
			http.NotFound(w, r)
		}
	})
	labels, err := c.Labels(context.Background(), nil, "", "")
	if err != nil || len(labels) != 3 {
		t.Fatalf("labels err=%v n=%d", err, len(labels))
	}
	groups, err := c.Rules(context.Background(), "")
	if err != nil || len(groups) != 1 || groups[0].Rules[0].Name != "HighCPU" {
		t.Fatalf("rules err=%v groups=%+v", err, groups)
	}
}
