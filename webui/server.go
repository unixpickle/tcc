package main

import (
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/unixpickle/tcc"
)

//go:embed static/*
var staticFiles embed.FS

type session interface {
	RefreshZones() error
	Zones() []tcc.Zone
	ZoneInfo(tcc.ZoneID) (*tcc.ZoneInfo, error)
	SubmitControlChanges(tcc.ZoneID, tcc.ControlChanges) error
}

type Handler struct {
	session session
	mu      sync.Mutex
	static  http.Handler
}

func main() {
	addr := flag.String("addr", ":8080", "HTTP listen address")
	rootFlag := flag.String("root", "", "single path segment under which to serve the UI and API")
	flag.Usage = usage
	flag.Parse()
	if flag.NArg() != 0 {
		usage()
		os.Exit(2)
	}
	root, err := cleanRoot(*rootFlag)
	if err != nil {
		log.Fatal(err)
	}
	username := os.Getenv("TCC_USERNAME")
	password := os.Getenv("TCC_PASSWORD")
	if username == "" || password == "" {
		log.Fatal("TCC_USERNAME and TCC_PASSWORD must be set")
	}
	session, err := tcc.NewSession(username, password)
	if err != nil {
		log.Fatal(err)
	}
	handler := mountRoot(newHandler(session), root)
	if root == "" {
		log.Printf("serving TCC web UI at http://localhost%s/", displayAddr(*addr))
	} else {
		log.Printf("serving TCC web UI at http://localhost%s/%s/", displayAddr(*addr), root)
	}
	log.Fatal(http.ListenAndServe(*addr, handler))
}

func newHandler(session session) http.Handler {
	static, err := fs.Sub(staticFiles, "static")
	if err != nil {
		panic(err)
	}
	return &Handler{
		session: session,
		static:  http.FileServer(http.FS(static)),
	}
}

func cleanRoot(value string) (string, error) {
	root := strings.Trim(value, "/")
	if root == "" {
		return "", nil
	}
	if strings.Contains(root, "/") {
		return "", fmt.Errorf("root must be a single directory name, got %q", value)
	}
	if strings.Contains(root, "..") {
		return "", fmt.Errorf("root cannot contain '..', got %q", value)
	}
	return root, nil
}

func mountRoot(handler http.Handler, root string) http.Handler {
	if root == "" {
		return handler
	}
	mount := "/" + root
	mux := http.NewServeMux()
	mux.Handle(mount+"/", http.StripPrefix(mount, handler))
	mux.HandleFunc(mount, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, mount+"/", http.StatusMovedPermanently)
	})
	return mux
}

func displayAddr(addr string) string {
	if strings.HasPrefix(addr, ":") {
		return addr
	}
	return " " + addr
}

func usage() {
	output := flag.CommandLine.Output()
	fmt.Fprintf(output, "usage: %s [-addr address] [-root name]\n", os.Args[0])
	fmt.Fprintln(output)
	fmt.Fprintln(output, "environment: TCC_USERNAME, TCC_PASSWORD")
	fmt.Fprintln(output)
	fmt.Fprintln(output, "flags:")
	flag.PrintDefaults()
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		h.serveAPI(w, r)
		return
	}
	h.static.ServeHTTP(w, r)
}

func (h *Handler) serveAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && r.URL.Path == "/api/devices" {
		h.serveDevices(w, r)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/devices/"), "/")
	if len(parts) == 1 && r.Method == http.MethodGet {
		h.serveDevice(w, r, parts[0])
		return
	}
	if len(parts) == 2 && r.Method == http.MethodPost {
		switch parts[1] {
		case "temperature":
			h.serveSetTemperature(w, r, parts[0])
			return
		case "system":
			h.serveSetSystem(w, r, parts[0])
			return
		case "fan":
			h.serveSetFan(w, r, parts[0])
			return
		}
	}
	writeError(w, http.StatusNotFound, "not found")
}

func (h *Handler) serveDevices(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.session.RefreshZones(); err != nil {
		writeBackendError(w, err)
		return
	}
	var devices []deviceResponse
	for _, zone := range h.session.Zones() {
		device, err := h.deviceResponseLocked(zone.ID)
		if err != nil {
			writeBackendError(w, err)
			return
		}
		device.Name = zone.Name
		device.Temperature = zone.Temperature
		device.Humidity = zone.Humidity
		devices = append(devices, device)
	}
	writeJSON(w, http.StatusOK, devicesResponse{Devices: devices})
}

