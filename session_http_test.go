package tcc

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestZonesReturnsUnauthorizedError(t *testing.T) {
	session := &Session{
		client: &sessionClient{
			httpClient: &http.Client{
				Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
					return responseWithStatus(http.StatusUnauthorized), nil
				}),
				Timeout: defaultTimeout,
			},
			zonesURL: "https://mytotalconnectcomfort.com/portal/1000000/Zones",
		},
	}
	_, err := session.Zones()
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized; got %v", err)
	}
	if err == ErrUnauthorized {
		t.Fatal("expected ErrUnauthorized to be wrapped with operation context")
	}
}

func TestZonesRedirectToLoginReturnsUnauthorizedError(t *testing.T) {
	session := &Session{
		client: &sessionClient{
			httpClient: &http.Client{
				Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
					response := responseWithStatus(http.StatusOK)
					response.Request = &http.Request{URL: mustParseURL("https://mytotalconnectcomfort.com/portal/")}
					response.Body = io.NopCloser(strings.NewReader(`<html><body>login</body></html>`))
					return response, nil
				}),
				Timeout: defaultTimeout,
			},
			zonesURL: "https://mytotalconnectcomfort.com/portal/1000000/Zones",
		},
	}
	_, err := session.Zones()
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized; got %v", err)
	}
}

func TestSubmitControlChangesReturnsUnauthorizedError(t *testing.T) {
	session := &Session{
		client: &sessionClient{
			httpClient: &http.Client{
				Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
					return responseWithStatus(http.StatusUnauthorized), nil
				}),
				Timeout: defaultTimeout,
			},
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
		client: &sessionClient{
			httpClient: &http.Client{
				Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
					t.Fatal("unexpected HTTP request")
					return nil, nil
				}),
				Timeout: defaultTimeout,
			},
		},
	}
	err := session.SubmitControlChanges(1000001, ControlChanges{HeatNextPeriod: &period})
	if err == nil {
		t.Fatal("expected invalid period to fail")
	}
}

func TestZoneInfoReturnsUnauthorizedError(t *testing.T) {
	session := &Session{
		client: &sessionClient{
			httpClient: &http.Client{
				Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
					return responseWithStatus(http.StatusUnauthorized), nil
				}),
				Timeout: defaultTimeout,
			},
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
		client: &sessionClient{
			httpClient: &http.Client{
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
				Timeout: defaultTimeout,
			},
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
		client: &sessionClient{
			httpClient: &http.Client{
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
				Timeout: defaultTimeout,
			},
			locationID: 1000000,
			zonesURL:   "https://mytotalconnectcomfort.com/portal/1000000/Zones",
		},
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

func TestZoneInfosFetchesRuntimeStatusOnce(t *testing.T) {
	var runtimeRequestCount int
	session := &Session{
		client: &sessionClient{
			httpClient: &http.Client{
				Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
					response := responseWithStatus(http.StatusOK)
					switch request.URL.String() {
					case "https://mytotalconnectcomfort.com/portal/Device/Control/1000001?page=1":
						response.Body = io.NopCloser(strings.NewReader(`
Control.Model.set(Control.Model.Property.deviceID, 1000001);
Control.Model.set(Control.Model.Property.systemSwitchPosition, 1);
Control.Model.set(Control.Model.Property.fanMode, 0);
`))
					case "https://mytotalconnectcomfort.com/portal/Device/Control/1000002?page=1":
						response.Body = io.NopCloser(strings.NewReader(`
Control.Model.set(Control.Model.Property.deviceID, 1000002);
Control.Model.set(Control.Model.Property.systemSwitchPosition, 3);
Control.Model.set(Control.Model.Property.fanMode, 0);
`))
					case "https://mytotalconnectcomfort.com/portal/Device/GetZoneListData?locationId=1000000&page=1":
						runtimeRequestCount++
						if request.Method != http.MethodPost {
							t.Fatalf("expected runtime status POST; got %s", request.Method)
						}
						response.Body = io.NopCloser(strings.NewReader(`[
							{"DeviceID":1000001,"IsLost":false,"GatewayIsLost":false,"GatewayUpgrading":false,"EquipmentOutputStatus":1,"IsFanRunning":false},
							{"DeviceID":1000002,"IsLost":true,"GatewayIsLost":false,"GatewayUpgrading":false,"EquipmentOutputStatus":2,"IsFanRunning":true}
						]`))
					default:
						t.Fatalf("unexpected request URL: %s", request.URL)
					}
					return response, nil
				}),
				Timeout: defaultTimeout,
			},
			locationID: 1000000,
			zonesURL:   "https://mytotalconnectcomfort.com/portal/1000000/Zones",
		},
	}
	infos, err := session.ZoneInfos([]ZoneID{1000001, 1000002})
	if err != nil {
		t.Fatal(err)
	}
	if runtimeRequestCount != 1 {
		t.Fatalf("expected one runtime status request; got %d", runtimeRequestCount)
	}
	if infos[1000001].EquipmentOutputStatus != 1 || infos[1000001].IsFanRunning {
		t.Fatalf("unexpected first zone runtime status: %+v", infos[1000001])
	}
	if !infos[1000002].RuntimeStatusAvailable || !infos[1000002].IsLost || !infos[1000002].IsFanRunning {
		t.Fatalf("unexpected second zone runtime status: %+v", infos[1000002])
	}
}

func TestSessionClientsUseDefaultTimeout(t *testing.T) {
	session := &Session{
		client: &sessionClient{
			httpClient: &http.Client{Timeout: defaultTimeout},
		},
	}
	if session.getClient().httpClient.Timeout != defaultTimeout {
		t.Fatalf("expected default timeout; got %s", session.getClient().httpClient.Timeout)
	}
}

func TestNewHTTPClientUsesRapidRetryTransport(t *testing.T) {
	retryTransport, ok := newHTTPClient(nil).Transport.(rapidRetryTransport)
	if !ok {
		t.Fatalf("expected rapidRetryTransport; got %T", newHTTPClient(nil).Transport)
	}
	transport, ok := retryTransport.base.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport; got %T", retryTransport.base)
	}
	if transport.DisableKeepAlives {
		t.Fatal("expected HTTP keepalives to be enabled")
	}
}

func TestRapidRetryTransportRetriesTimedOutRequestsThenWaits(t *testing.T) {
	request, err := http.NewRequest(http.MethodGet, "https://mytotalconnectcomfort.com/portal/1000000/Zones", nil)
	if err != nil {
		t.Fatal(err)
	}
	var attempts int
	transport := rapidRetryTransport{
		base: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			attempts++
			if attempts <= rapidRetryCount {
				deadline, ok := request.Context().Deadline()
				if !ok {
					t.Fatal("rapid retry request should use a deadline")
				}
				if timeout := time.Until(deadline); timeout <= 0 || timeout > rapidRetryTimeout {
					t.Fatalf("unexpected rapid retry timeout: %s", timeout)
				}
				return nil, context.DeadlineExceeded
			}
			if _, ok := request.Context().Deadline(); ok {
				t.Fatal("final request should not use rapid retry deadline")
			}
			return responseWithStatus(http.StatusOK), nil
		}),
	}
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if attempts != rapidRetryCount+1 {
		t.Fatalf("expected %d attempts; got %d", rapidRetryCount+1, attempts)
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
		Request:    &http.Request{URL: mustParseURL("https://mytotalconnectcomfort.com/portal/1000000/Zones")},
	}
}

func mustParseURL(value string) *url.URL {
	result, err := url.Parse(value)
	if err != nil {
		panic(err)
	}
	return result
}
