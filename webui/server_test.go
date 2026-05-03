package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/unixpickle/tcc"
)

func TestDevicesEndpointReturnsZonesAndInfo(t *testing.T) {
	temp := 68.0
	humidity := 35.0
	session := &fakeSession{
		zones: []tcc.Zone{{
			ID:          1001,
			Name:        "Downstairs",
			Temperature: &temp,
			Humidity:    &humidity,
		}},
		infos: map[tcc.ZoneID]*tcc.ZoneInfo{
			1001: {
				DeviceID:                    1001,
				DisplayedUnits:              "F",
				DisplayTemperature:          68,
				DisplayTemperatureAvailable: true,
				SystemSwitchPosition:        tcc.SystemSwitchCool,
				FanMode:                     tcc.FanModeAuto,
				CoolSetpoint:                72,
				HeatSetpoint:                66,
				EquipmentOutputStatus:       2,
				RuntimeStatusAvailable:      true,
				SetpointChangeAllowed:       true,
				SwitchCoolAllowed:           true,
				SwitchOffAllowed:            true,
				FanModeAutoAllowed:          true,
				FanModeOnAllowed:            true,
				CoolLowerSetpointLimit:      50,
				CoolUpperSetpointLimit:      99,
				HeatLowerSetpointLimit:      40,
				HeatUpperSetpointLimit:      90,
			},
		},
	}
	recorder := httptest.NewRecorder()
	newTestHandler(session).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/devices", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}
	var response devicesResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if session.zoneCalls != 1 {
		t.Fatalf("expected zones to be fetched once; got %d", session.zoneCalls)
	}
	if len(response.Devices) != 1 {
		t.Fatalf("expected one device; got %d", len(response.Devices))
	}
	device := response.Devices[0]
	if device.Name != "Downstairs" || device.System != "cool" || device.Fan != "auto" {
		t.Fatalf("unexpected device metadata: %+v", device)
	}
	if device.ActiveSetpoint == nil || *device.ActiveSetpoint != 72 {
		t.Fatalf("expected active cool setpoint; got %v", device.ActiveSetpoint)
	}
	if !device.EquipmentRunning || device.EquipmentOutputStatus != 2 {
		t.Fatalf("expected equipment to be running: %+v", device)
	}
}

func TestSetTemperatureCreatesPermanentHoldForActiveMode(t *testing.T) {
	session := &fakeSession{
		infos: map[tcc.ZoneID]*tcc.ZoneInfo{
			1001: {
				DeviceID:             1001,
				SystemSwitchPosition: tcc.SystemSwitchHeat,
				FanMode:              tcc.FanModeAuto,
				HeatSetpoint:         67,
				CoolSetpoint:         73,
			},
		},
	}
	body := bytes.NewBufferString(`{"temperature":69}`)
	recorder := httptest.NewRecorder()
	newTestHandler(session).ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/devices/1001/temperature", body))
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", recorder.Code, recorder.Body)
	}
	change := session.lastChange
	if change.HeatSetpoint == nil || *change.HeatSetpoint != 69 {
		t.Fatalf("expected heat setpoint change; got %+v", change)
	}
	if change.CoolSetpoint != nil {
		t.Fatalf("did not expect cool setpoint change; got %+v", change)
	}
	if change.StatusHeat == nil || *change.StatusHeat != tcc.HoldPermanent {
		t.Fatalf("expected permanent heat hold; got %+v", change)
	}
	if change.StatusCool == nil || *change.StatusCool != tcc.HoldPermanent {
		t.Fatalf("expected permanent cool hold; got %+v", change)
	}
}

func TestSetTemperatureUsesRequestedSystem(t *testing.T) {
	session := &fakeSession{
		infos: map[tcc.ZoneID]*tcc.ZoneInfo{
			1001: {
				DeviceID:             1001,
				SystemSwitchPosition: tcc.SystemSwitchCool,
				FanMode:              tcc.FanModeAuto,
				HeatSetpoint:         66,
				CoolSetpoint:         63,
				Deadband:             1,
			},
		},
	}
	body := bytes.NewBufferString(`{"temperature":66,"system":"cool"}`)
	recorder := httptest.NewRecorder()
	newTestHandler(session).ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/devices/1001/temperature", body))
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", recorder.Code, recorder.Body)
	}
	change := session.lastChange
	if change.CoolSetpoint == nil || *change.CoolSetpoint != 66 {
		t.Fatalf("expected cool setpoint change; got %+v", change)
	}
	if change.HeatSetpoint == nil || *change.HeatSetpoint != 65 {
		t.Fatalf("expected paired heat setpoint change; got %+v", change)
	}
	if session.infoCalls != 2 {
		t.Fatalf("expected pre-submit deadband check and post-submit reload; got %d info calls", session.infoCalls)
	}
}

