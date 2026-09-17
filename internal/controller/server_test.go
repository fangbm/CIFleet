package controller

import (
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fangbm/cifleet/internal/model"
)

func TestHeartbeatBindsCertificateCNToNodeID(t *testing.T) {
	s := New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	req := httptest.NewRequest(http.MethodPost, "/v1/nodes/heartbeat", strings.NewReader(`{
		"id":"e5","os":"linux","arch":"amd64","backends":["docker"],"capabilities":["container"],"capacity":{}
	}`))
	req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{{Subject: pkix.Name{CommonName: "not-e5"}}}}
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected %d, got %d: %s", http.StatusForbidden, rr.Code, rr.Body.String())
	}
}

func TestHeartbeatAcceptsMatchingCertificateCN(t *testing.T) {
	s := New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	req := httptest.NewRequest(http.MethodPost, "/v1/nodes/heartbeat", strings.NewReader(`{
		"id":"e5","os":"linux","arch":"amd64","backends":["docker"],"capabilities":["container"],"capacity":{"free_cpu":4}
	}`))
	req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{{Subject: pkix.Name{CommonName: "e5"}}}}
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected %d, got %d: %s", http.StatusAccepted, rr.Code, rr.Body.String())
	}
	if got := s.snapshotNodes(time.Now().UTC()); len(got) != 1 || got[0].ID != "e5" || !got[0].Online {
		t.Fatalf("unexpected nodes: %+v", got)
	}
}

func TestSnapshotMarksStaleNodeOffline(t *testing.T) {
	s := New(slog.New(slog.NewTextHandler(io.Discard, nil)), 10*time.Second)
	s.nodes["e5"] = nodeForTest("e5", time.Now().UTC().Add(-11*time.Second))
	got := s.snapshotNodes(time.Now().UTC())
	if len(got) != 1 || got[0].Online {
		t.Fatalf("expected stale node to be offline: %+v", got)
	}
}

func nodeForTest(id string, lastSeen time.Time) model.Node {
	return model.Node{ID: id, Online: true, LastSeen: lastSeen}
}
