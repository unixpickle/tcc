# tcc

An API for controlling total connect comfort (Honeywell's legacy system for thermostat control).

## webui

The `webui` directory contains a standalone web server for controlling all
thermostats from one page. Credentials are read from environment variables:

```sh
TCC_USERNAME=email@example.com TCC_PASSWORD=password go run ./webui
```

The server listens on `:8080` by default. Use `-addr` to override it:

```sh
TCC_USERNAME=email@example.com TCC_PASSWORD=password go run ./webui -addr :9090
```

Pass an optional single path segment to root the UI and API under that path:

```sh
TCC_USERNAME=email@example.com TCC_PASSWORD=password go run ./webui -addr :9090 -root my-secret-root
```

With a root argument, the browser UI is served from `/{root}/` and all API
paths below are also rooted under `/{root}`.

### Web API

All responses are JSON. Control endpoints submit permanent holds.

`GET /api/devices`

Returns all thermostats and their current control/runtime state:

```json
{
  "devices": [
    {
      "id": 5572546,
      "name": "Downstairs",
      "displayTemperature": 66,
      "displayedUnits": "F",
      "system": "cool",
      "fan": "auto",
      "heatSetpoint": 65,
      "coolSetpoint": 66,
      "activeSetpoint": 66,
      "equipmentRunning": false,
      "fanRunning": true,
      "equipmentOutputStatus": 0,
      "runtimeAvailable": true,
      "offline": false,
      "systemOptions": ["heat", "cool", "off"],
      "fanOptions": ["auto", "on", "circulate"],
      "heatRange": {"min": 40, "max": 90},
      "coolRange": {"min": 50, "max": 99},
      "setpointAllowed": true
    }
  ]
}
```

`GET /api/devices/{id}`

Returns one thermostat object in the same shape used inside `devices`.

`POST /api/devices/{id}/temperature`

Sets the active temperature. Include `system` when the caller knows whether the
setpoint applies to heat or cool:

```json
{"temperature": 70, "system": "cool"}
```

`system` may be `"heat"` or `"cool"`. If omitted, the server reads the current
device mode first. The server may also adjust the paired heat/cool setpoint to
preserve the thermostat deadband.

`POST /api/devices/{id}/system`

Sets the system mode:

```json
{"system": "heat"}
```

Allowed values are `"heat"`, `"cool"`, and `"off"`.

`POST /api/devices/{id}/fan`

Sets the fan mode:

```json
{"fan": "auto"}
```

Allowed values are `"auto"`, `"on"`, and `"circulate"`.
