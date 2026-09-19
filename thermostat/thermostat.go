// Package thermostat defines the provider-independent thermostat API.
package thermostat

import (
	"errors"
	"fmt"
	"math"
	"slices"
)

var (
	ErrNotFound     = errors.New("thermostat not found")
	ErrUnauthorized = errors.New("thermostat session unauthorized")
	ErrInvalid      = errors.New("invalid thermostat control")
	ErrConflict     = errors.New("thermostat control unavailable")
)

// Backend owns discovery, authentication, and control for one account. IDs are
// opaque strings. Implementations must be safe for concurrent use. Temperatures
// use each device's displayed units. Controls submit permanent holds where the
// provider supports them; callers should read Device again after a write.
type Backend interface {
	Devices() ([]Device, error)
	Device(id string) (Device, error)
	SetTemperature(id string, temperature float64, system string) error
	SetSystem(id, system string) error
	SetFan(id, fan string) error
}

type Device struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name,omitempty"`
	Temperature        *float64 `json:"temperature,omitempty"`
	Humidity           *float64 `json:"humidity,omitempty"`
	DisplayTemperature *float64 `json:"displayTemperature,omitempty"`
	DisplayedUnits     string   `json:"displayedUnits"`
	System             string   `json:"system"`
	Fan                string   `json:"fan"`
	HeatSetpoint       float64  `json:"heatSetpoint"`
	CoolSetpoint       float64  `json:"coolSetpoint"`
	ActiveSetpoint     *float64 `json:"activeSetpoint,omitempty"`
	EquipmentRunning   bool     `json:"equipmentRunning"`
	FanRunning         bool     `json:"fanRunning"`
	RuntimeAvailable   bool     `json:"runtimeAvailable"`
	Offline            bool     `json:"offline"`
	SystemOptions      []string `json:"systemOptions"`
	FanOptions         []string `json:"fanOptions"`
	HeatRange          Range    `json:"heatRange"`
	CoolRange          Range    `json:"coolRange"`
	SetpointAllowed    bool     `json:"setpointAllowed"`
	TemperatureStep    float64  `json:"temperatureStep"`
}

type Range struct {
	Min float64 `json:"min"`
	Max float64 `json:"max"`
}

func (r Range) Contains(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= r.Min && value <= r.Max
}

// CheckOption rejects unsupported controls before contacting a device.
func CheckOption(value string, options []string) error {
	if !slices.Contains(options, value) {
		return fmt.Errorf("%w: unsupported option %q", ErrInvalid, value)
	}
	return nil
}

// Setpoints calculates a valid heat/cool pair, adjusting the other setpoint if
// needed. An empty system uses the current mode; ambiguous auto/off modes need
// an explicit heat or cool selection.
func Setpoints(d Device, value float64, system string, deadband float64) (heat, cool float64, err error) {
	if !d.SetpointAllowed || d.Offline {
		return 0, 0, ErrConflict
	}
	if system == "" {
		system = d.System
	}
	heat, cool = d.HeatSetpoint, d.CoolSetpoint
	switch system {
	case "heat", "emergencyheat":
		if !d.HeatRange.Contains(value) {
			return 0, 0, fmt.Errorf("%w: heat setpoint outside device limits", ErrInvalid)
		}
		heat = value
		if slices.Contains(d.SystemOptions, "cool") && heat+deadband > cool {
			cool = heat + deadband
			if !d.CoolRange.Contains(cool) {
				return 0, 0, fmt.Errorf("%w: cannot preserve deadband within cooling limits", ErrInvalid)
			}
		}
	case "cool":
		if !d.CoolRange.Contains(value) {
			return 0, 0, fmt.Errorf("%w: cool setpoint outside device limits", ErrInvalid)
		}
		cool = value
		if slices.Contains(d.SystemOptions, "heat") && heat+deadband > cool {
			heat = cool - deadband
			if !d.HeatRange.Contains(heat) {
				return 0, 0, fmt.Errorf("%w: cannot preserve deadband within heating limits", ErrInvalid)
			}
		}
	default:
		return 0, 0, fmt.Errorf("%w: select heat or cool before changing temperature", ErrConflict)
	}
	if err := CheckOption(system, d.SystemOptions); err != nil {
		return 0, 0, err
	}
	return heat, cool, nil
}
