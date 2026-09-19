# tcc

Control Total Connect Comfort and Resideo app thermostats from one web UI.

## Run

Set either complete credential pair, or both, then start the server:

```sh
export TCC_USERNAME=email@example.com
export TCC_PASSWORD=your-tcc-password
export RESIDEO_USERNAME=email@example.com
export RESIDEO_PASSWORD=your-resideo-password
go run ./webui
```

Omit both variables for a provider you do not use. A missing half of a pair is
an error. The server initializes every configured provider and reports login
failures at startup. Credentials and Resideo tokens remain in memory.

The server listens on `:8080`. Use `-addr :9090` to change it, or
`-root my-secret-root` to serve the UI and API below `/my-secret-root/`.

## Packages

- `thermostat` defines `Backend`, device state, control validation, and a
  `Collection` that combines providers with distinct device IDs.
- `tcc` contains the legacy TCC client and its `NewBackend` adapter, including
  session renewal and permanent-hold controls. The previous root-package API
  is now imported from `github.com/unixpickle/tcc/tcc`.
- `resideo` implements the Resideo app's B2C login with PKCE, token refresh,
  location discovery, CHIL controls, and Titan controls for Focus Pro devices.
- `webui` wires credentials to providers. Its HTTP handlers and browser UI use
  only the common thermostat API.

Both providers implement the same interface:

```go
Devices() ([]thermostat.Device, error)
Device(id string) (thermostat.Device, error)
SetTemperature(id string, temperature float64, system string) error
SetSystem(id, system string) error
SetFan(id, fan string) error
```

Resideo uses the app's private APIs, based on the 6.21.0 Android app. Jasper,
HoneyBadger, Blackbeard, FlyCatcher, and Storm use CHIL; Focus Pro uses Titan
and its internal device ID. Jasper uses the app's default CHIL routing.
Accounts requiring an interactive sign-in step or First Alert migration are
not supported by the username/password login. Device families with Celsius
conversion use the app's conversion rules for reads and writes.

## Web API

All responses are JSON. Paths below are relative to the optional `-root`.
Device IDs are opaque **strings** and must be URL-encoded when used in paths:
for example, `tcc:1001` or `resideo:123:LCC-0123456789AB`. This replaces the
previous numeric TCC IDs.

`GET /api/devices` returns `{"devices": [...]}`. `GET /api/devices/{id}` returns
one device. Each device reports its name, displayed units and temperature,
heat/cool setpoints, active setpoint when applicable, system/fan modes and
supported options, temperature limits and step, and runtime/offline state.
Unavailable temperature/humidity values are omitted. Empty fan options mean
fan control is unavailable; `runtimeAvailable: false` means runtime is unknown.

`POST /api/devices/{id}/temperature`

```json
{"temperature": 70, "system": "cool"}
```

Temperatures are in the device's displayed units. Omit `system` to use the
current mode, or specify `heat`, `cool`, or supported `emergencyheat`. The
provider validates limits and adjusts the paired setpoint when needed to
preserve the deadband. Auto/off modes require an explicit side; the browser
lets you select heat or cool first.

`POST /api/devices/{id}/system`

```json
{"system": "cool"}
```

`POST /api/devices/{id}/fan`

```json
{"fan": "auto"}
```

Choose from the device's `systemOptions` and `fanOptions`. Controls request
permanent holds where supported. Titan system changes pause the schedule.
Resideo operations that need multiple cloud commands stop on the first failure;
a preceding command may already have taken effect. After a successful write,
the server reads the device again. Cloud state can take several seconds to
reflect a command; the browser also refreshes periodically.
