package tcc

import (
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

type ZoneInfo struct {
	DeviceID                      int
	RuntimeStatusAvailable        bool
	IsLost                        bool
	GatewayIsLost                 bool
	GatewayUpgrading              bool
	EquipmentOutputStatus         int
	IsFanRunning                  bool
	DisplayedUnits                string
	Commercial                    bool
	CommunicationLost             bool
	CoolLowerSetpointLimit        float64
	CoolUpperSetpointLimit        float64
	CoolNextPeriod                Period
	CoolSetpoint                  float64
	Deadband                      float64
	DisplayTemperature            float64
	DisplayTemperatureAvailable   bool
	DualSetpointStatus            bool
	HeatLowerSetpointLimit        float64
	HeatUpperSetpointLimit        float64
	HeatNextPeriod                Period
	HeatSetpoint                  float64
	HoldUntilCapable              bool
	IndoorHumidity                float64
	IndoorHumiditySensorAvailable bool
	IsInVacationHoldMode          bool
	OutdoorHumidity               float64
	OutdoorTemperature            float64
	ScheduledCoolSetpoint         float64
	ScheduledHeatSetpoint         float64
	ScheduleCapable               bool
	SetpointChangeAllowed         bool
	StatusCool                    HoldStatus
	StatusHeat                    HoldStatus
	SwitchAutoAllowed             bool
	SwitchCoolAllowed             bool
	SwitchEmergencyHeatAllowed    bool
	SwitchHeatAllowed             bool
	SwitchOffAllowed              bool
	SystemSwitchPosition          SystemSwitch
	TemporaryHoldUntilTime        *string
	VacationHold                  int
	VacationHoldUntilTime         *string
	FanMode                       FanMode
	FanModeAutoAllowed            bool
	FanModeOnAllowed              bool
	FanModeCirculateAllowed       bool
	FanModeFollowScheduleAllowed  bool
	HasFan                        bool
	WeatherPhrase                 string
	WeatherHumidity               string
	WeatherTemperature            string
	WeatherHasStation             bool
	WeatherIcon                   int
	CanControlHumidification      bool
}

type zoneRuntimeStatus struct {
	DeviceID              int  `json:"DeviceID"`
	IsLost                bool `json:"IsLost"`
	GatewayIsLost         bool `json:"GatewayIsLost"`
	GatewayUpgrading      bool `json:"GatewayUpgrading"`
	EquipmentOutputStatus int  `json:"EquipmentOutputStatus"`
	IsFanRunning          bool `json:"IsFanRunning"`
}

var controlModelSetPattern = regexp.MustCompile(`(?s)Control\.Model\.set\(Control\.Model\.Property\.([A-Za-z0-9_]+),\s*(.*?)\);`)

func parseZoneInfo(r io.Reader) (*ZoneInfo, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	values := controlModelValues(string(data))
	info := &ZoneInfo{}
	if err := setInt(values, "deviceID", &info.DeviceID); err != nil {
		return nil, err
	}
	if info.DeviceID == 0 {
		return nil, fmt.Errorf("parse zone info: missing deviceID")
	}
	setString(values, "displayedUnits", &info.DisplayedUnits)
	setBool(values, "commercial", &info.Commercial)
	setBool(values, "communicationLost", &info.CommunicationLost)
	setFloat(values, "coolLowerSetpLimit", &info.CoolLowerSetpointLimit)
	setFloat(values, "coolUpperSetptLimit", &info.CoolUpperSetpointLimit)
	setPeriod(values, "coolNextPeriod", &info.CoolNextPeriod)
	setFloat(values, "coolSetpoint", &info.CoolSetpoint)
	setFloat(values, "deadband", &info.Deadband)
	setFloat(values, "dispTemperature", &info.DisplayTemperature)
	setBool(values, "dispTemperatureAvailable", &info.DisplayTemperatureAvailable)
	setBool(values, "dualSetpointStatus", &info.DualSetpointStatus)
	setFloat(values, "heatLowerSetptLimit", &info.HeatLowerSetpointLimit)
	setFloat(values, "heatUpperSetptLimit", &info.HeatUpperSetpointLimit)
	setPeriod(values, "heatNextPeriod", &info.HeatNextPeriod)
	setFloat(values, "heatSetpoint", &info.HeatSetpoint)
	setBool(values, "holdUntilCapable", &info.HoldUntilCapable)
	setFloat(values, "indoorHumidity", &info.IndoorHumidity)
	setBool(values, "indoorHumiditySensorAvailable", &info.IndoorHumiditySensorAvailable)
	setBool(values, "isInVacationHoldMode", &info.IsInVacationHoldMode)
	setFloat(values, "outdoorHumidity", &info.OutdoorHumidity)
	setFloat(values, "outdoorTemp", &info.OutdoorTemperature)
	setFloat(values, "schedCoolSp", &info.ScheduledCoolSetpoint)
	setFloat(values, "schedHeatSp", &info.ScheduledHeatSetpoint)
	setBool(values, "scheduleCapable", &info.ScheduleCapable)
	setBool(values, "setpointChangeAllowed", &info.SetpointChangeAllowed)
	setHoldStatus(values, "statusCool", &info.StatusCool)
	setHoldStatus(values, "statusHeat", &info.StatusHeat)
	setBool(values, "switchAutoAllowed", &info.SwitchAutoAllowed)
	setBool(values, "switchCoolAllowed", &info.SwitchCoolAllowed)
	setBool(values, "switchEmergencyHeatAllowed", &info.SwitchEmergencyHeatAllowed)
	setBool(values, "switchHeatAllowed", &info.SwitchHeatAllowed)
	setBool(values, "switchOffAllowed", &info.SwitchOffAllowed)
	setSystemSwitch(values, "systemSwitchPosition", &info.SystemSwitchPosition)
	setOptionalString(values, "temporaryHoldUntilTime", &info.TemporaryHoldUntilTime)
	setInt(values, "vacationHold", &info.VacationHold)
	setOptionalString(values, "vacationHoldUntilTime", &info.VacationHoldUntilTime)
	setFanMode(values, "fanMode", &info.FanMode)
	setBool(values, "fanModeAutoAllowed", &info.FanModeAutoAllowed)
	setBool(values, "fanModeOnAllowed", &info.FanModeOnAllowed)
	setBool(values, "fanModeCirculateAllowed", &info.FanModeCirculateAllowed)
	setBool(values, "fanModeFollowScheduleAllowed", &info.FanModeFollowScheduleAllowed)
	setBool(values, "hasFan", &info.HasFan)
	setString(values, "weatherPhrase", &info.WeatherPhrase)
	setString(values, "weatherHumidity", &info.WeatherHumidity)
	setString(values, "weatherTemperature", &info.WeatherTemperature)
	setBool(values, "weatherHasStation", &info.WeatherHasStation)
	setInt(values, "weatherIcon", &info.WeatherIcon)
	setBool(values, "canControlHumidification", &info.CanControlHumidification)
	return info, nil
}

func controlModelValues(data string) map[string]string {
	values := map[string]string{}
	for _, match := range controlModelSetPattern.FindAllStringSubmatch(data, -1) {
		raw := strings.TrimSpace(match[2])
		if supportedControlModelValue(raw) {
			values[match[1]] = raw
		}
	}
	return values
}

func supportedControlModelValue(raw string) bool {
	if raw == "null" || raw == "true" || raw == "false" {
		return true
	}
	if _, err := strconv.ParseFloat(raw, 64); err == nil {
		return true
	}
	_, ok := parseJSString(raw)
	return ok
}

func setBool(values map[string]string, key string, target *bool) error {
	raw, ok := values[key]
	if !ok || raw == "null" {
		return nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return fmt.Errorf("parse %s: %w", key, err)
	}
	*target = value
	return nil
}

func setFloat(values map[string]string, key string, target *float64) error {
	raw, ok := values[key]
	if !ok || raw == "null" {
		return nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return fmt.Errorf("parse %s: %w", key, err)
	}
	*target = value
	return nil
}

func setInt(values map[string]string, key string, target *int) error {
	raw, ok := values[key]
	if !ok || raw == "null" {
		return nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fmt.Errorf("parse %s: %w", key, err)
	}
	*target = value
	return nil
}

func setPeriod(values map[string]string, key string, target *Period) error {
	var value int
	if err := setInt(values, key, &value); err != nil {
		return err
	}
	period, err := NewPeriod(value)
	if err != nil {
		return fmt.Errorf("parse %s: %w", key, err)
	}
	*target = period
	return nil
}

func setString(values map[string]string, key string, target *string) error {
	raw, ok := values[key]
	if !ok || raw == "null" {
		return nil
	}
	value, ok := parseJSString(raw)
	if !ok {
		return nil
	}
	*target = value
	return nil
}

func setOptionalString(values map[string]string, key string, target **string) error {
	raw, ok := values[key]
	if !ok || raw == "null" {
		return nil
	}
	value, ok := parseJSString(raw)
	if !ok {
		return nil
	}
	*target = &value
	return nil
}

func setFanMode(values map[string]string, key string, target *FanMode) error {
	var value int
	if err := setInt(values, key, &value); err != nil {
		return err
	}
	*target = FanMode(value)
	return nil
}

func setHoldStatus(values map[string]string, key string, target *HoldStatus) error {
	var value int
	if err := setInt(values, key, &value); err != nil {
		return err
	}
	*target = HoldStatus(value)
	return nil
}

func setSystemSwitch(values map[string]string, key string, target *SystemSwitch) error {
	var value int
	if err := setInt(values, key, &value); err != nil {
		return err
	}
	*target = SystemSwitch(value)
	return nil
}

func parseJSString(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if len(raw) < 2 {
		return "", false
	}
	quote := raw[0]
	if quote != '\'' && quote != '"' || raw[len(raw)-1] != quote {
		return "", false
	}
	if quote == '"' {
		value, err := strconv.Unquote(raw)
		return value, err == nil
	}
	value, err := strconv.Unquote(`"` + strings.ReplaceAll(raw[1:len(raw)-1], `"`, `\"`) + `"`)
	return value, err == nil
}
