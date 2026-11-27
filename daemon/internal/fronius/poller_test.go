package fronius

import (
	"errors"
	"sort"
	"strings"
	"testing"
)

// fakeAPI zaehlt die Aufrufe je Endpunkt und liefert feste Testdaten
// (1 Wechselrichter, 1 Speicher, 1 Zaehler). err[name] != nil laesst den
// jeweiligen Endpunkt fehlschlagen.
type fakeAPI struct {
	calls map[string]int
	err   map[string]error
}

func newFakeAPI() *fakeAPI {
	return &fakeAPI{calls: map[string]int{}, err: map[string]error{}}
}

func (f *fakeAPI) hit(name string) error {
	f.calls[name]++
	return f.err[name]
}

func (f *fakeAPI) GetActiveDeviceInfo() (*ActiveDeviceInfo, error) {
	if err := f.hit("discovery"); err != nil {
		return nil, err
	}
	return &ActiveDeviceInfo{
		Inverter: map[string]DeviceRef{"1": {}},
		Storage:  map[string]DeviceRef{"0": {}},
		Meter:    map[string]DeviceRef{"0": {}},
	}, nil
}

func (f *fakeAPI) GetCommonInverterData(string) (*CommonInverterData, error) {
	if err := f.hit("common"); err != nil {
		return nil, err
	}
	return &CommonInverterData{IDC: 1.23456, UDC: 612.34}, nil
}

func (f *fakeAPI) Get3PInverterData(string) (*ThreePInverterData, error) {
	if err := f.hit("3p"); err != nil {
		return nil, err
	}
	return &ThreePInverterData{}, nil
}

func (f *fakeAPI) GetStorageRealtimeData() (map[string]StorageController, error) {
	if err := f.hit("storage"); err != nil {
		return nil, err
	}
	return map[string]StorageController{"0": {TemperatureCell: 21.55}}, nil
}

func (f *fakeAPI) GetMeterRealtimeData() (map[string]MeterData, error) {
	if err := f.hit("meter"); err != nil {
		return nil, err
	}
	return map[string]MeterData{"0": {}}, nil
}

func (f *fakeAPI) GetPowerFlowRealtimeData() (*PowerFlow, error) {
	if err := f.hit("powerflow"); err != nil {
		return nil, err
	}
	soc := 55.0
	return &PowerFlow{Inverters: map[string]InverterFlow{"1": {SOC: &soc}}}, nil
}

func topicCategories(kv []KV) []string {
	seen := map[string]bool{}
	for _, e := range kv {
		c, _, _ := strings.Cut(e.Topic, "/")
		seen[c] = true
	}
	out := make([]string, 0, len(seen))
	for c := range seen {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

func TestPollAllCategoriesWhenNil(t *testing.T) {
	f := newFakeAPI()
	p := newPoller(f, nil)
	kv := p.Poll()
	for _, ep := range []string{"discovery", "common", "3p", "storage", "meter", "powerflow"} {
		if f.calls[ep] != 1 {
			t.Errorf("%s: %d Aufrufe, erwartet 1", ep, f.calls[ep])
		}
	}
	if got := strings.Join(topicCategories(kv), ","); got != "inverter,meter,site,storage" {
		t.Errorf("Kategorien = %s", got)
	}
}

func TestPollSkipsDisabledCategories(t *testing.T) {
	f := newFakeAPI()
	p := newPoller(f, []string{"inverter", "storage"})
	p.Poll()
	if f.calls["meter"] != 0 {
		t.Errorf("meter abgefragt, obwohl abgewaehlt")
	}
	// PowerFlow wird fuer inverter/<id>/state/soc weiterhin gebraucht.
	if f.calls["powerflow"] != 1 {
		t.Errorf("powerflow: %d Aufrufe, erwartet 1", f.calls["powerflow"])
	}
}

func TestPollOnlyStorage(t *testing.T) {
	f := newFakeAPI()
	p := newPoller(f, []string{"storage"})
	p.Poll()
	for _, ep := range []string{"common", "3p", "meter", "powerflow"} {
		if f.calls[ep] != 0 {
			t.Errorf("%s abgefragt, obwohl nur storage aktiv", ep)
		}
	}
	if f.calls["storage"] != 1 {
		t.Errorf("storage nicht abgefragt")
	}
}

func TestPollOnlySiteSkipsDiscovery(t *testing.T) {
	f := newFakeAPI()
	p := newPoller(f, []string{"site"})
	p.Poll()
	if f.calls["discovery"] != 0 {
		t.Errorf("Discovery laeuft, obwohl keine geraetebezogene Kategorie aktiv ist")
	}
	if f.calls["powerflow"] != 1 {
		t.Errorf("powerflow nicht abgefragt")
	}
}

func TestPollEmptyListFetchesNothing(t *testing.T) {
	f := newFakeAPI()
	p := newPoller(f, []string{})
	if kv := p.Poll(); len(kv) != 0 {
		t.Errorf("leere Positivliste lieferte %d Werte", len(kv))
	}
	for ep, n := range f.calls {
		t.Errorf("%s: %d Aufrufe, erwartet 0", ep, n)
	}
}

var errTest = errors.New("test")

func TestDecimalsFor(t *testing.T) {
	cases := map[string]int{
		"inverter/1/state/pac":                0,
		"inverter/1/state/total_energy":       0,
		"inverter/1/state/udc":                1,
		"inverter/1/state/uac_l2":             1,
		"inverter/1/state/idc":                2,
		"inverter/1/state/iac_l1":             2,
		"inverter/1/state/fac":                2,
		"inverter/1/state/soc":                1,
		"storage/0/state/temperature":         1,
		"storage/0/state/current_dc":          2,
		"storage/0/state/voltage_dc":          1,
		"storage/0/state/capacity_max":        0,
		"meter/0/state/power_l1":              0,
		"meter/0/state/power_factor_sum":      2,
		"meter/0/state/energy_consumed":       0,
		"meter/0/state/frequency":             2,
		"site/state/p_grid":                   0,
		"site/state/rel_selfconsumption":      1,
		"storage/0/state/status_battery_cell": 0,
	}
	for topic, want := range cases {
		if got := decimalsFor(topic); got != want {
			t.Errorf("decimalsFor(%s) = %d, erwartet %d", topic, got, want)
		}
	}
}

func TestPollRoundsValues(t *testing.T) {
	p := newPoller(newFakeAPI(), []string{"inverter", "storage"})
	got := map[string]string{}
	for _, e := range p.Poll() {
		got[e.Topic] = e.Value
	}
	want := map[string]string{
		"inverter/1/state/idc":        "1.23",
		"inverter/1/state/udc":        "612.3",
		"storage/0/state/temperature": "21.6",
		"inverter/1/state/soc":        "55.0",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, erwartet %q", k, got[k], v)
		}
	}
}
