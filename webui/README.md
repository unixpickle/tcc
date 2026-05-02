# webui

This directory contains a standalone web server for controlling all thermostats
from a single page.

```sh
TCC_USERNAME=email@example.com TCC_PASSWORD=password go run ./webui
```

Pass an optional single path segment to hide the app and API under that root:

```sh
TCC_USERNAME=email@example.com TCC_PASSWORD=password go run ./webui -root my-secret-root
```

The server listens on `:8080` by default. Use `-addr` to override it:

```sh
TCC_USERNAME=email@example.com TCC_PASSWORD=password go run ./webui -addr :9090
```

The browser UI is served from `/`, or from `/{root}/` when a root is passed.
JSON endpoints are rooted the same way:

- `GET /api/devices`
- `GET /api/devices/{id}`
- `POST /api/devices/{id}/temperature` with `{"temperature": 70}`
- `POST /api/devices/{id}/system` with `{"system": "heat" | "cool" | "off"}`
- `POST /api/devices/{id}/fan` with `{"fan": "auto" | "on" | "circulate"}`

All control endpoints submit permanent holds.
