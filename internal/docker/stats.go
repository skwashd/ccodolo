package docker

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Stats is one resource-usage sample for a container.
type Stats struct {
	CPUPct   float64 // percent of one core; exceeds 100 on multi-core use
	MemUsed  uint64
	MemLimit uint64
	NetRx    uint64
	NetTx    uint64
	BlkRead  uint64
	BlkWrite uint64
	PIDs     int
}

// StreamDockerStats reports samples for ids through fn until ctx is
// cancelled. It runs one long-lived `docker stats` process, which emits a
// sample per container roughly every second, so the host never spawns a
// process per refresh.
func StreamDockerStats(ctx context.Context, ids []string, fn func(id string, s Stats)) error {
	if len(ids) == 0 {
		<-ctx.Done()
		return nil
	}
	cmd := exec.CommandContext(ctx, "docker", append([]string{"stats", "--format", "{{json .}}"}, ids...)...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("streaming container stats: %w", err)
	}
	sc := bufio.NewScanner(stdout)
	for sc.Scan() {
		if id, s, ok := parseDockerStatsLine(sc.Text()); ok {
			fn(id, s)
		}
	}
	if err := cmd.Wait(); err != nil && ctx.Err() == nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("streaming container stats: %s", msg)
		}
		return fmt.Errorf("streaming container stats: %w", err)
	}
	return nil
}

// AppleSampler polls `container stats`. The CLI streams only in its table
// format, so each poll runs `container stats --format json --no-stream`.
// Depending on the CLI version, that either samples once (no
// cpuUtilization) or samples twice 2s apart; CPU is therefore computed
// from the cumulative cpuUsageUsec between polls whenever the CLI does not
// supply it.
type AppleSampler struct {
	prev map[string]cpuMark
}

type cpuMark struct {
	usec uint64
	at   time.Time
}

// NewAppleSampler returns a sampler with no CPU history.
func NewAppleSampler() *AppleSampler {
	return &AppleSampler{prev: make(map[string]cpuMark)}
}

// Sample takes one sample of ids.
func (a *AppleSampler) Sample(ctx context.Context, ids []string) (map[string]Stats, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	out, err := exec.CommandContext(ctx, "container",
		append([]string{"stats", "--format", "json", "--no-stream"}, ids...)...).Output()
	if err != nil {
		return nil, fmt.Errorf("polling container stats: %w", cliError(err))
	}
	raw, err := parseAppleStats(out)
	if err != nil {
		return nil, err
	}
	return a.apply(raw, time.Now()), nil
}

// apply turns parsed reports into Stats, filling CPU from the previous
// poll's cpuUsageUsec when the report has no cpuUtilization. The first
// poll of a container has nothing to compare against and reports 0.
func (a *AppleSampler) apply(raw map[string]appleReport, now time.Time) map[string]Stats {
	samples := make(map[string]Stats, len(raw))
	for id, r := range raw {
		s := r.Stats
		switch {
		case r.cpuUtilization != nil:
			s.CPUPct = *r.cpuUtilization * 100
		case r.cpuUsageUsec != nil:
			if p, ok := a.prev[id]; ok && *r.cpuUsageUsec >= p.usec && now.After(p.at) {
				elapsed := float64(now.Sub(p.at).Microseconds())
				s.CPUPct = float64(*r.cpuUsageUsec-p.usec) / elapsed * 100
			}
		}
		if r.cpuUsageUsec != nil {
			a.prev[id] = cpuMark{usec: *r.cpuUsageUsec, at: now}
		}
		samples[id] = s
	}
	for id := range a.prev {
		if _, ok := raw[id]; !ok {
			delete(a.prev, id)
		}
	}
	return samples
}

