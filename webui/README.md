# webui

A standalone server for Total Connect Comfort and Resideo app thermostats.
Set `TCC_USERNAME` / `TCC_PASSWORD`, `RESIDEO_USERNAME` / `RESIDEO_PASSWORD`,
or both complete pairs, then run:

```sh
go run ./webui
```

Use `-addr :9090` to change the listen address, and `-root my-secret-root` to
serve the UI and API below `/my-secret-root/`.

The HTTP handlers accept a `thermostat.Backend`; provider setup is in `main.go`.
Device IDs are opaque strings. See the [project README](../README.md) for the
shared API, credential setup, and provider behavior.
