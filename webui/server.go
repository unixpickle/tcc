package main

import (
	"embed"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"strings"

	"github.com/unixpickle/tcc/thermostat"
)

//go:embed static/*
var staticFiles embed.FS

type Handler struct {
	backend thermostat.Backend
	static  http.Handler
}

func newHandler(backend thermostat.Backend) *Handler {
	static, err := fs.Sub(staticFiles, "static")
	if err != nil {
		panic(err)
	}
	return &Handler{backend: backend, static: http.FileServer(http.FS(static))}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		h.serveAPI(w, r)
		return
	}
	h.static.ServeHTTP(w, r)
}

type devicesResponse struct {
	Devices []thermostat.Device `json:"devices"`
}

func (h *Handler) serveAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && r.URL.Path == "/api/devices" {
		devices, err := h.backend.Devices()
		if err != nil {
			writeBackendError(w, err)
			return
		}
		if devices == nil {
			devices = []thermostat.Device{}
		}
		writeJSON(w, http.StatusOK, devicesResponse{Devices: devices})
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/api/devices/") {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/devices/"), "/")
	if parts[0] == "" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	id := parts[0]
	if len(parts) == 1 && r.Method == http.MethodGet {
		h.writeDevice(w, id)
		return
	}
	if len(parts) != 2 || r.Method != http.MethodPost {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	var err error
	switch parts[1] {
	case "temperature":
		var request struct {
			Temperature *float64 `json:"temperature"`
			System      string   `json:"system"`
		}
		if !decodeJSON(w, r, &request) {
			return
		}
		if request.Temperature == nil {
			writeError(w, http.StatusBadRequest, "temperature is required")
			return
		}
		err = h.backend.SetTemperature(id, *request.Temperature, strings.ToLower(request.System))
	case "system":
		var request struct {
			System string `json:"system"`
		}
		if !decodeJSON(w, r, &request) {
			return
		}
		err = h.backend.SetSystem(id, strings.ToLower(request.System))
	case "fan":
		var request struct {
			Fan string `json:"fan"`
		}
		if !decodeJSON(w, r, &request) {
			return
		}
		err = h.backend.SetFan(id, strings.ToLower(request.Fan))
	default:
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeBackendError(w, err)
		return
	}
	h.writeDevice(w, id)
}

func (h *Handler) writeDevice(w http.ResponseWriter, id string) {
	d, err := h.backend.Device(id)
	if err != nil {
		writeBackendError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	defer r.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeBackendError(w http.ResponseWriter, err error) {
	status := http.StatusBadGateway
	switch {
	case errors.Is(err, thermostat.ErrUnauthorized):
		status = http.StatusUnauthorized
	case errors.Is(err, thermostat.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, thermostat.ErrInvalid):
		status = http.StatusBadRequest
	case errors.Is(err, thermostat.ErrConflict):
		status = http.StatusConflict
	}
	writeError(w, status, err.Error())
}
