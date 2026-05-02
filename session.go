package tcc

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	baseURL           = "https://mytotalconnectcomfort.com"
	loginURL          = baseURL + "/portal/"
	submitControlURL  = baseURL + "/portal/Device/SubmitControlScreenChanges"
	defaultControlURL = baseURL + "/portal/Device/Control/%d?page=1"
	zoneListDataURL   = baseURL + "/portal/Device/GetZoneListData?locationId=%d&page=1"
)

var (
	ErrLoginFailed  = errors.New("TCC login failed")
	ErrUnauthorized = errors.New("TCC session unauthorized")
)

type Session struct {
	client     *http.Client
	locationID int
	zonesURL   string
	zones      []Zone
}

type Zone struct {
	ID          ZoneID
	Name        string
	ControlURL  string
	Temperature *float64
	Humidity    *float64
}

type ZoneID int

type SystemSwitch int

const (
	SystemSwitchHeat SystemSwitch = 1
	SystemSwitchOff  SystemSwitch = 2
	SystemSwitchCool SystemSwitch = 3
)

type FanMode int

const (
	FanModeAuto      FanMode = 0
	FanModeOn        FanMode = 1
	FanModeCirculate FanMode = 2
)

type HoldStatus int

const (
	HoldTemporary HoldStatus = 1
	HoldPermanent HoldStatus = 2
)

type ControlChanges struct {
	SystemSwitch   *SystemSwitch
	HeatSetpoint   *float64
	CoolSetpoint   *float64
	HeatNextPeriod *Period
	CoolNextPeriod *Period
	StatusHeat     *HoldStatus
	StatusCool     *HoldStatus
	FanMode        *FanMode
}

