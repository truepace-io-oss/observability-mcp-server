package datasources

import (
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/truepace-io-oss/observability-mcp-server/internal/config"
)

// loadCACertPool builds an x509 pool from the datasource TLS config, or returns
// nil (meaning "use the system roots") when no explicit CA is configured.
func loadCACertPool(tls config.DatasourceTLS) (*x509.CertPool, error) {
	var pem []byte
	switch {
	case tls.CAFile != "":
		b, err := os.ReadFile(tls.CAFile)
		if err != nil {
			return nil, fmt.Errorf("read caFile: %w", err)
		}
		pem = b
	case tls.CAData != "":
		b, err := base64.StdEncoding.DecodeString(tls.CAData)
		if err != nil {
			return nil, fmt.Errorf("caData is not valid base64: %w", err)
		}
		pem = b
	default:
		return nil, nil
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("no valid certificates found in CA")
	}
	return pool, nil
}

// authTransport injects datasource-side auth (basic or bearer) into every
// request. File-backed credentials are re-read per request so rotation
// (ESO / projected files) is picked up without a restart.
type authTransport struct {
	next http.RoundTripper
	auth config.DatasourceAuth
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	switch {
	case t.auth.Basic != nil:
		pass, err := valueOrFile(t.auth.Basic.Password, t.auth.Basic.PasswordFile)
		if err != nil {
			return nil, fmt.Errorf("basic auth: %w", err)
		}
		req = cloneReq(req)
		req.SetBasicAuth(t.auth.Basic.Username, pass)
	case t.auth.Bearer != nil:
		tok, err := valueOrFile(t.auth.Bearer.Token, t.auth.Bearer.TokenFile)
		if err != nil {
			return nil, fmt.Errorf("bearer auth: %w", err)
		}
		req = cloneReq(req)
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	return t.next.RoundTrip(req)
}

// valueOrFile returns the inline value, or the trimmed contents of the file.
func valueOrFile(inline, file string) (string, error) {
	if file != "" {
		b, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("read %q: %w", file, err)
		}
		return strings.TrimSpace(string(b)), nil
	}
	return inline, nil
}

// cloneReq shallow-clones a request so we can mutate headers without touching the
// caller's request (RoundTrippers must not modify the request they are given).
func cloneReq(req *http.Request) *http.Request {
	r2 := req.Clone(req.Context())
	return r2
}
