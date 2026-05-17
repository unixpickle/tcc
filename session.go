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
	"sync"
	"time"
)

const (
	baseURL           = "https://mytotalconnectcomfort.com"
	loginURL          = baseURL + "/portal/"
	submitControlURL  = baseURL + "/portal/Device/SubmitControlScreenChanges"
	defaultControlURL = baseURL + "/portal/Device/Control/%d?page=1"
	zoneListDataURL   = baseURL + "/portal/Device/GetZoneListData?locationId=%d&page=1"
	defaultTimeout    = 30 * time.Second
	rapidRetryTimeout = 2 * time.Second
	rapidRetryCount   = 4
)

var (
	ErrLoginFailed  = errors.New("TCC login failed")
	ErrUnauthorized = errors.New("TCC session unauthorized")
)

type Session struct {
	clientMu sync.RWMutex
	client   *sessionClient
}

type sessionClient struct {
	httpClient *http.Client
	locationID int
	zonesURL   string
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
	client, err := login(username, password)
	if err != nil {
		return nil, err
	}
	return &Session{client: client}, nil
}

func login(username, password string) (*sessionClient, error) {
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
	client := newHTTPClient(jar)
	response, err := client.PostForm(loginURL, body)
	if err != nil {
		return nil, err
	}
	if response.Body != nil {
		defer response.Body.Close()
	}
	if err := checkResponseStatus("login", response); err != nil {
		return nil, err
	}
	if !strings.HasSuffix(response.Request.URL.Path, "Zones") {
		return nil, ErrLoginFailed
	}
	return &sessionClient{
		httpClient: client,
		locationID: locationIDFromZonesURL(response.Request.URL),
		zonesURL:   response.Request.URL.String(),
	}, nil
}

func newHTTPClient(jar http.CookieJar) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DisableKeepAlives = true
	return &http.Client{
		Jar:       jar,
		Timeout:   defaultTimeout,
		Transport: rapidRetryTransport{base: transport},
	}
}

func (s *Session) Relogin(username, password string) error {
	s.clientMu.Lock()
	defer s.clientMu.Unlock()
	client, err := login(username, password)
	if err != nil {
		return err
	}
	s.client = client
	return nil
}

func (s *Session) Zones() ([]Zone, error) {
	client := s.getClient()
	response, err := client.httpClient.Get(client.zonesURL)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if err := checkResponseStatus("fetch zones", response); err != nil {
		return nil, err
	}
	if !strings.HasSuffix(response.Request.URL.Path, "Zones") {
		return nil, fmt.Errorf("fetch zones: %w", ErrUnauthorized)
	}
	zones, err := parseZones(response.Body)
	if err != nil {
		return nil, err
	}
	return zones, nil
}

func (s *Session) ZoneInfo(zoneID ZoneID) (*ZoneInfo, error) {
	client := s.getClient()
	info, err := fetchZoneInfoControlPage(client, zoneID)
	if err != nil {
		return nil, err
	}
	if client.locationID != 0 {
		statuses, err := fetchZoneRuntimeStatuses(client)
		if err != nil {
			return nil, err
		}
		applyZoneRuntimeStatuses(info, statuses)
	}
	return info, nil
}

func (s *Session) ZoneInfos(zoneIDs []ZoneID) (map[ZoneID]*ZoneInfo, error) {
	client := s.getClient()
	infos := make(map[ZoneID]*ZoneInfo, len(zoneIDs))
	for _, zoneID := range zoneIDs {
		info, err := fetchZoneInfoControlPage(client, zoneID)
		if err != nil {
			return nil, err
		}
		infos[zoneID] = info
	}
	if client.locationID != 0 && len(infos) != 0 {
		statuses, err := fetchZoneRuntimeStatuses(client)
		if err != nil {
			return nil, err
		}
		for _, info := range infos {
			applyZoneRuntimeStatuses(info, statuses)
		}
	}
	return infos, nil
}

func fetchZoneInfoControlPage(client *sessionClient, zoneID ZoneID) (*ZoneInfo, error) {
	response, err := client.httpClient.Get(fmt.Sprintf(defaultControlURL, zoneID))
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
	return info, nil
}

func fetchZoneRuntimeStatuses(client *sessionClient) ([]zoneRuntimeStatus, error) {
	request, err := http.NewRequest(http.MethodPost, fmt.Sprintf(zoneListDataURL, client.locationID), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json, text/javascript, */*; q=0.01")
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	request.Header.Set("Origin", baseURL)
	request.Header.Set("Referer", client.zonesURL)
	request.Header.Set("X-Requested-With", "XMLHttpRequest")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if err := checkResponseStatus("fetch zone runtime status", response); err != nil {
		return nil, err
	}
	var statuses []zoneRuntimeStatus
	if err := json.NewDecoder(response.Body).Decode(&statuses); err != nil {
		return nil, err
	}
	return statuses, nil
}

func applyZoneRuntimeStatuses(info *ZoneInfo, statuses []zoneRuntimeStatus) {
	for _, status := range statuses {
		if status.DeviceID == info.DeviceID {
			info.RuntimeStatusAvailable = true
			info.IsLost = status.IsLost
			info.GatewayIsLost = status.GatewayIsLost
			info.GatewayUpgrading = status.GatewayUpgrading
			info.EquipmentOutputStatus = status.EquipmentOutputStatus
			info.IsFanRunning = status.IsFanRunning
			return
		}
	}
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
	client := s.getClient()
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
	response, err := client.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if err := checkResponseStatus("submit control changes", response); err != nil {
		return err
	}
	return nil
}

func (s *Session) getClient() *sessionClient {
	s.clientMu.RLock()
	defer s.clientMu.RUnlock()
	return s.client
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
