// Package resideo implements the Resideo app's B2C, CHIL, and Titan APIs.
package resideo

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/unixpickle/tcc/thermostat"
)

var systemNames = map[string]string{"heat": "Heat", "cool": "Cool", "off": "Off", "auto": "Auto", "emergencyheat": "EmergencyHeat"}
var fanNames = map[string]string{"auto": "Auto", "on": "On", "circulate": "Circulate"}

// Backend controls an account. Passwords and OAuth tokens are kept in memory.
type Backend struct {
	client    *apiClient
	controlMu sync.Mutex
}

var _ thermostat.Backend = (*Backend)(nil)

func NewBackend(username, password string) (*Backend, error) {
	b := &Backend{client: newAPIClient(username, password)}
	if err := b.client.api(http.MethodGet, chilBase, "/api/v2/session", nil, nil); err != nil {
		return nil, err
	}
	return b, nil
}

func (b *Backend) discover() ([]device, error) {
	var locations []struct {
		ID identifier `json:"id"`
	}
	if err := b.client.api(http.MethodGet, titanBase, "/locations", nil, &locations); err != nil {
		return nil, err
	}
	result := []device{}
	for _, location := range locations {
		if location.ID == "" {
			return nil, fmt.Errorf("Resideo location has no ID")
		}
		var devices []device
		path := "/api/v6/locations/" + url.PathEscape(string(location.ID)) + "/devices"
		if err := b.client.api(http.MethodGet, chilBase, path, nil, &devices); err != nil {
			return nil, err
		}
		for _, d := range devices {
			if d.Class != "Thermostat" {
				continue
			}
			if d.DeviceID == "" || strings.ContainsAny(d.DeviceID, "/:") {
				return nil, fmt.Errorf("invalid Resideo device ID")
			}
			d.location = string(location.ID)
			result = append(result, d)
		}
	}
	return result, nil
}

func (b *Backend) Devices() ([]thermostat.Device, error) {
	devices, err := b.discover()
	if err != nil {
		return nil, err
	}
	result := make([]thermostat.Device, 0, len(devices))
	for _, d := range devices {
		if d.titan() {
			d, err = b.refresh(d)
			if err != nil {
				return nil, err
			}
		}
		normalized, err := d.normalized()
		if err != nil {
			return nil, err
		}
		result = append(result, normalized)
	}
	return result, nil
}

func (b *Backend) read(id string) (device, error) {
	devices, err := b.discover()
	if err != nil {
		return device{}, err
	}
	for _, d := range devices {
		if d.location+":"+d.DeviceID == id {
			return b.refresh(d)
		}
	}
	return device{}, thermostat.ErrNotFound
}

func (b *Backend) refresh(d device) (device, error) {
	if d.titan() {
		if d.InternalID == "" {
			return device{}, fmt.Errorf("Resideo Titan device has no internal ID")
		}
		var state titanDevice
		if err := b.client.api(http.MethodGet, titanBase, "/devices/"+url.PathEscape(string(d.InternalID)), nil, &state); err != nil {
			return device{}, err
		}
		state.apply(&d)
	} else {
		// The Android app and working probe both use this double slash.
		path := "//api/v6/locations/" + url.PathEscape(d.location) + "/devices/" + url.PathEscape(d.DeviceID)
		if err := b.client.api(http.MethodGet, chilBase, path, nil, &d); err != nil {
			return device{}, err
		}
	}
	return d, nil
}

func (b *Backend) Device(id string) (thermostat.Device, error) {
	d, err := b.read(id)
	if err != nil {
		return thermostat.Device{}, err
	}
	return d.normalized()
}

func (b *Backend) SetTemperature(id string, temperature float64, system string) error {
	b.controlMu.Lock()
	defer b.controlMu.Unlock()
	d, err := b.read(id)
	if err != nil {
		return err
	}
	current, err := d.normalized()
	if err != nil {
		return err
	}
	system = strings.ToLower(system)
	if system == "" {
		system = current.System
	}
	deadband := d.Thermostat.Deadband
	if d.convertedCelsius() {
		deadband *= 5.0 / 9
	}
	heat, cool, err := thermostat.Setpoints(current, temperature, system, deadband)
	if err != nil {
		return err
	}
	if current.System == "auto" {
		if d.titan() {
			return b.command(d, "changethermostatsetpoint", []parameter{
				{"homes.p.holdstatus", "PermanentHold"}, {"homes.p.thermostatmode", "Auto"},
				{"homes.p.heatsetpoint", numberString(d.wire(heat))}, {"homes.p.coolsetpoint", numberString(d.wire(cool))}, {"homes.p.unit", d.Thermostat.Units},
			})
		}
		return b.client.api(http.MethodPost, chilBase, d.path("")+"/thermostat/changeableValues", map[string]any{
			"autoChangeoverActive": true, "heatSetPoint": d.wire(heat), "coolSetPoint": d.wire(cool),
			"mode": systemNames[system], "thermostatSetpointStatus": "PermanentHold", "unit": d.Thermostat.Units,
		}, nil)
	}
	// Move the paired setpoint first so neither intermediate state crosses the
	// deadband. A failed command stops the sequence and is returned to the caller.
	if system == "cool" {
		if heat != current.HeatSetpoint {
			if err := b.setpoint(d, "heat", heat); err != nil {
				return err
			}
		}
		return b.setpoint(d, "cool", cool)
	}
	if cool != current.CoolSetpoint {
		if err := b.setpoint(d, "cool", cool); err != nil {
			return err
		}
	}
	return b.setpoint(d, "heat", heat)
}

