package tcc

import (
	"bytes"
	"encoding/json"
	"net/url"
	"testing"
)

func TestParseZones(t *testing.T) {
	zones, err := parseZones(bytes.NewBufferString(`
<div id="zone-list">
	<table>
		<tr class="separator"></tr>
		<tr class="gray-capsule pointerCursor" data-id="1000001" data-url="/portal/Device/Control/1000001?page=1">
			<td class="location-zone-title">
				<div class="location-name">UPSTAIRS</div>
			</td>
			<td class="zone-temperature">
				<span class="tempValue">68&deg;</span>
			</td>
			<td class="zone-humidity">
				<div class="hum-num">34%</div>
			</td>
		</tr>
		<tr class="gray-capsule pointerCursor" data-id="1000002" data-url="/portal/Device/Control/1000002?page=1">
			<td class="location-zone-title">
				<div class="location-name">DOWNSTAIRS</div>
			</td>
			<td class="zone-temperature">
				<span class="tempValue">66&deg;</span>
			</td>
			<td class="zone-humidity">
				<div class="hum-num">34%</div>
			</td>
		</tr>
	</table>
</div>`))
	if err != nil {
		t.Fatal(err)
	}
	if len(zones) != 2 {
		t.Fatalf("expected 2 zones; got %d", len(zones))
	}
	if zones[0].ID != 1000001 {
		t.Fatalf("expected first zone ID 1000001; got %d", zones[0].ID)
	}
	if zones[0].Name != "UPSTAIRS" {
		t.Fatalf("expected first zone name; got %q", zones[0].Name)
	}
	if zones[0].ControlURL != "https://mytotalconnectcomfort.com/portal/Device/Control/1000001?page=1" {
		t.Fatalf("expected first zone control URL; got %q", zones[0].ControlURL)
	}
	if zones[0].Temperature == nil || *zones[0].Temperature != 68 {
		t.Fatalf("expected first zone temperature 68; got %v", zones[0].Temperature)
	}
	if zones[0].Humidity == nil || *zones[0].Humidity != 34 {
		t.Fatalf("expected first zone humidity 34; got %v", zones[0].Humidity)
	}
	if zones[1].ID != 1000002 || zones[1].Name != "DOWNSTAIRS" {
		t.Fatalf("unexpected second zone: %+v", zones[1])
	}
}

func TestControlScreenChangesJSONIncludesNulls(t *testing.T) {
	cool := 68.0
	body, err := json.Marshal(controlScreenChanges{
		DeviceID:     1000001,
		CoolSetpoint: &cool,
	})
	if err != nil {
		t.Fatal(err)
	}
	expected := `{"DeviceID":1000001,"SystemSwitch":null,"HeatSetpoint":null,"CoolSetpoint":68,"HeatNextPeriod":null,"CoolNextPeriod":null,"StatusHeat":null,"StatusCool":null,"FanMode":null}`
	if string(body) != expected {
		t.Fatalf("unexpected JSON:\n%s", body)
	}
}

func TestControlScreenChangesJSONIncludesPeriods(t *testing.T) {
	heatPeriod := Period(74)
	coolPeriod := Period(75)
	body, err := json.Marshal(controlScreenChanges{
		DeviceID:       1000001,
		HeatNextPeriod: &heatPeriod,
		CoolNextPeriod: &coolPeriod,
	})
	if err != nil {
		t.Fatal(err)
	}
	expected := `{"DeviceID":1000001,"SystemSwitch":null,"HeatSetpoint":null,"CoolSetpoint":null,"HeatNextPeriod":74,"CoolNextPeriod":75,"StatusHeat":null,"StatusCool":null,"FanMode":null}`
	if string(body) != expected {
		t.Fatalf("unexpected JSON:\n%s", body)
	}
}

func TestLocationIDFromZonesURL(t *testing.T) {
	value, err := url.Parse("https://mytotalconnectcomfort.com/portal/1000000/Zones")
	if err != nil {
		t.Fatal(err)
	}
	if locationID := locationIDFromZonesURL(value); locationID != 1000000 {
		t.Fatalf("expected location ID 1000000; got %d", locationID)
	}
}
