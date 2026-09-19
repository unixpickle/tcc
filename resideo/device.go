package resideo

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/unixpickle/tcc/thermostat"
)

// API IDs may be JSON strings or numbers, depending on the endpoint.
type identifier string

func (i *identifier) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	if data[0] == '"' {
		return json.Unmarshal(data, (*string)(i))
	}
	var n json.Number
	if err := json.Unmarshal(data, &n); err != nil {
		return err
	}
	*i = identifier(n.String())
	return nil
}

type device struct {
	DeviceID   string           `json:"deviceID"`
	InternalID identifier       `json:"deviceInternalID"`
	Name       string           `json:"name"`
	UserName   string           `json:"userDefinedDeviceName"`
	Class      string           `json:"deviceClass"`
	Type       string           `json:"deviceType"`
	TitanType  string           `json:"titanDeviceType"`
	Alive      *bool            `json:"isAlive"`
	Upgrading  bool             `json:"isUpgrading"`
	Thermostat *thermostatState `json:"thermostat"`
	Fan        *fanState        `json:"fan"`
	Settings   struct {
		Fan *fanState `json:"fan"`
	} `json:"settings"`
	location string
}

type thermostatState struct {
	Units             string   `json:"units"`
	IndoorTemperature *float64 `json:"indoorTemperature"`
	IndoorHumidity    *float64 `json:"indoorHumidity"`
	AllowedModes      []string `json:"allowedModes"`
	Deadband          float64  `json:"deadband"`
	MinHeat           *float64 `json:"minHeatSetpoint"`
	MaxHeat           *float64 `json:"maxHeatSetpoint"`
	MinCool           *float64 `json:"minCoolSetpoint"`
	MaxCool           *float64 `json:"maxCoolSetpoint"`
	Changes           struct {
		Mode      string   `json:"mode"`
		Heat      *float64 `json:"heatSetpoint"`
		Cool      *float64 `json:"coolSetpoint"`
		Auto      bool     `json:"autoChangeoverActive"`
		Emergency bool     `json:"emergencyHeatActive"`
	} `json:"changeableValues"`
	Operation *operationStatus `json:"operationStatus"`
}

type operationStatus struct {
	Mode        string `json:"mode"`
	Fan         bool   `json:"fanRequest"`
	Circulation bool   `json:"circulationFanRequest"`
}

type fanState struct {
	AllowedModes []string `json:"allowedModes"`
	Running      bool     `json:"fanRunning"`
	Changes      struct {
		Mode string `json:"mode"`
	} `json:"changeableValues"`
}

func (d device) titan() bool {
	return strings.EqualFold(d.Type, "Focus Pro") || strings.EqualFold(d.Type, "HOUDINI") || strings.EqualFold(d.TitanType, "homes.d.focuspro")
}

func (d device) convertedCelsius() bool {
	if d.Thermostat.Units != "Celsius" {
		return false
	}
	switch strings.ToLower(d.Type) {
	case "jasper", "flycatcher", "storm", "houdini", "focus pro", "blackbeard":
		return true
	}
	return d.titan()
}

func (d device) display(value float64) float64 {
	if !d.convertedCelsius() {
		return value
	}
	c := (value - 32) * 5 / 9
	if strings.EqualFold(d.Type, "Blackbeard") {
		return math.Round(c)
	}
	return math.Round(c*2) / 2
}

func (d device) wire(value float64) float64 {
	if !d.convertedCelsius() {
		return value
	}
	f := value*1.8 + 32
	if strings.EqualFold(d.Type, "Blackbeard") {
		return math.Round(f)
	}
	return f
}

func (d device) displayPointer(value *float64) *float64 {
	if value == nil {
		return nil
	}
	result := d.display(*value)
	return &result
}

