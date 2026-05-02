package tcc

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestRefreshZonesReturnsUnauthorizedError(t *testing.T) {
	session := &Session{
		client: &http.Client{
			Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return responseWithStatus(http.StatusUnauthorized), nil
			}),
		},
		zonesURL: "https://mytotalconnectcomfort.com/portal/1000000/Zones",
	}
	err := session.RefreshZones()
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized; got %v", err)
	}
	if err == ErrUnauthorized {
		t.Fatal("expected ErrUnauthorized to be wrapped with operation context")
	}
}

func TestSubmitControlChangesReturnsUnauthorizedError(t *testing.T) {
	session := &Session{
		client: &http.Client{
			Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return responseWithStatus(http.StatusUnauthorized), nil
			}),
		},
	}
	err := session.SetCoolSetpoint(1000001, 68)
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized; got %v", err)
	}
	if err == ErrUnauthorized {
		t.Fatal("expected ErrUnauthorized to be wrapped with operation context")
	}
}

func TestSubmitControlChangesRejectsInvalidPeriod(t *testing.T) {
	period := Period(PeriodsPerDay)
	session := &Session{
		client: &http.Client{
			Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				t.Fatal("unexpected HTTP request")
				return nil, nil
			}),
		},
	}
	err := session.SubmitControlChanges(1000001, ControlChanges{HeatNextPeriod: &period})
	if err == nil {
		t.Fatal("expected invalid period to fail")
	}
}

func TestZoneInfoReturnsUnauthorizedError(t *testing.T) {
	session := &Session{
		client: &http.Client{
			Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return responseWithStatus(http.StatusUnauthorized), nil
			}),
		},
	}
	_, err := session.ZoneInfo(1000001)
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized; got %v", err)
	}
	if err == ErrUnauthorized {
		t.Fatal("expected ErrUnauthorized to be wrapped with operation context")
	}
}

func TestZoneInfoFetchesControlPage(t *testing.T) {
	var requestURL *url.URL
	session := &Session{
		client: &http.Client{
			Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				requestURL = request.URL
				response := responseWithStatus(http.StatusOK)
				response.Body = io.NopCloser(strings.NewReader(`
Control.Model.set(Control.Model.Property.deviceID, 1000001);
Control.Model.set(Control.Model.Property.systemSwitchPosition, 1);
Control.Model.set(Control.Model.Property.fanMode, 2);
Control.Model.set(Control.Model.Property.hasFan, true);
`))
				return response, nil
			}),
		},
	}
	info, err := session.ZoneInfo(1000001)
	if err != nil {
		t.Fatal(err)
	}
	if requestURL.String() != "https://mytotalconnectcomfort.com/portal/Device/Control/1000001?page=1" {
		t.Fatalf("unexpected request URL: %s", requestURL)
	}
	if info.SystemSwitchPosition != SystemSwitchHeat {
		t.Fatalf("expected heat mode; got %d", info.SystemSwitchPosition)
	}
	if info.FanMode != FanModeCirculate || !info.HasFan {
		t.Fatalf("unexpected fan state: mode=%d hasFan=%t", info.FanMode, info.HasFan)
	}
}

func TestZoneInfoMergesRuntimeStatus(t *testing.T) {
	var requestCount int
	session := &Session{
		client: &http.Client{
			Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				requestCount++
				response := responseWithStatus(http.StatusOK)
				switch requestCount {
				case 1:
					response.Body = io.NopCloser(strings.NewReader(`
Control.Model.set(Control.Model.Property.deviceID, 1000001);
Control.Model.set(Control.Model.Property.systemSwitchPosition, 3);
Control.Model.set(Control.Model.Property.fanMode, 0);
`))
				case 2:
					if request.Method != http.MethodPost {
						t.Fatalf("expected runtime status POST; got %s", request.Method)
					}
					if request.URL.String() != "https://mytotalconnectcomfort.com/portal/Device/GetZoneListData?locationId=1000000&page=1" {
						t.Fatalf("unexpected runtime status URL: %s", request.URL)
					}
					response.Body = io.NopCloser(strings.NewReader(`[
						{"DeviceID":1000002,"IsLost":false,"GatewayIsLost":false,"GatewayUpgrading":false,"EquipmentOutputStatus":0,"IsFanRunning":false},
						{"DeviceID":1000001,"IsLost":false,"GatewayIsLost":false,"GatewayUpgrading":false,"EquipmentOutputStatus":2,"IsFanRunning":true}
					]`))
				default:
					t.Fatalf("unexpected request %d", requestCount)
				}
				return response, nil
			}),
		},
		locationID: 1000000,
		zonesURL:   "https://mytotalconnectcomfort.com/portal/1000000/Zones",
	}
	info, err := session.ZoneInfo(1000001)
	if err != nil {
		t.Fatal(err)
	}
	if !info.RuntimeStatusAvailable {
		t.Fatal("expected runtime status to be available")
	}
	if !info.IsFanRunning {
		t.Fatal("expected fan to be running")
	}
	if info.EquipmentOutputStatus != 2 {
		t.Fatalf("expected equipment output status 2; got %d", info.EquipmentOutputStatus)
	}
	if info.SystemSwitchPosition != SystemSwitchCool {
		t.Fatalf("expected cool mode; got %d", info.SystemSwitchPosition)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (r roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return r(request)
}

func responseWithStatus(statusCode int) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Status:     http.StatusText(statusCode),
		Body:       io.NopCloser(strings.NewReader("")),
		Header:     make(http.Header),
	}
}