func NewSession(username, password string) (*Session, error) {
	d := time.Now()
	_, offsetSeconds := d.Zone()
	timeOffset := -offsetSeconds / 60
	body := url.Values{
		"timeOffset": []string{strconv.Itoa(timeOffset)},
		"UserName":   []string{username},
		"Password":   []string{password},
		"RememberMe": []string{"true"},
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{
		Jar: jar,
	}
	response, err := client.PostForm(loginURL, body)
	if err != nil {
		return nil, err
	}
	if response.Body != nil {
		defer response.Body.Close()
	}
	if !strings.HasSuffix(response.Request.URL.Path, "Zones") {
		return nil, ErrLoginFailed
	}
	zones, err := parseZones(response.Body)
	if err != nil {
		return nil, err
	}
	return &Session{
		client:     client,
		locationID: locationIDFromZonesURL(response.Request.URL),
		zonesURL:   response.Request.URL.String(),
		zones:      zones,
	}, nil
}

func (s *Session) Zones() []Zone {
	zones := make([]Zone, len(s.zones))
	copy(zones, s.zones)
	return zones
}

func (s *Session) RefreshZones() error {
	response, err := s.client.Get(s.zonesURL)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if err := checkResponseStatus("refresh zones", response); err != nil {
		return err
	}
	zones, err := parseZones(response.Body)
	if err != nil {
		return err
	}
	s.zones = zones
	return nil
}

func (s *Session) ZoneInfo(zoneID ZoneID) (*ZoneInfo, error) {
	response, err := s.client.Get(fmt.Sprintf(defaultControlURL, zoneID))
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if err := checkResponseStatus("fetch zone info", response); err != nil {
		return nil, err
	}
	info, err := parseZoneInfo(response.Body)
	if err != nil {
		return nil, err
	}
	if s.locationID != 0 {
		if err := s.populateZoneInfoRuntimeStatus(info); err != nil {
			return nil, err
		}
	}
	return info, nil
}

func (s *Session) populateZoneInfoRuntimeStatus(info *ZoneInfo) error {
	request, err := http.NewRequest(http.MethodPost, fmt.Sprintf(zoneListDataURL, s.locationID), nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json, text/javascript, */*; q=0.01")
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	request.Header.Set("Origin", baseURL)
	request.Header.Set("Referer", s.zonesURL)
	request.Header.Set("X-Requested-With", "XMLHttpRequest")
	response, err := s.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if err := checkResponseStatus("fetch zone runtime status", response); err != nil {
		return err
	}
	var statuses []zoneRuntimeStatus
	if err := json.NewDecoder(response.Body).Decode(&statuses); err != nil {
		return err
	}
	for _, status := range statuses {
		if status.DeviceID == info.DeviceID {
			info.RuntimeStatusAvailable = true
			info.IsLost = status.IsLost
			info.GatewayIsLost = status.GatewayIsLost
			info.GatewayUpgrading = status.GatewayUpgrading
			info.EquipmentOutputStatus = status.EquipmentOutputStatus
			info.IsFanRunning = status.IsFanRunning
			return nil
		}
	}
	return nil
}

func (s *Session) SetSystemSwitch(zoneID ZoneID, value SystemSwitch) error {
	return s.SubmitControlChanges(zoneID, ControlChanges{SystemSwitch: &value})
}

func (s *Session) SetHeatSetpoint(zoneID ZoneID, temperature float64) error {
	return s.SubmitControlChanges(zoneID, ControlChanges{HeatSetpoint: &temperature})
}

func (s *Session) SetCoolSetpoint(zoneID ZoneID, temperature float64) error {
	return s.SubmitControlChanges(zoneID, ControlChanges{CoolSetpoint: &temperature})
}

func (s *Session) SetFanMode(zoneID ZoneID, value FanMode) error {
	return s.SubmitControlChanges(zoneID, ControlChanges{FanMode: &value})
}

func (s *Session) SetHold(zoneID ZoneID, value HoldStatus) error {
	return s.SubmitControlChanges(zoneID, ControlChanges{
		StatusHeat: &value,
		StatusCool: &value,
	})
}

func (s *Session) SetNextPeriods(zoneID ZoneID, heatPeriod, coolPeriod Period) error {
	return s.SubmitControlChanges(zoneID, ControlChanges{
		HeatNextPeriod: &heatPeriod,
		CoolNextPeriod: &coolPeriod,
	})
}

func (s *Session) SetNextPeriodSetpoints(zoneID ZoneID, heatSetpoint, coolSetpoint int) error {
	heatPeriod, err := NewPeriod(heatSetpoint)
	if err != nil {
		return err
	}
	coolPeriod, err := NewPeriod(coolSetpoint)
	if err != nil {
		return err
	}
	return s.SetNextPeriods(zoneID, heatPeriod, coolPeriod)
}

func (s *Session) SubmitControlChanges(zoneID ZoneID, changes ControlChanges) error {
	if err := validatePeriodPointer("HeatNextPeriod", changes.HeatNextPeriod); err != nil {
		return err
	}
	if err := validatePeriodPointer("CoolNextPeriod", changes.CoolNextPeriod); err != nil {
		return err
	}
	body, err := json.Marshal(controlScreenChanges{
		DeviceID:       zoneID,
		SystemSwitch:   changes.SystemSwitch,
		HeatSetpoint:   changes.HeatSetpoint,
		CoolSetpoint:   changes.CoolSetpoint,
		HeatNextPeriod: changes.HeatNextPeriod,
		CoolNextPeriod: changes.CoolNextPeriod,
		StatusHeat:     changes.StatusHeat,
		StatusCool:     changes.StatusCool,
		FanMode:        changes.FanMode,
	})
	if err != nil {
		return err
	}
	request, err := http.NewRequest(http.MethodPost, submitControlURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json, text/javascript, */*; q=0.01")
	request.Header.Set("Content-Type", "application/json; charset=UTF-8")
	request.Header.Set("Origin", baseURL)
	request.Header.Set("Referer", fmt.Sprintf(defaultControlURL, zoneID))
	request.Header.Set("X-Requested-With", "XMLHttpRequest")
	response, err := s.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if err := checkResponseStatus("submit control changes", response); err != nil {
		return err
	}
	return nil
}

func validatePeriodPointer(name string, period *Period) error {
	if period != nil && !period.Valid() {
		return fmt.Errorf("%s period index %d out of range [0,%d]", name, *period, PeriodsPerDay-1)
	}
	return nil
}

func checkResponseStatus(operation string, response *http.Response) error {
	if response.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("%s: %w", operation, ErrUnauthorized)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("%s: %s", operation, response.Status)
	}
	return nil
}

type controlScreenChanges struct {
	DeviceID       ZoneID        `json:"DeviceID"`
	SystemSwitch   *SystemSwitch `json:"SystemSwitch"`
	HeatSetpoint   *float64      `json:"HeatSetpoint"`
	CoolSetpoint   *float64      `json:"CoolSetpoint"`
	HeatNextPeriod *Period       `json:"HeatNextPeriod"`
	CoolNextPeriod *Period       `json:"CoolNextPeriod"`
	StatusHeat     *HoldStatus   `json:"StatusHeat"`
	StatusCool     *HoldStatus   `json:"StatusCool"`
	FanMode        *FanMode      `json:"FanMode"`
}
