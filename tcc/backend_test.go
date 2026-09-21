package tcc

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/unixpickle/tcc/thermostat"
)

func backendFixture() (*Backend, *fakeSession) {
	temp, humidity := 68.0, 35.0
	s := &fakeSession{
		zones: []Zone{{ID: 1001, Name: "Downstairs", Temperature: &temp, Humidity: &humidity}},
		infos: map[ZoneID]*ZoneInfo{1001: {
			DeviceID: 1001, DisplayedUnits: "F", DisplayTemperatureAvailable: true, DisplayTemperature: 68,
			SystemSwitchPosition: SystemSwitchHeat, FanMode: FanModeAuto,
			HeatSetpoint: 67, CoolSetpoint: 73, Deadband: 1,
			SetpointChangeAllowed: true, SwitchHeatAllowed: true, SwitchCoolAllowed: true, SwitchOffAllowed: true,
			FanModeAutoAllowed: true, FanModeOnAllowed: true, FanModeCirculateAllowed: true,
			HeatLowerSetpointLimit: 40, HeatUpperSetpointLimit: 90, CoolLowerSetpointLimit: 50, CoolUpperSetpointLimit: 99,
			EquipmentOutputStatus: 2, RuntimeStatusAvailable: true,
		}},
	}
	return &Backend{session: s, username: "user", password: "pass"}, s
}

func TestBackendDiscoveryAndControls(t *testing.T) {
	b, s := backendFixture()
	devices, err := b.Devices()
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 {
		t.Fatalf("devices: %+v", devices)
	}
	d := devices[0]
	if d.ID != "1001" || d.Name != "Downstairs" || *d.ActiveSetpoint != 67 || *d.Humidity != 35 || !d.EquipmentRunning {
		t.Fatalf("state: %+v", d)
	}
	if s.zoneCalls != 1 {
		t.Fatalf("discovery queried zones %d times", s.zoneCalls)
	}
	if err := b.SetTemperature("1001", 69, ""); err != nil {
		t.Fatal(err)
	}
	if s.lastChange.HeatSetpoint == nil || *s.lastChange.HeatSetpoint != 69 || s.lastChange.CoolSetpoint != nil {
		t.Fatalf("changes: %+v", s.lastChange)
	}
	if *s.lastChange.StatusHeat != HoldPermanent || *s.lastChange.StatusCool != HoldPermanent {
		t.Fatal("missing permanent hold")
	}
	if err := b.SetTemperature("1001", 69, "cool"); err != nil {
		t.Fatal(err)
	}
	if *s.lastChange.HeatSetpoint != 68 || *s.lastChange.CoolSetpoint != 69 {
		t.Fatalf("deadband changes: %+v", s.lastChange)
	}
	if err := b.SetSystem("1001", "cool"); err != nil {
		t.Fatal(err)
	}
	d, err = b.Device("1001")
	if err != nil || d.System != "cool" || *d.ActiveSetpoint != 69 || d.Name != "Downstairs" {
		t.Fatalf("reloaded: %+v %v", d, err)
	}
	if err := b.SetFan("1001", "on"); err != nil {
		t.Fatal(err)
	}
	if *s.lastChange.FanMode != FanModeOn || *s.lastChange.StatusHeat != HoldPermanent {
		t.Fatal("fan/hold not set")
	}
}

func TestBackendRejectsInvalidControls(t *testing.T) {
	b, s := backendFixture()
	for _, id := range []string{"", "nope", "-1", "9999"} {
		if _, err := b.Device(id); !errors.Is(err, thermostat.ErrNotFound) {
			t.Fatalf("%q: %v", id, err)
		}
	}
	if err := b.SetTemperature("1001", 101, "cool"); !errors.Is(err, thermostat.ErrInvalid) {
		t.Fatal(err)
	}
	if err := b.SetSystem("1001", "auto"); !errors.Is(err, thermostat.ErrInvalid) {
		t.Fatal(err)
	}
	if err := b.SetFan("1001", "bogus"); !errors.Is(err, thermostat.ErrInvalid) {
		t.Fatal(err)
	}
	s.infos[1001].SystemSwitchPosition = SystemSwitchOff
	if err := b.SetTemperature("1001", 68, ""); !errors.Is(err, thermostat.ErrConflict) {
		t.Fatal(err)
	}
	if s.lastID != 0 {
		t.Fatal("invalid controls reached device")
	}
}

func TestBackendEmptyDevices(t *testing.T) {
	b := &Backend{session: &fakeSession{}}
	d, err := b.Devices()
	if err != nil || d == nil || len(d) != 0 {
		t.Fatalf("devices: %+v %v", d, err)
	}
}

