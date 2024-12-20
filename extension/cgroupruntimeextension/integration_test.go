// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0
//go:build integration && linux
// +build integration,linux

// Privileged access is required to set cgroup's memory and cpu max values

package cgroupruntimeextension // import "github.com/open-telemetry/opentelemetry-collector-contrib/extension/cgroupruntimeextension"

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"testing"

	"github.com/containerd/cgroups/v3/cgroup2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/extension/extensiontest"
	"golang.org/x/sys/unix"
)

const (
	defaultCgroup2Path = "/sys/fs/cgroup"
)

// checkCgroupSystem skips the test if is not run in a cgroupv2 system
func checkCgroupSystem(tb testing.TB) {
	var st unix.Statfs_t
	err := unix.Statfs(defaultCgroup2Path, &st)
	if err != nil {
		tb.Skip("cannot statfs cgroup root")
	}

	isUnified := st.Type == unix.CGROUP2_SUPER_MAGIC
	if !isUnified {
		tb.Skip("System running in hybrid or cgroupv1 mode")
	}
}

// cgroupMaxCpu returns the CPU max definition for a given cgroup slice path
// File format: cpu_quote cpu_period
func cgroupMaxCpu(filename string) (quota int64, period uint64, err error) {
	out, err := os.ReadFile(filepath.Join(defaultCgroup2Path, filename, "cpu.max"))
	if err != nil {
		return 0, 0, err
	}
	values := strings.Split(strings.TrimSpace(string(out)), " ")
	if values[0] == "max" {
		quota = math.MaxInt64
	} else {
		quota, _ = strconv.ParseInt(values[0], 10, 64)
	}
	period, _ = strconv.ParseUint(values[1], 10, 64)
	return quota, period, err
}

func startMockECSServer() *httptest.Server {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := map[string]interface{}{
			"DockerID": "container-id",
			"Limits": map[string]interface{}{
				"CPU": 2.0,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	})

	return httptest.NewServer(handler)
}