func TestSetSystemReloadsDeviceStatus(t *testing.T) {
	session := &fakeSession{
		infos: map[tcc.ZoneID]*tcc.ZoneInfo{
			1001: {
				DeviceID:             1001,
				SystemSwitchPosition: tcc.SystemSwitchHeat,
				FanMode:              tcc.FanModeAuto,
				HeatSetpoint:         67,
				CoolSetpoint:         73,
			},
		},
	}
	body := bytes.NewBufferString(`{"system":"cool"}`)
	recorder := httptest.NewRecorder()
	newTestHandler(session).ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/devices/1001/system", body))
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", recorder.Code, recorder.Body)
	}
	if session.infoCalls != 1 {
		t.Fatalf("expected status reload after submit; got %d info calls", session.infoCalls)
	}
	if session.lastChange.SystemSwitch == nil || *session.lastChange.SystemSwitch != tcc.SystemSwitchCool {
		t.Fatalf("expected cool system change; got %+v", session.lastChange)
	}
	var response deviceResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.System != "cool" || response.ActiveSetpoint == nil || *response.ActiveSetpoint != 73 {
		t.Fatalf("expected reloaded cool state; got %+v", response)
	}
}

func TestDevicesEndpointReloginsAndRetriesUnauthorizedZones(t *testing.T) {
	temp := 68.0
	session := &fakeSession{
		zones: []tcc.Zone{{
			ID:          1001,
			Name:        "Downstairs",
			Temperature: &temp,
		}},
		zoneErrors: []error{fmt.Errorf("fetch zones: %w", tcc.ErrUnauthorized)},
		infos: map[tcc.ZoneID]*tcc.ZoneInfo{
			1001: {
				DeviceID:             1001,
				SystemSwitchPosition: tcc.SystemSwitchHeat,
				FanMode:              tcc.FanModeAuto,
			},
		},
	}
	recorder := httptest.NewRecorder()
	newTestHandler(session).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/devices", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", recorder.Code, recorder.Body)
	}
	if session.reloginCalls != 1 {
		t.Fatalf("expected one relogin; got %d", session.reloginCalls)
	}
	if session.zoneCalls != 2 {
		t.Fatalf("expected zones to be retried; got %d calls", session.zoneCalls)
	}
}

func TestRecentReloginAttemptReturnsOriginalUnauthorizedError(t *testing.T) {
	session := &fakeSession{
		zoneErrors: []error{fmt.Errorf("fetch zones: %w", tcc.ErrUnauthorized)},
	}
	handler := newTestHandler(session)
	handler.lastLoginAttempt = time.Now()
	handler.lastLoginErr = errors.New("recent relogin failed")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/devices", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("unexpected status: %d body=%s", recorder.Code, recorder.Body)
	}
	if session.reloginCalls != 0 {
		t.Fatalf("expected relogin to be throttled; got %d calls", session.reloginCalls)
	}
	if !strings.Contains(recorder.Body.String(), tcc.ErrUnauthorized.Error()) {
		t.Fatalf("expected original unauthorized error in response; got %s", recorder.Body)
	}
}

func TestRecentSuccessfulReloginRetriesUnauthorizedError(t *testing.T) {
	session := &fakeSession{
		zones: []tcc.Zone{{
			ID:   1001,
			Name: "Downstairs",
		}},
		zoneErrors: []error{fmt.Errorf("fetch zones: %w", tcc.ErrUnauthorized)},
		infos: map[tcc.ZoneID]*tcc.ZoneInfo{
			1001: {
				DeviceID:             1001,
				SystemSwitchPosition: tcc.SystemSwitchHeat,
				FanMode:              tcc.FanModeAuto,
			},
		},
	}
	handler := newTestHandler(session)
	handler.lastLoginAttempt = time.Now()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/devices", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", recorder.Code, recorder.Body)
	}
	if session.reloginCalls != 0 {
		t.Fatalf("expected recent successful relogin to be reused; got %d calls", session.reloginCalls)
	}
	if session.zoneCalls != 2 {
		t.Fatalf("expected zones to be retried; got %d calls", session.zoneCalls)
	}
}