func (d device) normalized() (thermostat.Device, error) {
	t := d.Thermostat
	if t == nil {
		return thermostat.Device{}, fmt.Errorf("Resideo thermostat %q has no state", d.DeviceID)
	}
	units := "F"
	switch t.Units {
	case "Fahrenheit":
	case "Celsius":
		units = "C"
	default:
		return thermostat.Device{}, fmt.Errorf("Resideo thermostat has unknown units %q", t.Units)
	}
	result := thermostat.Device{
		ID: d.location + ":" + d.DeviceID, Name: d.UserName, DisplayedUnits: units,
		DisplayTemperature: d.displayPointer(t.IndoorTemperature), Humidity: t.IndoorHumidity,
		System: normalizeMode(t.Changes.Mode), Fan: "unknown", TemperatureStep: 1,
		Offline:       d.Alive == nil || !*d.Alive || d.Upgrading,
		SystemOptions: []string{}, FanOptions: []string{},
	}
	if result.Name == "" {
		result.Name = d.Name
	}
	if units == "C" && !strings.EqualFold(d.Type, "Blackbeard") {
		result.TemperatureStep = 0.5
	}
	if t.Changes.Auto {
		result.System = "auto"
	}
	if t.Changes.Emergency {
		result.System = "emergencyheat"
	}
	for _, mode := range t.AllowedModes {
		mode = normalizeMode(mode)
		if _, ok := systemNames[mode]; ok && !contains(result.SystemOptions, mode) {
			result.SystemOptions = append(result.SystemOptions, mode)
		}
	}
	if t.Changes.Heat != nil {
		result.HeatSetpoint = d.display(*t.Changes.Heat)
	}
	if t.Changes.Cool != nil {
		result.CoolSetpoint = d.display(*t.Changes.Cool)
	}
	if t.MinHeat != nil && t.MaxHeat != nil {
		result.HeatRange = thermostat.Range{Min: d.display(*t.MinHeat), Max: d.display(*t.MaxHeat)}
	}
	if t.MinCool != nil && t.MaxCool != nil {
		result.CoolRange = thermostat.Range{Min: d.display(*t.MinCool), Max: d.display(*t.MaxCool)}
	}
	heatOK := !contains(result.SystemOptions, "heat") && !contains(result.SystemOptions, "emergencyheat") || t.MinHeat != nil && t.MaxHeat != nil && t.Changes.Heat != nil
	coolOK := !contains(result.SystemOptions, "cool") || t.MinCool != nil && t.MaxCool != nil && t.Changes.Cool != nil
	result.SetpointAllowed = heatOK && coolOK && !result.Offline
	switch result.System {
	case "heat", "emergencyheat":
		result.ActiveSetpoint = d.displayPointer(t.Changes.Heat)
	case "cool":
		result.ActiveSetpoint = d.displayPointer(t.Changes.Cool)
	}
	if t.Operation != nil {
		result.RuntimeAvailable = true
		switch strings.ToLower(t.Operation.Mode) {
		case "heat", "cool", "emergencyheat":
			result.EquipmentRunning = true
		}
		result.FanRunning = t.Operation.Fan || t.Operation.Circulation
	}
	if d.Settings.Fan != nil && !d.titan() {
		d.Fan = d.Settings.Fan
	}
	if d.Fan != nil {
		result.Fan = strings.ToLower(d.Fan.Changes.Mode)
		result.FanRunning = result.FanRunning || d.Fan.Running
		for _, mode := range d.Fan.AllowedModes {
			mode = strings.ToLower(mode)
			if _, ok := fanNames[mode]; ok {
				result.FanOptions = append(result.FanOptions, mode)
			}
		}
	}
	return result, nil
}

func normalizeMode(value string) string {
	switch strings.ToLower(value) {
	case "autoheat", "autocool":
		return "auto"
	default:
		return strings.ToLower(value)
	}
}

func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

// Titan status/configuration fields deliberately retain their response casing;
// command parameter IDs use different casing (see backend.go).
type titanDevice struct {
	Status struct {
		Heat        *float64         `json:"homes.p.heatSetpoint"`
		Cool        *float64         `json:"homes.p.coolSetpoint"`
		Mode        string           `json:"homes.p.thermostatMode"`
		Temperature *float64         `json:"homes.p.indoorTemperature"`
		Humidity    *float64         `json:"homes.p.indoorHumidity"`
		Alive       *bool            `json:"homes.p.isAlive"`
		Upgrading   bool             `json:"homes.p.isUpgrading"`
		Operation   *operationStatus `json:"homes.p.operationStatus"`
		Fan         *struct {
			Position string `json:"position"`
		} `json:"homes.p.fanSwitch"`
	} `json:"status"`
	Config struct {
		Units    string   `json:"homes.c.temperatureUnits"`
		Modes    []string `json:"homes.c.systemSwitch"`
		FanModes []string `json:"homes.c.fanswitch.mode"`
		MinHeat  *float64 `json:"homes.c.minimumHeatSetpointAllowed"`
		MaxHeat  *float64 `json:"homes.c.maximumHeatSetpointAllowed"`
		MinCool  *float64 `json:"homes.c.minimumCoolSetpointAllowed"`
		MaxCool  *float64 `json:"homes.c.maximumCoolSetpointAllowed"`
		Deadband float64  `json:"homes.c.setpointDeadband"`
	} `json:"configurations"`
}

func (t titanDevice) apply(d *device) {
	d.Alive, d.Upgrading = t.Status.Alive, t.Status.Upgrading
	state := &thermostatState{
		Units: t.Config.Units, AllowedModes: t.Config.Modes, Deadband: t.Config.Deadband,
		IndoorTemperature: t.Status.Temperature, IndoorHumidity: t.Status.Humidity,
		MinHeat: t.Config.MinHeat, MaxHeat: t.Config.MaxHeat, MinCool: t.Config.MinCool, MaxCool: t.Config.MaxCool,
		Operation: t.Status.Operation,
	}
	state.Changes.Heat, state.Changes.Cool, state.Changes.Mode = t.Status.Heat, t.Status.Cool, t.Status.Mode
	d.Thermostat = state
	if t.Status.Fan != nil {
		d.Fan = &fanState{AllowedModes: t.Config.FanModes}
		d.Fan.Changes.Mode = t.Status.Fan.Position
	}
}