func (b *Backend) setpoint(d device, system string, value float64) error {
	if d.titan() {
		return b.command(d, "changethermostat"+system+"setpoint", []parameter{
			{"homes.p.holdstatus", "PermanentHold"}, {"homes.p." + system + "setpoint", numberString(d.wire(value))}, {"homes.p.unit", d.Thermostat.Units},
		})
	}
	suffix := "HeatSetpoint"
	if system == "cool" {
		suffix = "Coolsetpoint"
	}
	return b.client.api(http.MethodPut, chilBase, d.path("v2")+"/thermostat/"+suffix, map[string]any{
		"thermostatSetpoint": d.wire(value), "thermostatSetpointStatus": "PermanentHold", "unit": d.Thermostat.Units,
	}, nil)
}

func (b *Backend) SetSystem(id, system string) error {
	b.controlMu.Lock()
	defer b.controlMu.Unlock()
	d, err := b.read(id)
	if err != nil {
		return err
	}
	current, err := d.normalized()
	if err != nil {
		return err
	}
	system = strings.ToLower(system)
	if err := thermostat.CheckOption(system, current.SystemOptions); err != nil {
		return err
	}
	if current.Offline {
		return thermostat.ErrConflict
	}
	if d.titan() {
		// Mode operations have no hold parameter; pause the schedule separately.
		if err := b.command(d, "changethermostatschedulestatus", []parameter{{"homes.p.scheduleenabled", false}}); err != nil {
			return err
		}
		return b.command(d, "changethermostatmode", []parameter{{"homes.p.thermostatmode", systemNames[system]}})
	}
	if err := b.client.api(http.MethodPut, chilBase, d.path("")+"/hold", map[string]string{"Status": "PermanentHold"}, nil); err != nil {
		return err
	}
	return b.client.api(http.MethodPut, chilBase, d.path("")+"/thermostat/Mode", map[string]string{"thermostatMode": systemNames[system]}, nil)
}

func (b *Backend) SetFan(id, fan string) error {
	b.controlMu.Lock()
	defer b.controlMu.Unlock()
	d, err := b.read(id)
	if err != nil {
		return err
	}
	current, err := d.normalized()
	if err != nil {
		return err
	}
	fan = strings.ToLower(fan)
	if err := thermostat.CheckOption(fan, current.FanOptions); err != nil {
		return err
	}
	if current.Offline {
		return thermostat.ErrConflict
	}
	if d.titan() {
		return b.command(d, "changethermostatfan", []parameter{{"homes.p.fanswitch.position", fanNames[fan]}, {"homes.p.holdstatus", "PermanentHold"}})
	}
	body := map[string]string{"mode": fanNames[fan]}
	// Jasper/round thermostats do not accept fan hold semantics. Match the app:
	// only FlyCatcher and Storm attach holds to CHIL fan commands.
	if strings.EqualFold(d.Type, "FlyCatcher") || strings.EqualFold(d.Type, "Storm") {
		body["HoldStatus"] = "PermanentHold"
	}
	return b.client.api(http.MethodPost, chilBase, d.path("v2")+"/fan/changeableValues", body, nil)
}

func (d device) path(version string) string {
	prefix := "/api/"
	if version != "" {
		prefix += version + "/"
	}
	return prefix + "locations/" + url.PathEscape(d.location) + "/devices/" + url.PathEscape(d.DeviceID)
}

type parameter struct {
	ID    string `json:"id"`
	Value any    `json:"value"`
}

func (b *Backend) command(d device, operation string, parameters []parameter) error {
	if d.InternalID == "" {
		return fmt.Errorf("Resideo Titan device has no internal ID")
	}
	return b.client.api(http.MethodPut, titanBase, "/devices/"+url.PathEscape(string(d.InternalID))+"/control", struct {
		Operation  string      `json:"operationId"`
		Parameters []parameter `json:"parameters"`
	}{"homes.o." + operation, parameters}, nil)
}

func numberString(value float64) string {
	result := strconv.FormatFloat(value, 'f', -1, 64)
	if !strings.Contains(result, ".") {
		result += ".0"
	}
	return result
}