// parseDockerStatsLine parses one line of `docker stats --format '{{json .}}'`.
// In streaming mode docker writes cursor-control sequences before each
// frame, so parsing starts at the first brace. The key is the Container
// field, which echoes the ID passed on the command line.
func parseDockerStatsLine(line string) (string, Stats, bool) {
	i := strings.IndexByte(line, '{')
	if i < 0 {
		return "", Stats{}, false
	}
	var raw struct {
		Container string `json:"Container"`
		CPUPerc   string `json:"CPUPerc"`
		MemUsage  string `json:"MemUsage"`
		NetIO     string `json:"NetIO"`
		BlockIO   string `json:"BlockIO"`
		PIDs      string `json:"PIDs"`
	}
	if err := json.Unmarshal([]byte(line[i:]), &raw); err != nil || raw.Container == "" {
		return "", Stats{}, false
	}
	var s Stats
	s.CPUPct, _ = strconv.ParseFloat(strings.TrimSuffix(raw.CPUPerc, "%"), 64)
	s.MemUsed, s.MemLimit = parseSizePair(raw.MemUsage)
	s.NetRx, s.NetTx = parseSizePair(raw.NetIO)
	s.BlkRead, s.BlkWrite = parseSizePair(raw.BlockIO)
	s.PIDs, _ = strconv.Atoi(raw.PIDs)
	return raw.Container, s, true
}

// appleReport is one StatsReport from `container stats --format json`. The
// CPU fields are optional: cpuUtilization (a fraction of one core) is
// present only when the CLI took two samples.
type appleReport struct {
	Stats
	cpuUsageUsec   *uint64
	cpuUtilization *float64
}

// parseAppleStats parses `container stats --format json`.
func parseAppleStats(out []byte) (map[string]appleReport, error) {
	var raw []struct {
		ID               string   `json:"id"`
		CPUUsageUsec     *uint64  `json:"cpuUsageUsec"`
		CPUUtilization   *float64 `json:"cpuUtilization"`
		MemoryUsageBytes uint64   `json:"memoryUsageBytes"`
		MemoryLimitBytes uint64   `json:"memoryLimitBytes"`
		NetworkRxBytes   uint64   `json:"networkRxBytes"`
		NetworkTxBytes   uint64   `json:"networkTxBytes"`
		BlockReadBytes   uint64   `json:"blockReadBytes"`
		BlockWriteBytes  uint64   `json:"blockWriteBytes"`
		NumProcesses     int      `json:"numProcesses"`
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return nil, nil
	}
	if err := json.Unmarshal([]byte(trimmed), &raw); err != nil {
		return nil, fmt.Errorf("parsing container stats: %w", err)
	}
	reports := make(map[string]appleReport, len(raw))
	for _, r := range raw {
		reports[r.ID] = appleReport{
			Stats: Stats{
				MemUsed:  r.MemoryUsageBytes,
				MemLimit: r.MemoryLimitBytes,
				NetRx:    r.NetworkRxBytes,
				NetTx:    r.NetworkTxBytes,
				BlkRead:  r.BlockReadBytes,
				BlkWrite: r.BlockWriteBytes,
				PIDs:     r.NumProcesses,
			},
			cpuUsageUsec:   r.CPUUsageUsec,
			cpuUtilization: r.CPUUtilization,
		}
	}
	return reports, nil
}

// parseSizePair parses docker's "used / limit" or "in / out" columns.
func parseSizePair(s string) (uint64, uint64) {
	a, b, _ := strings.Cut(s, "/")
	x, _ := parseSize(a)
	y, _ := parseSize(b)
	return x, y
}

// sizeUnits covers both unit families docker prints: decimal (kB, MB) for
// network and block I/O, binary (KiB, MiB) for memory.
var sizeUnits = map[string]float64{
	"b":  1,
	"kb": 1e3, "mb": 1e6, "gb": 1e9, "tb": 1e12, "pb": 1e15,
	"kib": 1 << 10, "mib": 1 << 20, "gib": 1 << 30, "tib": 1 << 40, "pib": 1 << 50,
}

// parseSize parses a docker-formatted size such as "1.5MiB" or "3kB".
func parseSize(s string) (uint64, error) {
	s = strings.TrimSpace(s)
	i := strings.IndexFunc(s, func(r rune) bool {
		return (r < '0' || r > '9') && r != '.'
	})
	if i <= 0 {
		return 0, fmt.Errorf("invalid size %q", s)
	}
	n, err := strconv.ParseFloat(s[:i], 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size %q: %w", s, err)
	}
	mult, ok := sizeUnits[strings.ToLower(strings.TrimSpace(s[i:]))]
	if !ok {
		return 0, fmt.Errorf("invalid size unit in %q", s)
	}
	return uint64(math.Round(n * mult)), nil
}
