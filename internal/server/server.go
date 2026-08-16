// Package server provides the HTTP API for the dispatch service. It maps REST
// endpoints to App operations and returns JSON responses.
package server

import (
	"encoding/json"
	"net/http"
	"strconv"

	"ejina-microgrid/internal/app"
	"ejina-microgrid/internal/domain/battery"
	"ejina-microgrid/internal/domain/workorder"
)

// Server wraps the HTTP handler with the application core.
type Server struct {
	app *app.App
	mux *http.ServeMux
}

// New creates a new HTTP server.
func New(a *app.App) *Server {
	s := &Server{app: a, mux: http.NewServeMux()}
	s.routes()
	return s
}

// Handler returns the HTTP handler.
func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) routes() {
	s.mux.HandleFunc("GET /health", s.health)
	s.mux.HandleFunc("POST /api/cabins", s.registerCabin)
	s.mux.HandleFunc("GET /api/cabins", s.listCabins)
	s.mux.HandleFunc("PATCH /api/cabins/{id}/sensors", s.updateSensors)
	s.mux.HandleFunc("POST /api/cabins/{id}/offline", s.setOffline)
	s.mux.HandleFunc("POST /api/cabins/{id}/online", s.setOnline)
	s.mux.HandleFunc("POST /api/cabins/{id}/inspect", s.inspectCabin)
	s.mux.HandleFunc("POST /api/orders", s.createOrder)
	s.mux.HandleFunc("GET /api/orders", s.listOrders)
	s.mux.HandleFunc("POST /api/orders/{id}/accept", s.acceptOrder)
	s.mux.HandleFunc("POST /api/orders/{id}/resolve", s.resolveOrder)
	s.mux.HandleFunc("POST /api/grid/lose", s.loseExternalGrid)
	s.mux.HandleFunc("POST /api/grid/blackstart", s.blackStart)
	s.mux.HandleFunc("POST /api/grid/reconnect", s.reconnect)
	s.mux.HandleFunc("POST /api/grid/complete-blackstart", s.completeBlackStart)
	s.mux.HandleFunc("POST /api/grid/complete-sync", s.completeSync)
	s.mux.HandleFunc("GET /api/grid/state", s.gridState)
	s.mux.HandleFunc("POST /api/controller/heartbeat", s.heartbeat)
	s.mux.HandleFunc("POST /api/controller/failover", s.failover)
	s.mux.HandleFunc("GET /api/controller/state", s.controllerState)
	s.mux.HandleFunc("POST /api/fleet/demand", s.setDemand)
	s.mux.HandleFunc("GET /api/snapshot", s.getSnapshot)
	s.mux.HandleFunc("POST /api/snapshot/save", s.saveSnapshot)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// --- Cabin endpoints ---

type registerCabinReq struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Capacity    float64 `json:"capacity"`
	SOC         float64 `json:"soc"`
	Temperature float64 `json:"temperature"`
	Insulation  float64 `json:"insulation"`
	Online      bool    `json:"online"`
}

func (s *Server) registerCabin(w http.ResponseWriter, r *http.Request) {
	var req registerCabinReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	c := battery.Cabin{
		ID:          req.ID,
		Name:        req.Name,
		Capacity:    req.Capacity,
		SOC:         req.SOC,
		Temperature: req.Temperature,
		Insulation:  req.Insulation,
		Online:      req.Online,
	}
	if err := s.app.RegisterCabin(c); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": req.ID, "status": "registered"})
}

func (s *Server) listCabins(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.app.ListCabins())
}

type updateSensorsReq struct {
	SOC         float64 `json:"soc"`
	Temperature float64 `json:"temperature"`
	Insulation  float64 `json:"insulation"`
}

func (s *Server) updateSensors(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req updateSensorsReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.app.UpdateCabinSensors(id, req.SOC, req.Temperature, req.Insulation); err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (s *Server) setOffline(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.app.SetCabinOffline(id); err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "offline"})
}

func (s *Server) setOnline(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.app.SetCabinOnline(id); err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "online"})
}

func (s *Server) inspectCabin(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	anomalies, err := s.app.InspectCabin(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"cabin_id": id, "anomalies": anomalies})
}

// --- Work order endpoints ---

type createOrderReq struct {
	CabinID  string              `json:"cabin_id"`
	Type     battery.AnomalyType `json:"type"`
	Severity workorder.Severity  `json:"severity"`
}

func (s *Server) createOrder(w http.ResponseWriter, r *http.Request) {
	var req createOrderReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	o, err := s.app.CreateOrder(req.CabinID, req.Type, req.Severity)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, o)
}

func (s *Server) listOrders(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.app.ListOrders())
}

func (s *Server) acceptOrder(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	o, err := s.app.AcceptOrder(id)
	if err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, o)
}

type resolveOrderReq struct {
	Result string `json:"result"`
}

func (s *Server) resolveOrder(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req resolveOrderReq
	_ = decodeJSON(r, &req) // result is optional
	o, err := s.app.ResolveOrder(id, req.Result)
	if err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, o)
}

// --- Grid endpoints ---

func (s *Server) loseExternalGrid(w http.ResponseWriter, r *http.Request) {
	if err := s.app.LoseExternalGrid(); err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "off_grid"})
}

type blackStartReq struct {
	ID string `json:"id"`
}

func (s *Server) blackStart(w http.ResponseWriter, r *http.Request) {
	var req blackStartReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.app.BlackStart(req.ID); err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "black_starting", "id": req.ID})
}

func (s *Server) reconnect(w http.ResponseWriter, r *http.Request) {
	queued, err := s.app.RequestReconnect()
	if err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	status := "syncing"
	if queued {
		status = "queued"
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": status})
}

func (s *Server) completeBlackStart(w http.ResponseWriter, r *http.Request) {
	var req blackStartReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.app.CompleteBlackStart(req.ID); err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "completed"})
}

func (s *Server) completeSync(w http.ResponseWriter, r *http.Request) {
	s.app.CompleteSync()
	writeJSON(w, http.StatusOK, map[string]string{"status": "connected"})
}

func (s *Server) gridState(w http.ResponseWriter, r *http.Request) {
	snap := s.app.GridSnapshot()
	writeJSON(w, http.StatusOK, snap)
}

// --- Controller endpoints ---

type heartbeatReq struct {
	ControllerID string `json:"controller_id"`
}

func (s *Server) heartbeat(w http.ResponseWriter, r *http.Request) {
	var req heartbeatReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.app.Heartbeat(req.ControllerID); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) failover(w http.ResponseWriter, r *http.Request) {
	if err := s.app.TriggerFailover(); err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "failed_over"})
}

func (s *Server) controllerState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.app.ControllerSnapshot())
}

// --- Fleet endpoints ---

type setDemandReq struct {
	Demand float64 `json:"demand"`
}

func (s *Server) setDemand(w http.ResponseWriter, r *http.Request) {
	var req setDemandReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.app.SetFleetDemand(req.Demand)
	writeJSON(w, http.StatusOK, map[string]float64{"demand": req.Demand})
}

// --- Snapshot endpoints ---

func (s *Server) getSnapshot(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.app.Snapshot())
}

func (s *Server) saveSnapshot(w http.ResponseWriter, r *http.Request) {
	if err := s.app.SaveSnapshot(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

// --- helpers ---

func decodeJSON(r *http.Request, v interface{}) error {
	dec := json.NewDecoder(r.Body)
	defer r.Body.Close()
	return dec.Decode(v)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

// ParseFloatQueryParam extracts a float query parameter.
func ParseFloatQueryParam(r *http.Request, key string) (float64, bool) {
	v := r.URL.Query().Get(key)
	if v == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}
