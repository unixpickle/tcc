package tcc

import (
	"bytes"
	"testing"
)

func TestParseZoneInfo(t *testing.T) {
	info, err := parseZoneInfo(bytes.NewBufferString(`
<script>
Control.Model.set(Control.Model.Property.displayedUnits, 'F');
Control.Model.set(Control.Model.Property.commercial, false);
Control.Model.set(Control.Model.Property.communicationLost, false);
Control.Model.set(Control.Model.Property.coolLowerSetpLimit, 50.0000);
Control.Model.set(Control.Model.Property.coolNextPeriod, 74);
Control.Model.set(Control.Model.Property.coolSetpoint, 68.0);
Control.Model.set(Control.Model.Property.coolUpperSetptLimit, 99.0000);
Control.Model.set(Control.Model.Property.deadband, 0.0000);
Control.Model.set(Control.Model.Property.deviceID, 1000001);
Control.Model.set(Control.Model.Property.dispTemperature, 69.0000);
Control.Model.set(Control.Model.Property.dispTemperatureAvailable, true);
Control.Model.set(Control.Model.Property.dualSetpointStatus, false);
Control.Model.set(Control.Model.Property.heatLowerSetptLimit, 40.0000);
Control.Model.set(Control.Model.Property.heatNextPeriod, 74);
Control.Model.set(Control.Model.Property.heatSetpoint, 70.0);
Control.Model.set(Control.Model.Property.heatUpperSetptLimit, 90.0000);
Control.Model.set(Control.Model.Property.holdUntilCapable, true);
Control.Model.set(Control.Model.Property.indoorHumidity, 34.0000);
Control.Model.set(Control.Model.Property.indoorHumiditySensorAvailable, true);
Control.Model.set(Control.Model.Property.isInVacationHoldMode, false);
Control.Model.set(Control.Model.Property.outdoorHumidity, 51.0000);
Control.Model.set(Control.Model.Property.outdoorTemp, 54.0000);
Control.Model.set(Control.Model.Property.schedCoolSp, 72.0000);
Control.Model.set(Control.Model.Property.schedHeatSp, 68.0000);
Control.Model.set(Control.Model.Property.scheduleCapable, true);
Control.Model.set(Control.Model.Property.setpointChangeAllowed, true);
Control.Model.set(Control.Model.Property.statusCool, 2);
Control.Model.set(Control.Model.Property.statusHeat, 2);
Control.Model.set(Control.Model.Property.switchAutoAllowed, false);
Control.Model.set(Control.Model.Property.switchCoolAllowed, true);
Control.Model.set(Control.Model.Property.switchEmergencyHeatAllowed, false);
Control.Model.set(Control.Model.Property.switchHeatAllowed, true);
Control.Model.set(Control.Model.Property.switchOffAllowed, true);
Control.Model.set(Control.Model.Property.systemSwitchPosition, 3);
Control.Model.set(Control.Model.Property.temporaryHoldUntilTime, null);
Control.Model.set(Control.Model.Property.vacationHold, 0);
Control.Model.set(Control.Model.Property.vacationHoldUntilTime, null);
Control.Model.set(Control.Model.Property.fanMode, 0);
Control.Model.set(Control.Model.Property.fanModeAutoAllowed, true);
Control.Model.set(Control.Model.Property.fanModeOnAllowed, true);
Control.Model.set(Control.Model.Property.fanModeCirculateAllowed, true);
Control.Model.set(Control.Model.Property.fanModeFollowScheduleAllowed, false);
Control.Model.set(Control.Model.Property.hasFan, true);
Control.Model.set(Control.Model.Property.weatherPhrase, 'Partly sunny');
Control.Model.set(Control.Model.Property.weatherHumidity, '51');
Control.Model.set(Control.Model.Property.weatherHumidity, formatHumidity(0, false));
Control.Model.set(Control.Model.Property.weatherTemperature, '54');
Control.Model.set(Control.Model.Property.weatherTemperature, formatTemperature(0, false));
Control.Model.set(Control.Model.Property.weatherHasStation, false);
Control.Model.set(Control.Model.Property.weatherIcon, 3);
Control.Model.set(Control.Model.Property.canControlHumidification, false);
</script>`))
	if err != nil {
		t.Fatal(err)
	}
	if info.DeviceID != 1000001 {
		t.Fatalf("expected device ID 1000001; got %d", info.DeviceID)
	}
	if info.DisplayedUnits != "F" {
		t.Fatalf("expected displayed units F; got %q", info.DisplayedUnits)
	}
	if info.DisplayTemperature != 69 || !info.DisplayTemperatureAvailable {
		t.Fatalf("unexpected display temperature state: %+v", info)
	}
	if info.SystemSwitchPosition != SystemSwitchCool {
		t.Fatalf("expected system switch cool; got %d", info.SystemSwitchPosition)
	}
	if info.FanMode != FanModeAuto || !info.HasFan {
		t.Fatalf("unexpected fan state: mode=%d hasFan=%t", info.FanMode, info.HasFan)
	}
	if info.StatusHeat != HoldPermanent || info.StatusCool != HoldPermanent {
		t.Fatalf("unexpected hold state: heat=%d cool=%d", info.StatusHeat, info.StatusCool)
	}
	if info.HeatNextPeriod != 74 || info.CoolNextPeriod != 74 {
		t.Fatalf("unexpected next periods: heat=%d cool=%d", info.HeatNextPeriod, info.CoolNextPeriod)
	}
	if info.WeatherHumidity != "51" || info.WeatherTemperature != "54" {
		t.Fatalf("unexpected weather values: humidity=%q temperature=%q", info.WeatherHumidity, info.WeatherTemperature)
	}
}
