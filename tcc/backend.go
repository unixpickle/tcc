package tcc

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/unixpickle/tcc/thermostat"
)

const (
	MinLoginInterval = 10 * time.Minute
	ReloginInterval  = 12 * time.Hour
)

type session interface {
	Zones() ([]Zone, error)
	ZoneInfo(ZoneID) (*ZoneInfo, error)
	ZoneInfos([]ZoneID) (map[ZoneID]*ZoneInfo, error)
	SubmitControlChanges(ZoneID, ControlChanges) error
	Relogin(username, password string) error
}

// Backend adapts a TCC account to the common thermostat API.
type Backend struct {
	session            session
	username, password string
	controlMu          sync.Mutex
	reauthMu           sync.Mutex
	lastLoginAttempt   time.Time
	lastLoginErr       error
}

var _ thermostat.Backend = (*Backend)(nil)

func NewBackend(username, password string) (*Backend, error) {
	s, err := NewSession(username, password)
	if err != nil {
		return nil, err
	}
	return &Backend{session: s, username: username, password: password, lastLoginAttempt: time.Now()}, nil
}

func (h *Backend) Devices() ([]thermostat.Device, error) {
	if err := h.refreshIfDue(); err != nil {
		return nil, err
	}
	zones, err := h.zones()
	if err != nil {
		return nil, err
	}
	ids := make([]ZoneID, 0, len(zones))
	for _, z := range zones {
		ids = append(ids, z.ID)
	}
	infos, err := h.zoneInfos(ids)
	if err != nil {
		return nil, err
	}
	result := make([]thermostat.Device, 0, len(zones))
	for _, z := range zones {
		info := infos[z.ID]
		if info == nil {
			return nil, fmt.Errorf("missing zone info for %d", z.ID)
		}
		d := deviceFromInfo(info)
		d.Name, d.Temperature, d.Humidity = z.Name, z.Temperature, z.Humidity
		result = append(result, d)
	}
	return result, nil
}

func (h *Backend) Device(id string) (thermostat.Device, error) {
	if err := h.refreshIfDue(); err != nil {
		return thermostat.Device{}, err
	}
	zone, err := h.findZone(id)
	if err != nil {
		return thermostat.Device{}, err
	}
	info, err := h.zoneInfo(zone.ID)
	if err != nil {
		return thermostat.Device{}, err
	}
	d := deviceFromInfo(info)
	d.Name, d.Temperature, d.Humidity = zone.Name, zone.Temperature, zone.Humidity
	return d, nil
}

func (h *Backend) findZone(id string) (Zone, error) {
	n, err := strconv.Atoi(id)
	if err != nil || n <= 0 {
		return Zone{}, thermostat.ErrNotFound
	}
	zones, err := h.zones()
	if err != nil {
		return Zone{}, err
	}
	for _, z := range zones {
		if z.ID == ZoneID(n) {
			return z, nil
		}
	}
	return Zone{}, thermostat.ErrNotFound
}

func (h *Backend) SetTemperature(id string, temperature float64, system string) error {
	h.controlMu.Lock()
	defer h.controlMu.Unlock()
	if err := h.refreshIfDue(); err != nil {
		return err
	}
	zone, err := h.findZone(id)
	if err != nil {
		return err
	}
	info, err := h.zoneInfo(zone.ID)
	if err != nil {
		return err
	}
	system = strings.ToLower(system)
	if system == "" {
		system = systemString(info.SystemSwitchPosition)
	}
	deadband := info.Deadband
	if deadband <= 0 {
		deadband = 1
	}
	heat, cool, err := thermostat.Setpoints(deviceFromInfo(info), temperature, system, deadband)
	if err != nil {
		return err
	}
	hold := HoldPermanent
	changes := ControlChanges{StatusHeat: &hold, StatusCool: &hold}
	if system == "heat" || heat != info.HeatSetpoint {
		changes.HeatSetpoint = &heat
	}
	if system == "cool" || cool != info.CoolSetpoint {
		changes.CoolSetpoint = &cool
	}
	return h.submitControlChanges(zone.ID, changes)
}

func (h *Backend) SetSystem(id, system string) error {
	h.controlMu.Lock()
	defer h.controlMu.Unlock()
	value, err := parseSystem(system)
	if err != nil {
		return fmt.Errorf("%w: %v", thermostat.ErrInvalid, err)
	}
	d, err := h.Device(id)
	if err != nil {
		return err
	}
	if err := thermostat.CheckOption(systemString(value), d.SystemOptions); err != nil {
		return err
	}
	if d.Offline {
		return thermostat.ErrConflict
	}
	n, _ := strconv.Atoi(id)
	hold := HoldPermanent
	return h.submitControlChanges(ZoneID(n), ControlChanges{SystemSwitch: &value, StatusHeat: &hold, StatusCool: &hold})
}

func (h *Backend) SetFan(id, fan string) error {
	h.controlMu.Lock()
	defer h.controlMu.Unlock()
	value, err := parseFan(fan)
	if err != nil {
		return fmt.Errorf("%w: %v", thermostat.ErrInvalid, err)
	}
	d, err := h.Device(id)
	if err != nil {
		return err
	}
	if err := thermostat.CheckOption(fanString(value), d.FanOptions); err != nil {
		return err
	}
	if d.Offline {
		return thermostat.ErrConflict
	}
	n, _ := strconv.Atoi(id)
	hold := HoldPermanent
	return h.submitControlChanges(ZoneID(n), ControlChanges{FanMode: &value, StatusHeat: &hold, StatusCool: &hold})
}

