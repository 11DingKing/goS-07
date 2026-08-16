package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"ejina-microgrid/internal/app"
	"ejina-microgrid/internal/config"
	"ejina-microgrid/internal/domain/battery"
	"ejina-microgrid/internal/domain/grid"
	"ejina-microgrid/internal/domain/workorder"
)

func newTestServer(t *testing.T) (*Server, *app.App) {
	t.Helper()
	cfg := config.Default()
	a := app.New(cfg, nil)
	return New(a), a
}

func doRequest(t *testing.T, s *Server, method, path string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func TestHTTPCreateCabinOrderAndAccept(t *testing.T) {
	s, _ := newTestServer(t)
	// Register a cabin.
	rec := doRequest(t, s, "POST", "/api/cabins", map[string]interface{}{
		"id": "c1", "capacity": 100, "soc": 80, "temperature": 25, "insulation": 5, "online": true,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("register cabin: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	// List cabins.
	rec = doRequest(t, s, "GET", "/api/cabins", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list cabins: expected 200, got %d", rec.Code)
	}
	var cabins []battery.Cabin
	json.Unmarshal(rec.Body.Bytes(), &cabins)
	if len(cabins) != 1 {
		t.Fatalf("expected 1 cabin, got %d", len(cabins))
	}
	// Create an order.
	rec = doRequest(t, s, "POST", "/api/orders", map[string]interface{}{
		"cabin_id": "c1", "type": "temperature", "severity": "urgent",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create order: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var order workorder.Order
	json.Unmarshal(rec.Body.Bytes(), &order)
	if order.Status != workorder.StatusOpen {
		t.Fatalf("expected open, got %s", order.Status)
	}
	// Accept the order.
	rec = doRequest(t, s, "POST", "/api/orders/"+order.ID+"/accept", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("accept: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	json.Unmarshal(rec.Body.Bytes(), &order)
	if order.Status != workorder.StatusAccepted {
		t.Fatalf("expected accepted, got %s", order.Status)
	}
	// Resolve.
	rec = doRequest(t, s, "POST", "/api/orders/"+order.ID+"/resolve", map[string]string{"result": "fixed"})
	if rec.Code != http.StatusOK {
		t.Fatalf("resolve: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHTTPBlackStartFlow(t *testing.T) {
	s, _ := newTestServer(t)
	// Lose external grid.
	rec := doRequest(t, s, "POST", "/api/grid/lose", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("lose grid: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	// Black start.
	rec = doRequest(t, s, "POST", "/api/grid/blackstart", map[string]string{"id": "bs-1"})
	if rec.Code != http.StatusOK {
		t.Fatalf("black start: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	// Reconnect should be queued.
	rec = doRequest(t, s, "POST", "/api/grid/reconnect", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("reconnect: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["status"] != "queued" {
		t.Errorf("expected queued, got %s", resp["status"])
	}
	// Complete black start.
	rec = doRequest(t, s, "POST", "/api/grid/complete-blackstart", map[string]string{"id": "bs-1"})
	if rec.Code != http.StatusOK {
		t.Fatalf("complete black start: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	// Grid state should be syncing.
	rec = doRequest(t, s, "GET", "/api/grid/state", nil)
	var gs grid.Snapshot
	json.Unmarshal(rec.Body.Bytes(), &gs)
	if gs.State != grid.StateSyncing {
		t.Errorf("expected syncing, got %s", gs.State)
	}
	// Complete sync.
	rec = doRequest(t, s, "POST", "/api/grid/complete-sync", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("complete sync: expected 200, got %d", rec.Code)
	}
	rec = doRequest(t, s, "GET", "/api/grid/state", nil)
	json.Unmarshal(rec.Body.Bytes(), &gs)
	if gs.State != grid.StateConnected {
		t.Errorf("expected connected, got %s", gs.State)
	}
}

func TestHTTPHeartbeatAndFailover(t *testing.T) {
	s, a := newTestServer(t)
	// Send heartbeat.
	rec := doRequest(t, s, "POST", "/api/controller/heartbeat", map[string]string{"controller_id": "master-01"})
	if rec.Code != http.StatusOK {
		t.Fatalf("heartbeat: expected 200, got %d", rec.Code)
	}
	// Manual failover should fail while healthy.
	rec = doRequest(t, s, "POST", "/api/controller/failover", nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("failover while healthy: expected 409, got %d", rec.Code)
	}
	// Advance clock past heartbeat timeout.
	a.SetClock(func() time.Time { return time.Now().Add(30 * time.Second) })
	rec = doRequest(t, s, "POST", "/api/controller/failover", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("failover: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	rec = doRequest(t, s, "GET", "/api/controller/state", nil)
	var cresp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &cresp)
	if cresp["active_id"] != "backup-01" {
		t.Errorf("expected backup-01, got %v", cresp["active_id"])
	}
}

func TestHTTPNotFoundCabin(t *testing.T) {
	s, _ := newTestServer(t)
	rec := doRequest(t, s, "POST", "/api/cabins/missing/offline", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestHTTPHealth(t *testing.T) {
	s, _ := newTestServer(t)
	rec := doRequest(t, s, "GET", "/health", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}
