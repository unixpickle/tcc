const devicesEl = document.querySelector("#devices");
const statusEl = document.querySelector("#status");
const refreshButton = document.querySelector("#refresh");
const template = document.querySelector("#device-template");
const cards = new Map();
const temperatureStates = new Map();

const POLL_INTERVAL_MS = 90 * 1000;
const TEMPERATURE_DEBOUNCE_MS = 2 * 1000;
const apiRoot = new URL("api/", window.location.href).pathname;

refreshButton.addEventListener("click", () => loadDevices());
window.addEventListener("focus", () => loadDevices());
document.addEventListener("visibilitychange", () => {
  if (!document.hidden) {
    loadDevices();
  }
});

setInterval(() => {
  if (!document.hidden) {
    loadDevices({ quiet: true });
  }
}, POLL_INTERVAL_MS);

loadDevices();

async function loadDevices(options = {}) {
  if (!options.quiet) {
    setStatus("Refreshing...");
  }
  try {
    const data = await requestJSON(apiPath("devices"));
    const seen = new Set();
    data.devices.forEach((device) => {
      seen.add(String(device.id));
      renderDevice(device);
    });
    for (const [id, card] of cards) {
      if (!seen.has(id)) {
        card.remove();
        cards.delete(id);
        temperatureStates.delete(id);
      }
    }
    setStatus(`Updated ${new Date().toLocaleTimeString()}`);
  } catch (error) {
    setStatus(error.message, true);
  }
}

function renderDevice(device) {
  const id = String(device.id);
  const temperatureState = getTemperatureState(id);
  let card = cards.get(id);
  if (!card) {
    card = template.content.firstElementChild.cloneNode(true);
    cards.set(id, card);
    devicesEl.append(card);
    wireCard(card, id);
  }
  card.dataset.deviceId = id;
  card.dataset.system = device.system;
  card.querySelector("h2").textContent = device.name || `Device ${device.id}`;
  const units = device.displayedUnits || "F";
  const humidity = device.humidity == null ? "" : ` · ${Math.round(device.humidity)}% humidity`;
  card.querySelector(".meta").textContent = `${label(device.system)}${device.fanOptions?.length ? ` · fan ${label(device.fan)}` : ""}${humidity}`;
  card.querySelector(".current-temp").textContent = formatTemp(
    device.displayTemperature ?? device.temperature,
    units,
  );
  const runState = card.querySelector(".run-state");
  runState.textContent = device.offline ? "Offline" : !device.runtimeAvailable ? "Unknown" : device.equipmentRunning ? "Running" : "Idle";
  runState.classList.toggle("running", !device.offline && device.runtimeAvailable && device.equipmentRunning);
  const input = card.querySelector(".setpoint");
  if (temperatureState.pending != null) {
    input.value = String(temperatureState.pending);
  } else if (temperatureState.savingTemperature != null) {
    input.value = String(temperatureState.savingTemperature);
  } else if (document.activeElement !== input) {
    input.value = device.activeSetpoint == null ? "" : device.activeSetpoint;
  }
  input.min = activeRange(device).min ?? "";
  input.max = activeRange(device).max ?? "";
  input.step = device.temperatureStep || 1;
  input.disabled = !["heat", "cool", "emergencyheat"].includes(device.system) || !device.setpointAllowed || device.offline;
  card.querySelector(".temp-down").disabled = input.disabled;
  card.querySelector(".temp-up").disabled = input.disabled;
  fillSelect(card.querySelector(".system"), device.systemOptions, device.system);
  fillSelect(card.querySelector(".fan"), device.fanOptions, device.fan);
  card.querySelector(".system").disabled = device.offline || !device.systemOptions?.length;
  card.querySelector(".fan").disabled = device.offline || !device.fanOptions?.length;
  if (!temperatureState.saving && temperatureState.pending == null) {
    card.querySelector(".message").textContent = device.offline ? "Device appears offline." : "";
  }
}

function wireCard(card, id) {
  const input = card.querySelector(".setpoint");
  card.querySelector(".temp-down").addEventListener("click", () => adjustTemperature(card, -1));
  card.querySelector(".temp-up").addEventListener("click", () => adjustTemperature(card, 1));
  input.addEventListener("change", () => setTemperature(card, Number(input.value)));
  card.querySelector(".system").addEventListener("change", (event) => {
    postControl(card, apiPath(`devices/${encodeURIComponent(id)}/system`), { system: event.target.value });
  });
  card.querySelector(".fan").addEventListener("change", (event) => {
    postControl(card, apiPath(`devices/${encodeURIComponent(id)}/fan`), { fan: event.target.value });
  });
}