// Refresh lazily on use rather than keeping a background goroutine alive.
func (h *Backend) refreshIfDue() error {
	h.reauthMu.Lock()
	defer h.reauthMu.Unlock()
	if !h.lastLoginAttempt.IsZero() && time.Since(h.lastLoginAttempt) >= ReloginInterval {
		return h.reloginLocked()
	}
	return nil
}

func (h *Backend) zones() ([]Zone, error) {
	zones, err := h.session.Zones()
	if err == nil {
		return zones, nil
	}
	if err := h.maybeRelogin(err); err != nil {
		return nil, err
	}
	return h.session.Zones()
}

func (h *Backend) zoneInfo(zoneID ZoneID) (*ZoneInfo, error) {
	info, err := h.session.ZoneInfo(zoneID)
	if err == nil {
		return info, nil
	}
	if err := h.maybeRelogin(err); err != nil {
		return nil, err
	}
	return h.session.ZoneInfo(zoneID)
}

func (h *Backend) zoneInfos(zoneIDs []ZoneID) (map[ZoneID]*ZoneInfo, error) {
	infos, err := h.session.ZoneInfos(zoneIDs)
	if err == nil {
		return infos, nil
	}
	if err := h.maybeRelogin(err); err != nil {
		return nil, err
	}
	return h.session.ZoneInfos(zoneIDs)
}

func (h *Backend) submitControlChanges(zoneID ZoneID, changes ControlChanges) error {
	err := h.session.SubmitControlChanges(zoneID, changes)
	if err == nil {
		return nil
	}
	if err := h.maybeRelogin(err); err != nil {
		return err
	}
	return h.session.SubmitControlChanges(zoneID, changes)
}

func (h *Backend) maybeRelogin(err error) error {
	if err == nil || !errors.Is(err, ErrUnauthorized) {
		return err
	}
	h.reauthMu.Lock()
	defer h.reauthMu.Unlock()
	if time.Since(h.lastLoginAttempt) < MinLoginInterval {
		if h.lastLoginErr == nil {
			return nil
		}
		return err
	}
	return h.reloginLocked()
}

func (h *Backend) reloginLocked() error {
	h.lastLoginAttempt = time.Now()
	if reloginErr := h.session.Relogin(h.username, h.password); reloginErr != nil {
		h.lastLoginErr = reloginErr
		return fmt.Errorf("relogin: %w", reloginErr)
	}
	h.lastLoginErr = nil
	return nil
}

func deviceFromInfo(info *ZoneInfo) thermostat.Device {
	displayTemperature := (*float64)(nil)
	if info.DisplayTemperatureAvailable {
		displayTemperature = &info.DisplayTemperature
	}
	response := thermostat.Device{
		ID:                 strconv.Itoa(int(info.DeviceID)),
		DisplayTemperature: displayTemperature,
		DisplayedUnits:     info.DisplayedUnits,
		System:             systemString(info.SystemSwitchPosition),
		Fan:                fanString(info.FanMode),
		HeatSetpoint:       info.HeatSetpoint,
		CoolSetpoint:       info.CoolSetpoint,
		EquipmentRunning:   info.EquipmentOutputStatus != 0,
		FanRunning:         info.IsFanRunning,
		RuntimeAvailable:   info.RuntimeStatusAvailable,
		Offline:            info.IsLost || info.GatewayIsLost || info.CommunicationLost,
		HeatRange: thermostat.Range{
			Min: info.HeatLowerSetpointLimit,
			Max: info.HeatUpperSetpointLimit,
		},
		CoolRange: thermostat.Range{
			Min: info.CoolLowerSetpointLimit,
			Max: info.CoolUpperSetpointLimit,
		},
		SetpointAllowed: info.SetpointChangeAllowed,
		TemperatureStep: 1,
		SystemOptions:   []string{},
		FanOptions:      []string{},
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
	if info.DisplayedUnits == "C" {
		response.TemperatureStep = 0.5
	}
	switch info.SystemSwitchPosition {
	case SystemSwitchHeat:
		response.ActiveSetpoint = &response.HeatSetpoint
	case SystemSwitchCool:
		response.ActiveSetpoint = &response.CoolSetpoint
	}
	return response
}

func parseSystem(value string) (SystemSwitch, error) {
	switch strings.ToLower(value) {
	case "heat":
		return SystemSwitchHeat, nil
	case "cool":
		return SystemSwitchCool, nil
	case "off":
		return SystemSwitchOff, nil
	default:
		return 0, fmt.Errorf("unknown system %q", value)
	}
}

func parseFan(value string) (FanMode, error) {
	switch strings.ToLower(value) {
	case "auto":
		return FanModeAuto, nil
	case "on":
		return FanModeOn, nil
	case "circulate":
		return FanModeCirculate, nil
	default:
		return 0, fmt.Errorf("unknown fan mode %q", value)
	}
}

func systemString(value SystemSwitch) string {
	switch value {
	case SystemSwitchHeat:
		return "heat"
	case SystemSwitchCool:
		return "cool"
	case SystemSwitchOff:
		return "off"
	default:
		return "unknown"
	}
}

func fanString(value FanMode) string {
	switch value {
	case FanModeAuto:
		return "auto"
	case FanModeOn:
		return "on"
	case FanModeCirculate:
		return "circulate"
	default:
		return "unknown"
	}
}