func TestCgroupV2SudoIntegration(t *testing.T) {
	checkCgroupSystem(t)
	pointerInt64 := func(val int64) *int64 {
		return &val
	}
	pointerUint64 := func(uval uint64) *uint64 {
		return &uval
	}

	tests := []struct {
		name string
		// nil CPU quota == "max" cgroup string value
		cgroupCpuQuota     *int64
		cgroupCpuPeriod    uint64
		cgroupMaxMemory    int64
		config             *Config
		expectedGoMaxProcs int
		expectedGoMemLimit int64
		setECSMetadataURI  bool
	}{
		{
			name:            "90% the max cgroup memory and 12 GOMAXPROCS",
			cgroupCpuQuota:  pointerInt64(100000),
			cgroupCpuPeriod: 8000,
			// 128 Mb
			cgroupMaxMemory: 134217728,
			config: &Config{
				GoMaxProcs: GoMaxProcsConfig{
					Enabled: true,
				},
				GoMemLimit: GoMemLimitConfig{
					Enabled: true,
					Ratio:   0.9,
				},
			},
			// 100000 / 8000
			expectedGoMaxProcs: 12,
			// 134217728 * 0.9
			expectedGoMemLimit: 120795955,
		},
		{
			name:            "50% of the max cgroup memory and 1 GOMAXPROCS",
			cgroupCpuQuota:  pointerInt64(100000),
			cgroupCpuPeriod: 100000,
			// 128 Mb
			cgroupMaxMemory: 134217728,
			config: &Config{
				GoMaxProcs: GoMaxProcsConfig{
					Enabled: true,
				},
				GoMemLimit: GoMemLimitConfig{
					Enabled: true,
					Ratio:   0.5,
				},
			},
			// 100000 / 100000
			expectedGoMaxProcs: 1,
			// 134217728 * 0.5
			expectedGoMemLimit: 67108864,
		},
		{
			name:            "10% of the max cgroup memory, max cpu, default GOMAXPROCS",
			cgroupCpuQuota:  nil,
			cgroupCpuPeriod: 100000,
			// 128 Mb
			cgroupMaxMemory: 134217728,
			config: &Config{
				GoMaxProcs: GoMaxProcsConfig{
					Enabled: true,
				},
				GoMemLimit: GoMemLimitConfig{
					Enabled: true,
					Ratio:   0.1,
				},
			},
			// GOMAXPROCS is set to the value of  `cpu.max / cpu.period`
			// If cpu.max is set to max, GOMAXPROCS should not be
			// modified
			expectedGoMaxProcs: runtime.GOMAXPROCS(-1),
			// 134217728 * 0.1
			expectedGoMemLimit: 13421772,
		},
		{
			name:            "running on AWS ECS with 90% of max cgroup memory and 2 GOMAXPROCS",
			cgroupCpuQuota:  pointerInt64(-1),
			cgroupCpuPeriod: 8000,
			cgroupMaxMemory: 134217728, // 128 MB
			config: &Config{
				GoMaxProcs: GoMaxProcsConfig{
					Enabled: true,
				},
				GoMemLimit: GoMemLimitConfig{
					Enabled: true,
					Ratio:   0.9,
				},
			},
			expectedGoMaxProcs: 22,
			expectedGoMemLimit: 120795955, // 134217728 * 0.9
			setECSMetadataURI:  true,
		},
	}

	cgroupPath, err := cgroup2.PidGroupPath(os.Getpid())
	assert.NoError(t, err)
	manager, err := cgroup2.Load(cgroupPath)
	assert.NoError(t, err)

	stats, err := manager.Stat()
	require.NoError(t, err)

	// Startup resource values
	initialMaxMemory := stats.GetMemory().GetUsageLimit()
	memoryCgroupCleanUp := func() {
		err = manager.Update(&cgroup2.Resources{
			Memory: &cgroup2.Memory{
				Max: pointerInt64(int64(initialMaxMemory)),
			},
		})
		assert.NoError(t, err)
	}

	if initialMaxMemory == math.MaxUint64 {
		// fallback solution to set cgroup's max memory to "max"
		memoryCgroupCleanUp = func() {
			err = os.WriteFile(path.Join(defaultCgroup2Path, cgroupPath, "memory.max"), []byte("max"), 0o600)
			assert.NoError(t, err)
		}
	}

	initialCpuQuota, initialCpuPeriod, err := cgroupMaxCpu(cgroupPath)
	require.NoError(t, err)
	cpuCgroupCleanUp := func() {
		fmt.Println(initialCpuQuota)
		err = manager.Update(&cgroup2.Resources{
			CPU: &cgroup2.CPU{
				Max: cgroup2.NewCPUMax(pointerInt64(initialCpuQuota), pointerUint64(initialCpuPeriod)),
			},
		})
		assert.NoError(t, err)
	}

	if initialCpuQuota == math.MaxInt64 {
		// fallback solution to set cgroup's max cpu to "max"
		cpuCgroupCleanUp = func() {
			err = os.WriteFile(path.Join(defaultCgroup2Path, cgroupPath, "cpu.max"), []byte("max"), 0o600)
			assert.NoError(t, err)
		}
	}

	initialGoMem := debug.SetMemoryLimit(-1)
	initialGoProcs := runtime.GOMAXPROCS(-1)

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// restore startup cgroup initial resource values
			t.Cleanup(func() {
				debug.SetMemoryLimit(initialGoMem)
				runtime.GOMAXPROCS(initialGoProcs)
				memoryCgroupCleanUp()
				cpuCgroupCleanUp()
			})

			err = manager.Update(&cgroup2.Resources{
				Memory: &cgroup2.Memory{
					// Default max memory must be
					// overwritten
					// to automemlimit change the GOMEMLIMIT
					// value
					Max: pointerInt64(test.cgroupMaxMemory),
				},
				CPU: &cgroup2.CPU{
					Max: cgroup2.NewCPUMax(test.cgroupCpuQuota, pointerUint64(test.cgroupCpuPeriod)),
				},
			})
			require.NoError(t, err)

			if test.setECSMetadataURI {
				server := startMockECSServer()
				defer server.Close()
				os.Setenv("ECS_CONTAINER_METADATA_URI_V4", server.URL)
				defer os.Unsetenv("ECS_CONTAINER_METADATA_URI_V4")
			}

			factory := NewFactory()
			ctx := context.Background()
			extension, err := factory.Create(ctx, extensiontest.NewNopSettings(), test.config)
			require.NoError(t, err)

			err = extension.Start(ctx, componenttest.NewNopHost())
			require.NoError(t, err)

			assert.Equal(t, test.expectedGoMaxProcs, runtime.GOMAXPROCS(-1))
			assert.Equal(t, test.expectedGoMemLimit, debug.SetMemoryLimit(-1))
		})
	}
}
