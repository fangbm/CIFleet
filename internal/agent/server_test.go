package agent

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fangbm/cifleet/internal/backend"
	"github.com/fangbm/cifleet/internal/model"
)

type fakeBackend struct {
	capacityCalls int
}

func (f *fakeBackend) Kind() model.BackendKind { return model.BackendDocker }
func (f *fakeBackend) Capacity(context.Context) (model.Capacity, error) {
	f.capacityCalls++
	return model.Capacity{TotalCPU: 4, FreeCPU: 4}, nil
}
func (f *fakeBackend) Create(context.Context, backend.JobSpec) (*backend.Instance, error) {
	return &backend.Instance{ID: "x", Backend: model.BackendDocker, NodeID: "e5"}, nil
}
func (f *fakeBackend) Destroy(context.Context, string) error                  { return nil }
func (f *fakeBackend) CleanupExpired(context.Context, time.Time) (int, error) { return 0, nil }

func TestWorkerAPIRejectsNonControllerCertificate(t *testing.T) {
	b := &fakeBackend{}
	s := &Server{NodeID: "e5", ControllerIdentity: "controller", Backend: b}
	req := httptest.NewRequest(http.MethodGet, "/v1/capacity", nil)
	req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{{Subject: pkix.Name{CommonName: "e5"}}}}
	rr := httptest.NewRecorder()

	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected %d, got %d: %s", http.StatusForbidden, rr.Code, rr.Body.String())
	}
	if b.capacityCalls != 0 {
		t.Fatalf("backend called despite rejected identity")
	}
}

func TestWorkerAPIAcceptsControllerCertificate(t *testing.T) {
	b := &fakeBackend{}
	s := &Server{NodeID: "e5", ControllerIdentity: "controller", Backend: b}
	req := httptest.NewRequest(http.MethodGet, "/v1/capacity", nil)
	req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{{Subject: pkix.Name{CommonName: "controller"}}}}
	rr := httptest.NewRecorder()

	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d: %s", http.StatusOK, rr.Code, rr.Body.String())
	}
	if b.capacityCalls != 1 {
		t.Fatalf("expected capacity call, got %d", b.capacityCalls)
	}
}