func TestBackendReauthentication(t *testing.T) {
	for _, operation := range []string{"zones", "info", "submit"} {
		t.Run(operation, func(t *testing.T) {
			b, s := backendFixture()
			unauthorized := fmt.Errorf("expired: %w", ErrUnauthorized)
			switch operation {
			case "zones":
				s.zoneErrors = []error{unauthorized}
			case "info":
				s.infoErrors = []error{unauthorized}
			case "submit":
				s.submitErrors = []error{unauthorized}
			}
			var err error
			if operation == "submit" {
				err = b.SetFan("1001", "on")
			} else {
				_, err = b.Devices()
			}
			if err != nil || s.reloginCalls != 1 {
				t.Fatalf("relogin calls=%d error=%v", s.reloginCalls, err)
			}
		})
	}
}

func TestBackendReloginThrottle(t *testing.T) {
	for _, failed := range []bool{false, true} {
		b, s := backendFixture()
		b.lastLoginAttempt = time.Now()
		if failed {
			b.lastLoginErr = errors.New("recent relogin failed")
		}
		s.zoneErrors = []error{ErrUnauthorized}
		_, err := b.Devices()
		if failed && !errors.Is(err, ErrUnauthorized) || !failed && err != nil {
			t.Fatalf("failed=%v err=%v", failed, err)
		}
		if s.reloginCalls != 0 {
			t.Fatal("recent relogin was repeated")
		}
	}
	b, s := backendFixture()
	s.zoneErrors = []error{ErrUnauthorized}
	s.reloginErr = errors.New("bad credentials")
	if _, err := b.Devices(); !errors.Is(err, s.reloginErr) {
		t.Fatal(err)
	}
	b.lastLoginAttempt = time.Now().Add(-ReloginInterval)
	s.reloginErr = nil
	if _, err := b.Devices(); err != nil {
		t.Fatal(err)
	}
	if s.reloginCalls != 2 {
		t.Fatalf("lazy periodic refresh: %d", s.reloginCalls)
	}
}

func TestBackendReloginAfterRepeatedTimeouts(t *testing.T) {
	b, s := backendFixture()
	timeout := fmt.Errorf("TCC stalled: %w", context.DeadlineExceeded)
	s.zoneErrors = []error{timeout, timeout, timeout}
	for i := 0; i < TimeoutReloginThreshold-1; i++ {
		if _, err := b.Devices(); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("timeout %d: %v", i+1, err)
		}
	}
	if s.reloginCalls != 0 {
		t.Fatalf("relogin occurred before threshold: %d", s.reloginCalls)
	}
	if _, err := b.Devices(); err != nil {
		t.Fatal(err)
	}
	if s.reloginCalls != 1 {
		t.Fatalf("relogin calls=%d", s.reloginCalls)
	}
}

func TestBackendSuccessfulRequestResetsTimeoutCount(t *testing.T) {
	b, s := backendFixture()
	timeout := fmt.Errorf("TCC stalled: %w", context.DeadlineExceeded)
	s.zoneErrors = []error{timeout}
	if _, err := b.Devices(); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if _, err := b.Devices(); err != nil {
		t.Fatal(err)
	}
	s.zoneErrors = []error{timeout, timeout}
	for range 2 {
		if _, err := b.Devices(); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
	}
	if s.reloginCalls != 0 {
		t.Fatalf("relogin calls=%d", s.reloginCalls)
	}
}

func TestBackendPeriodicRelogin(t *testing.T) {
	b, s := backendFixture()
	s.reloginNotify = make(chan struct{}, 1)
	done := make(chan struct{})
	go b.reloginEvery(time.Millisecond, done)
	select {
	case <-s.reloginNotify:
	case <-time.After(time.Second):
		close(done)
		t.Fatal("periodic relogin did not run")
	}
	close(done)
}

type fakeSession struct {
	zoneCalls     int
	infoCalls     int
	reloginCalls  int
	zones         []Zone
	infos         map[ZoneID]*ZoneInfo
	zoneErrors    []error
	infoErrors    []error
	submitErrors  []error
	reloginErr    error
	reloginNotify chan struct{}
	lastID        ZoneID
	lastChange    ControlChanges
}

func (f *fakeSession) Zones() ([]Zone, error) {
	f.zoneCalls++
	if len(f.zoneErrors) != 0 {
		err := f.zoneErrors[0]
		f.zoneErrors = f.zoneErrors[1:]
		return nil, err
	}
	return f.zones, nil
}

func (f *fakeSession) ZoneInfo(id ZoneID) (*ZoneInfo, error) {
	f.infoCalls++
	if len(f.infoErrors) != 0 {
		err := f.infoErrors[0]
		f.infoErrors = f.infoErrors[1:]
		return nil, err
	}
	info := *f.infos[id]
	return &info, nil
}

func (f *fakeSession) ZoneInfos(ids []ZoneID) (map[ZoneID]*ZoneInfo, error) {
	infos := make(map[ZoneID]*ZoneInfo, len(ids))
	for _, id := range ids {
		info, err := f.ZoneInfo(id)
		if err != nil {
			return nil, err
		}
		infos[id] = info
	}
	return infos, nil
}

func (f *fakeSession) SubmitControlChanges(id ZoneID, changes ControlChanges) error {
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
	if f.reloginNotify != nil {
		select {
		case f.reloginNotify <- struct{}{}:
		default:
		}
	}
	return f.reloginErr
}