func (h *Handler) serveDevice(w http.ResponseWriter, r *http.Request, idPart string) {
	zoneID, ok := parseZoneID(w, idPart)
	if !ok {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	device, err := h.deviceResponseLocked(zoneID)
	if err != nil {
		writeBackendError(w, err)
		return
	}
	for _, zone := range h.session.Zones() {
		if zone.ID == zoneID {
			device.Name = zone.Name
			device.Temperature = zone.Temperature
			device.Humidity = zone.Humidity
			break
		}
	}
	writeJSON(w, http.StatusOK, device)
}

func (h *Handler) serveSetTemperature(w http.ResponseWriter, r *http.Request, idPart string) {
	zoneID, ok := parseZoneID(w, idPart)
	if !ok {
		return
	}
	var request struct {
		Temperature float64 `json:"temperature"`
		System      string  `json:"system"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	info, err := h.session.ZoneInfo(zoneID)
	if err != nil {
		writeBackendError(w, err)
		return
	}
	system := tcc.SystemSwitch(0)
	if request.System != "" {
		system, err = parseSystem(request.System)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	} else {
		system = info.SystemSwitchPosition
	}
	hold := tcc.HoldPermanent
	changes := tcc.ControlChanges{
		StatusHeat: &hold,
		StatusCool: &hold,
	}
	switch system {
	case tcc.SystemSwitchHeat:
		changes.HeatSetpoint = &request.Temperature
		if coolSetpoint, ok := adjustedCoolSetpoint(info, request.Temperature); ok {
			changes.CoolSetpoint = &coolSetpoint
		}
	case tcc.SystemSwitchCool:
		changes.CoolSetpoint = &request.Temperature
		if heatSetpoint, ok := adjustedHeatSetpoint(info, request.Temperature); ok {
			changes.HeatSetpoint = &heatSetpoint
		}
	default:
		writeError(w, http.StatusConflict, "temperature cannot be changed while the system is off")
		return
	}
	h.submitAndWriteDeviceLocked(w, zoneID, changes)
}

func adjustedHeatSetpoint(info *tcc.ZoneInfo, coolSetpoint float64) (float64, bool) {
	deadband := setpointDeadband(info)
	if info.HeatSetpoint+deadband <= coolSetpoint {
		return 0, false
	}
	return coolSetpoint - deadband, true
}

func adjustedCoolSetpoint(info *tcc.ZoneInfo, heatSetpoint float64) (float64, bool) {
	deadband := setpointDeadband(info)
	if heatSetpoint+deadband <= info.CoolSetpoint {
		return 0, false
	}
	return heatSetpoint + deadband, true
}

func setpointDeadband(info *tcc.ZoneInfo) float64 {
	if info.Deadband > 0 {
		return info.Deadband
	}
	return 1
}

func (h *Handler) serveSetSystem(w http.ResponseWriter, r *http.Request, idPart string) {
	zoneID, ok := parseZoneID(w, idPart)
	if !ok {
		return
	}
	var request struct {
		System string `json:"system"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	system, err := parseSystem(request.System)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	hold := tcc.HoldPermanent
	h.mu.Lock()
	defer h.mu.Unlock()
	h.submitAndWriteDeviceLocked(w, zoneID, tcc.ControlChanges{
		SystemSwitch: &system,
		StatusHeat:   &hold,
		StatusCool:   &hold,
	})
}

func (h *Handler) serveSetFan(w http.ResponseWriter, r *http.Request, idPart string) {
	zoneID, ok := parseZoneID(w, idPart)
	if !ok {
		return
	}
	var request struct {
		Fan string `json:"fan"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	fan, err := parseFan(request.Fan)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	hold := tcc.HoldPermanent
	h.mu.Lock()
	defer h.mu.Unlock()
	h.submitAndWriteDeviceLocked(w, zoneID, tcc.ControlChanges{
		FanMode:    &fan,
		StatusHeat: &hold,
		StatusCool: &hold,
	})
}

func (h *Handler) submitAndWriteDeviceLocked(w http.ResponseWriter, zoneID tcc.ZoneID, changes tcc.ControlChanges) {
	if err := h.session.SubmitControlChanges(zoneID, changes); err != nil {
		writeBackendError(w, err)
		return
	}
	device, err := h.deviceResponseLocked(zoneID)
	if err != nil {
		writeBackendError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, device)
}

func (h *Handler) deviceResponseLocked(zoneID tcc.ZoneID) (deviceResponse, error) {
	info, err := h.session.ZoneInfo(zoneID)
	if err != nil {
		return deviceResponse{}, err
	}
	return newDeviceResponse(info), nil
}

type devicesResponse struct {
	Devices []deviceResponse `json:"devices"`
}

type deviceResponse struct {
	ID                    tcc.ZoneID `json:"id"`
	Name                  string     `json:"name,omitempty"`
	Temperature           *float64   `json:"temperature,omitempty"`
	Humidity              *float64   `json:"humidity,omitempty"`
	DisplayTemperature    *float64   `json:"displayTemperature,omitempty"`
	DisplayedUnits        string     `json:"displayedUnits"`
	System                string     `json:"system"`
	Fan                   string     `json:"fan"`
	HeatSetpoint          float64    `json:"heatSetpoint"`
	CoolSetpoint          float64    `json:"coolSetpoint"`
	ActiveSetpoint        *float64   `json:"activeSetpoint,omitempty"`
	EquipmentRunning      bool       `json:"equipmentRunning"`
	FanRunning            bool       `json:"fanRunning"`
	EquipmentOutputStatus int        `json:"equipmentOutputStatus"`
	RuntimeAvailable      bool       `json:"runtimeAvailable"`
	Offline               bool       `json:"offline"`
	SystemOptions         []string   `json:"systemOptions"`
	FanOptions            []string   `json:"fanOptions"`
	HeatRange             tempRange  `json:"heatRange"`
	CoolRange             tempRange  `json:"coolRange"`
	SetpointAllowed       bool       `json:"setpointAllowed"`
}

type tempRange struct {
	Min float64 `json:"min"`
	Max float64 `json:"max"`
}

func newDeviceResponse(info *tcc.ZoneInfo) deviceResponse {
	displayTemperature := (*float64)(nil)
	if info.DisplayTemperatureAvailable {
		displayTemperature = &info.DisplayTemperature
	}
	response := deviceResponse{
		ID:                    info.DeviceID,
		DisplayTemperature:    displayTemperature,
		DisplayedUnits:        info.DisplayedUnits,
		System:                systemString(info.SystemSwitchPosition),
		Fan:                   fanString(info.FanMode),
		HeatSetpoint:          info.HeatSetpoint,
		CoolSetpoint:          info.CoolSetpoint,
		EquipmentRunning:      info.EquipmentOutputStatus != 0,
		FanRunning:            info.IsFanRunning,
		EquipmentOutputStatus: info.EquipmentOutputStatus,
		RuntimeAvailable:      info.RuntimeStatusAvailable,
		Offline:               info.IsLost || info.GatewayIsLost || info.CommunicationLost,
		HeatRange: tempRange{
			Min: info.HeatLowerSetpointLimit,
			Max: info.HeatUpperSetpointLimit,
		},
		CoolRange: tempRange{
			Min: info.CoolLowerSetpointLimit,
			Max: info.CoolUpperSetpointLimit,
		},
		SetpointAllowed: info.SetpointChangeAllowed,
	}
	if info.SwitchHeatAllowed {
		response.SystemOptions = append(response.SystemOptions, "heat")
	}
	if info.SwitchCoolAllowed {
		response.SystemOptions = append(response.SystemOptions, "cool")
	}
	if info.SwitchOffAllowed {
		response.SystemOptions = append(response.SystemOptions, "off")
	}
	if info.FanModeAutoAllowed {
		response.FanOptions = append(response.FanOptions, "auto")
	}
	if info.FanModeOnAllowed {
		response.FanOptions = append(response.FanOptions, "on")
	}
	if info.FanModeCirculateAllowed {
		response.FanOptions = append(response.FanOptions, "circulate")
	}
	switch info.SystemSwitchPosition {
	case tcc.SystemSwitchHeat:
		response.ActiveSetpoint = &response.HeatSetpoint
	case tcc.SystemSwitchCool:
		response.ActiveSetpoint = &response.CoolSetpoint
	}
	return response
}

func parseZoneID(w http.ResponseWriter, value string) (tcc.ZoneID, bool) {
	id, err := strconv.Atoi(value)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid device id")
		return 0, false
	}
	return tcc.ZoneID(id), true
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return false
	}
	return true
}

func parseSystem(value string) (tcc.SystemSwitch, error) {
	switch strings.ToLower(value) {
	case "heat":
		return tcc.SystemSwitchHeat, nil
	case "cool":
		return tcc.SystemSwitchCool, nil
	case "off":
		return tcc.SystemSwitchOff, nil
	default:
		return 0, fmt.Errorf("unknown system %q", value)
	}
}

func parseFan(value string) (tcc.FanMode, error) {
	switch strings.ToLower(value) {
	case "auto":
		return tcc.FanModeAuto, nil
	case "on":
		return tcc.FanModeOn, nil
	case "circulate":
		return tcc.FanModeCirculate, nil
	default:
		return 0, fmt.Errorf("unknown fan mode %q", value)
	}
}

func systemString(value tcc.SystemSwitch) string {
	switch value {
	case tcc.SystemSwitchHeat:
		return "heat"
	case tcc.SystemSwitchCool:
		return "cool"
	case tcc.SystemSwitchOff:
		return "off"
	default:
		return "unknown"
	}
}

func fanString(value tcc.FanMode) string {
	switch value {
	case tcc.FanModeAuto:
		return "auto"
	case tcc.FanModeOn:
		return "on"
	case tcc.FanModeCirculate:
		return "circulate"
	default:
		return "unknown"
	}
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
	if errors.Is(err, tcc.ErrUnauthorized) {
		status = http.StatusUnauthorized
	}
	writeError(w, status, err.Error())
}
