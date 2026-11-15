package docker

import (
	"reflect"
	"testing"
	"time"
)

func TestParseSize(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want uint64
	}{
		{"0B", 0},
		{"512B", 512},
		{"3kB", 3000},
		{"1.5MB", 1500000},
		{"2GB", 2000000000},
		{"1KiB", 1024},
		{"1.5MiB", 1572864},
		{"7.6GiB", 8160437862},
		{" 4GiB ", 4 << 30},
	} {
		got, err := parseSize(tc.in)
		if err != nil {
			t.Errorf("parseSize(%q): unexpected error: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parseSize(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
	for _, bad := range []string{"", "--", "MiB", "12XB"} {
		if _, err := parseSize(bad); err == nil {
			t.Errorf("parseSize(%q): expected an error", bad)
		}
	}
}

func TestParseDockerStatsLine(t *testing.T) {
	// Streaming frames start with cursor-control sequences.
	line := "\x1b[2J\x1b[H" + `{"BlockIO":"4.1MB / 8.2kB","CPUPerc":"112.50%","Container":"abc123def456","ID":"abc123def456","MemPerc":"10.00%","MemUsage":"1.5MiB / 4GiB","Name":"ccodolo-p-w-1","NetIO":"3kB / 1MB","PIDs":"17"}`
	id, s, ok := parseDockerStatsLine(line)
	if !ok {
		t.Fatal("expected the line to parse")
	}
	if id != "abc123def456" {
		t.Errorf("id = %q", id)
	}
	want := Stats{
		CPUPct: 112.5, MemUsed: 1572864, MemLimit: 4 << 30,
		NetRx: 3000, NetTx: 1000000, BlkRead: 4100000, BlkWrite: 8200, PIDs: 17,
	}
	if !reflect.DeepEqual(s, want) {
		t.Errorf("stats = %+v, want %+v", s, want)
	}

	// A stopped container reports "--" everywhere; it still parses, as zeros.
	_, s, ok = parseDockerStatsLine(`{"Container":"x","CPUPerc":"--","MemUsage":"-- / --","NetIO":"--","BlockIO":"--","PIDs":"--"}`)
	if !ok || s != (Stats{}) {
		t.Errorf("stopped container: ok=%v stats=%+v", ok, s)
	}

	for _, bad := range []string{"", "\x1b[2J\x1b[H", "{not json", `{"CPUPerc":"1%"}`} {
		if _, _, ok := parseDockerStatsLine(bad); ok {
			t.Errorf("parseDockerStatsLine(%q): expected no sample", bad)
		}
	}
}

// appleStatsNoStream is real `container stats --format json --no-stream`
// output: a single sample, so no cpuUtilization.
const appleStatsNoStream = `[{"blockReadBytes":5185912832,"blockWriteBytes":3444428800,"cpuUsageUsec":51561398,"id":"buildkit","memoryLimitBytes":2147483648,"memoryUsageBytes":1422413824,"networkRxBytes":188940312,"networkTxBytes":3615716,"numProcesses":21},{"blockReadBytes":711389184,"blockWriteBytes":696315904,"cpuUsageUsec":252569598,"id":"ccodolo-ccodolo-ccodolo-202611011223","memoryLimitBytes":4294967296,"memoryUsageBytes":1337475072,"networkRxBytes":11979585,"networkTxBytes":6281653,"numProcesses":35}]`

func TestParseAppleStats(t *testing.T) {
	got, err := parseAppleStats([]byte(appleStatsNoStream))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r, ok := got["ccodolo-ccodolo-ccodolo-202611011223"]
	if !ok || len(got) != 2 {
		t.Fatalf("expected both reports keyed by id, got %v", got)
	}
	want := Stats{MemUsed: 1337475072, MemLimit: 4294967296, NetRx: 11979585, NetTx: 6281653,
		BlkRead: 711389184, BlkWrite: 696315904, PIDs: 35}
	if r.Stats != want {
		t.Errorf("stats = %+v, want %+v", r.Stats, want)
	}
	if r.cpuUsageUsec == nil || *r.cpuUsageUsec != 252569598 || r.cpuUtilization != nil {
		t.Errorf("cpu fields: usec %v utilization %v", r.cpuUsageUsec, r.cpuUtilization)
	}

	if got, err := parseAppleStats([]byte("  \n")); err != nil || len(got) != 0 {
		t.Errorf("empty output: got %v, %v", got, err)
	}
	if _, err := parseAppleStats([]byte("{")); err == nil {
		t.Error("expected an error for invalid JSON")
	}
}

func TestAppleSamplerCPU(t *testing.T) {
	usec := func(v uint64) *uint64 { return &v }
	util := 1.25
	a := NewAppleSampler()
	t0 := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

	// First poll: nothing to compare against.
	got := a.apply(map[string]appleReport{"c": {cpuUsageUsec: usec(1_000_000)}}, t0)
	if got["c"].CPUPct != 0 {
		t.Errorf("first poll CPU = %v, want 0", got["c"].CPUPct)
	}
	// 1.5 CPU-seconds over 1s of wall time is 150%.
	got = a.apply(map[string]appleReport{"c": {cpuUsageUsec: usec(2_500_000)}}, t0.Add(time.Second))
	if got["c"].CPUPct != 150 {
		t.Errorf("delta CPU = %v, want 150", got["c"].CPUPct)
	}
	// A CLI that sampled twice supplies cpuUtilization; it wins.
	got = a.apply(map[string]appleReport{"c": {cpuUsageUsec: usec(2_600_000), cpuUtilization: &util}}, t0.Add(2*time.Second))
	if got["c"].CPUPct != 125 {
		t.Errorf("reported CPU = %v, want 125", got["c"].CPUPct)
	}
	// A restarted container's counter goes backwards: no bogus spike.
	got = a.apply(map[string]appleReport{"c": {cpuUsageUsec: usec(10)}}, t0.Add(3*time.Second))
	if got["c"].CPUPct != 0 {
		t.Errorf("counter reset CPU = %v, want 0", got["c"].CPUPct)
	}
	// Containers that disappear are forgotten.
	a.apply(map[string]appleReport{}, t0.Add(4*time.Second))
	if len(a.prev) != 0 {
		t.Errorf("prev = %v, want empty", a.prev)
	}
}
