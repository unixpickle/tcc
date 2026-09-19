package thermostat

import (
	"fmt"
	"sort"
	"strings"
)

// Collection combines providers, prefixing their IDs with the map key and ':'.
// Keys must be nonempty and contain no colons. Do not mutate the map after use.
type Collection map[string]Backend

var _ Backend = Collection{}

func (c Collection) Devices() ([]Device, error) {
	names := make([]string, 0, len(c))
	for name := range c {
		names = append(names, name)
	}
	sort.Strings(names)
	result := []Device{}
	for _, name := range names {
		devices, err := c[name].Devices()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		for _, d := range devices {
			d.ID = name + ":" + d.ID
			result = append(result, d)
		}
	}
	return result, nil
}

func (c Collection) resolve(id string) (Backend, string, error) {
	name, local, ok := strings.Cut(id, ":")
	backend := c[name]
	if !ok || local == "" || backend == nil {
		return nil, "", ErrNotFound
	}
	return backend, local, nil
}

func (c Collection) Device(id string) (Device, error) {
	b, local, err := c.resolve(id)
	if err != nil {
		return Device{}, err
	}
	d, err := b.Device(local)
	if err != nil {
		return Device{}, err
	}
	d.ID = id
	return d, nil
}

func (c Collection) SetTemperature(id string, temperature float64, system string) error {
	b, local, err := c.resolve(id)
	if err != nil {
		return err
	}
	return b.SetTemperature(local, temperature, system)
}

func (c Collection) SetSystem(id, system string) error {
	b, local, err := c.resolve(id)
	if err != nil {
		return err
	}
	return b.SetSystem(local, system)
}

func (c Collection) SetFan(id, fan string) error {
	b, local, err := c.resolve(id)
	if err != nil {
		return err
	}
	return b.SetFan(local, fan)
}
