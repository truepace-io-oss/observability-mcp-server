package datasources

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"time"

	"github.com/truepace-io-oss/observability-mcp-server/internal/config"
	"github.com/truepace-io-oss/observability-mcp-server/internal/metrics"
)

// buildHTTPClient assembles the HTTP client for one datasource: a base transport
// with the configured TLS trust, wrapped with (optional) datasource auth and the
// outbound-metrics RoundTripper, with the configured request timeout.
func buildHTTPClient(ds config.DatasourceConfig) (*http.Client, error) {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("unexpected default transport type")
	}
	tr := base.Clone()

	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if ds.TLS.InsecureSkipTLSVerify {
		tlsCfg.InsecureSkipVerify = true
	} else {
		pool, err := loadCACertPool(ds.TLS)
		if err != nil {
			return nil, fmt.Errorf("datasource %q: %w", ds.Name, err)
		}
		if pool != nil {
			tlsCfg.RootCAs = pool
		}
	}
	tr.TLSClientConfig = tlsCfg

	var rt http.RoundTripper = tr
	if ds.Auth.Basic != nil || ds.Auth.Bearer != nil {
		rt = &authTransport{next: rt, auth: ds.Auth}
	}
	rt = metrics.NewRoundTripper(rt, ds.Name, ds.Type)

	timeout := 30 * time.Second
	if ds.Timeout != "" {
		d, err := time.ParseDuration(ds.Timeout)
		if err != nil {
			return nil, fmt.Errorf("datasource %q: invalid timeout: %w", ds.Name, err)
		}
		timeout = d
	}

	return &http.Client{Transport: rt, Timeout: timeout}, nil
}