function adjustTemperature(card, delta) {
  const input = card.querySelector(".setpoint");
  const step = Number(input.step) || 1;
  const next = Math.round((Number(input.value || 0) + delta * step) * 100) / 100;
  if ((input.min !== "" && next < Number(input.min)) || (input.max !== "" && next > Number(input.max))) return;
  input.value = String(next);
  setTemperature(card, next);
}

function setTemperature(card, temperature) {
  if (!Number.isFinite(temperature)) {
    return;
  }
  const state = getTemperatureState(card.dataset.deviceId);
  state.pending = temperature;
  window.clearTimeout(state.timer);
  state.timer = window.setTimeout(() => {
    state.timer = null;
    if (!state.saving) {
      sendTemperature(card);
    }
  }, TEMPERATURE_DEBOUNCE_MS);
  setCardMessage(card, "Waiting...");
}

async function sendTemperature(card) {
  const state = getTemperatureState(card.dataset.deviceId);
  if (state.pending == null || state.saving) {
    return;
  }
  const temperature = state.pending;
  state.pending = null;
  state.savingTemperature = temperature;
  state.saving = true;
  setCardMessage(card, "Saving...");
  card.classList.add("busy");
  try {
    const device = await requestJSON(apiPath(`devices/${encodeURIComponent(card.dataset.deviceId)}/temperature`), {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ temperature, system: card.dataset.system }),
    });
    state.saving = false;
    state.savingTemperature = null;
    renderDevice(device);
    if (state.pending == null) {
      setCardMessage(card, "Saved.");
    }
  } catch (error) {
    state.saving = false;
    state.savingTemperature = null;
    setCardMessage(card, error.message, true);
    loadDevices({ quiet: true });
  } finally {
    if (state.pending == null) {
      card.classList.remove("busy");
    } else if (state.timer == null) {
      sendTemperature(card);
    } else {
      setCardMessage(card, "Waiting...");
    }
  }
}

async function postControl(card, url, body) {
  setCardMessage(card, "Saving...");
  card.classList.add("busy");
  try {
    const device = await requestJSON(url, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
    renderDevice(device);
    setCardMessage(card, "Saved.");
  } catch (error) {
    setCardMessage(card, error.message, true);
    loadDevices({ quiet: true });
  } finally {
    card.classList.remove("busy");
  }
}

async function requestJSON(url, options) {
  const response = await fetch(url, options);
  const data = await response.json().catch(() => ({}));
  if (!response.ok) {
    throw new Error(data.error || `Request failed with ${response.status}`);
  }
  return data;
}

function fillSelect(select, options, value) {
  const normalized = options && options.length ? options : [value];
  const current = Array.from(select.options).map((option) => option.value).join(",");
  if (current !== normalized.join(",")) {
    select.replaceChildren(...normalized.map((option) => new Option(label(option), option)));
  }
  select.value = value;
}

function activeRange(device) {
  if (["heat", "emergencyheat"].includes(device.system)) {
    return device.heatRange || {};
  }
  if (device.system === "cool") {
    return device.coolRange || {};
  }
  return {};
}

function formatTemp(value, units) {
  return value == null ? "--" : `${Number(value.toFixed(1))}°${units}`;
}

function label(value) {
  if (value === "emergencyheat") return "Emergency heat";
  if (value === "followschedule") return "Follow schedule";
  return value.charAt(0).toUpperCase() + value.slice(1);
}

function apiPath(path) {
  return `${apiRoot}${path.replace(/^\/+/, "")}`;
}

function getTemperatureState(id) {
  let state = temperatureStates.get(id);
  if (!state) {
    state = {
      pending: null,
      timer: null,
      saving: false,
      savingTemperature: null,
    };
    temperatureStates.set(id, state);
  }
  return state;
}

function setStatus(message, isError = false) {
  statusEl.textContent = message;
  statusEl.classList.toggle("error", isError);
}

function setCardMessage(card, message, isError = false) {
  const el = card.querySelector(".message");
  el.textContent = message;
  el.classList.toggle("error", isError);
}