func TestFailedReloginReturnsReloginError(t *testing.T) {
	session := &fakeSession{
		zoneErrors: []error{fmt.Errorf("fetch zones: %w", tcc.ErrUnauthorized)},
		reloginErr: errors.New("bad credentials"),
	}
	recorder := httptest.NewRecorder()
	newTestHandler(session).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/devices", nil))
	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("unexpected status: %d body=%s", recorder.Code, recorder.Body)
	}
	if session.reloginCalls != 1 {
		t.Fatalf("expected one relogin; got %d", session.reloginCalls)
	}
	if !strings.Contains(recorder.Body.String(), "relogin: bad credentials") {
		t.Fatalf("expected relogin error in response; got %s", recorder.Body)
	}
}

func TestMountRootHidesUnprefixedRoutes(t *testing.T) {
	handler := mountRoot(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/devices" {
			t.Fatalf("unexpected stripped path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}), "secret")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/devices", nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected unprefixed path to be hidden; got %d", recorder.Code)
	}
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/secret/api/devices", nil))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("expected prefixed path to route; got %d", recorder.Code)
	}
}

func TestCleanRootRequiresSingleDirectoryName(t *testing.T) {
	if root, err := cleanRoot("/secret/"); err != nil || root != "secret" {
		t.Fatalf("unexpected clean root result: root=%q err=%v", root, err)
	}
	if _, err := cleanRoot("a/b"); err == nil {
		t.Fatal("expected nested root to fail")
	}
	if _, err := cleanRoot(".."); err == nil {
		t.Fatal("expected parent directory root to fail")
	}
}

type fakeSession struct {
	zoneCalls    int
	infoCalls    int
	reloginCalls int
	zones        []tcc.Zone
	infos        map[tcc.ZoneID]*tcc.ZoneInfo
	zoneErrors   []error
	infoErrors   []error
	submitErrors []error
	reloginErr   error
	lastID       tcc.ZoneID
	lastChange   tcc.ControlChanges
}

func (f *fakeSession) Zones() ([]tcc.Zone, error) {
	f.zoneCalls++
	if len(f.zoneErrors) != 0 {
		err := f.zoneErrors[0]
		f.zoneErrors = f.zoneErrors[1:]
		return nil, err
	}
	return f.zones, nil
}

func (f *fakeSession) ZoneInfo(id tcc.ZoneID) (*tcc.ZoneInfo, error) {
	f.infoCalls++
	if len(f.infoErrors) != 0 {
		err := f.infoErrors[0]
		f.infoErrors = f.infoErrors[1:]
		return nil, err
	}
	info := *f.infos[id]
	return &info, nil
}

func (f *fakeSession) SubmitControlChanges(id tcc.ZoneID, changes tcc.ControlChanges) error {
	if len(f.submitErrors) != 0 {
		err := f.submitErrors[0]
		f.submitErrors = f.submitErrors[1:]
		return err
	}
	f.lastID = id
	f.lastChange = changes
	if changes.SystemSwitch != nil {
		info := f.infos[id]
		info.SystemSwitchPosition = *changes.SystemSwitch
	}
	if changes.HeatSetpoint != nil {
		f.infos[id].HeatSetpoint = *changes.HeatSetpoint
	}
	if changes.CoolSetpoint != nil {
		f.infos[id].CoolSetpoint = *changes.CoolSetpoint
	}
	if changes.FanMode != nil {
		f.infos[id].FanMode = *changes.FanMode
	}
	return nil
}

func (f *fakeSession) Relogin(username, password string) error {
	f.reloginCalls++
	return f.reloginErr
}

func newTestHandler(session *fakeSession) *Handler {
	return newHandler(session, "user", "pass").(*Handler)
}
