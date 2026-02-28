package hardware

import (
	"testing"
)

func TestDetectHardware(t *testing.T) {
	hw, err := DetectHardware()
	if err != nil {
		t.Fatalf("DetectHardware() returned error: %v", err)
	}
	if hw == nil {
		t.Fatal("DetectHardware() returned nil")
	}

	// CPU cores should always be at least 1.
	if hw.CPUCores < 1 {
		t.Errorf("CPUCores = %d, want >= 1", hw.CPUCores)
	}

	// CPU name should not be empty.
	if hw.CPUName == "" {
		t.Error("CPUName is empty")
	}

	// Total memory should be positive on any real system.
	if hw.TotalMemoryGB <= 0 {
		t.Errorf("TotalMemoryGB = %f, want > 0", hw.TotalMemoryGB)
	}

	t.Logf("Detected: CPU=%q, Cores=%d, TotalMem=%.1f GB, FreeMem=%.1f GB, GPUs=%d, NUMA=%d, DGX=%v",
		hw.CPUName, hw.CPUCores, hw.TotalMemoryGB, hw.FreeMemoryGB,
		len(hw.GPUs), hw.NUMANodes, hw.IsDGXSpark)
}

func TestParseNvidiaSMILine(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		want    GPUInfo
		wantErr bool
	}{
		{
			name: "standard GPU with compute capability",
			line: "0, NVIDIA GeForce RTX 4090, 24564, 8.9",
			want: GPUInfo{
				Index:    0,
				Name:     "NVIDIA GeForce RTX 4090",
				MemoryGB: 24564.0 / 1024.0,
				Compute:  "sm_89",
			},
		},
		{
			name: "GB10 on DGX Spark",
			line: "0, NVIDIA GB10, 131072, 10.0",
			want: GPUInfo{
				Index:    0,
				Name:     "NVIDIA GB10",
				MemoryGB: 131072.0 / 1024.0,
				Compute:  "sm_100",
			},
		},
		{
			name: "multi-GPU second index",
			line: "1, NVIDIA A100-SXM4-80GB, 81920, 8.0",
			want: GPUInfo{
				Index:    1,
				Name:     "NVIDIA A100-SXM4-80GB",
				MemoryGB: 81920.0 / 1024.0,
				Compute:  "sm_80",
			},
		},
		{
			name: "three fields only (no compute)",
			line: "0, NVIDIA Tesla T4, 15360",
			want: GPUInfo{
				Index:    0,
				Name:     "NVIDIA Tesla T4",
				MemoryGB: 15360.0 / 1024.0,
			},
		},
		{
			name:    "too few fields",
			line:    "0, NVIDIA",
			wantErr: true,
		},
		{
			name:    "empty line",
			line:    "",
			wantErr: true,
		},
		{
			name:    "invalid index",
			line:    "abc, NVIDIA GPU, 8192",
			wantErr: true,
		},
		{
			name:    "invalid memory",
			line:    "0, NVIDIA GPU, notanumber",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseNvidiaSMILine(tt.line)
			if tt.wantErr {
				if err == nil {
					t.Errorf("parseNvidiaSMILine(%q) expected error, got %+v", tt.line, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseNvidiaSMILine(%q) error: %v", tt.line, err)
			}
			if got.Index != tt.want.Index {
				t.Errorf("Index = %d, want %d", got.Index, tt.want.Index)
			}
			if got.Name != tt.want.Name {
				t.Errorf("Name = %q, want %q", got.Name, tt.want.Name)
			}
			if !floatClose(got.MemoryGB, tt.want.MemoryGB, 0.01) {
				t.Errorf("MemoryGB = %f, want %f", got.MemoryGB, tt.want.MemoryGB)
			}
			if got.Compute != tt.want.Compute {
				t.Errorf("Compute = %q, want %q", got.Compute, tt.want.Compute)
			}
		})
	}
}

func TestFormatComputeCapability(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"8.9", "sm_89"},
		{"10.0", "sm_100"},
		{"8.0", "sm_80"},
		{"7.5", "sm_75"},
	}
	for _, tt := range tests {
		got := formatComputeCapability(tt.input)
		if got != tt.want {
			t.Errorf("formatComputeCapability(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestParseMemInfoValue(t *testing.T) {
	tests := []struct {
		input string
		want  int64
	}{
		{"MemTotal:       32661924 kB", 32661924},
		{"MemAvailable:   24512340 kB", 24512340},
		{"MemFree:         1234567 kB", 1234567},
		{"BadLine", 0},
		{"NoValue:", 0},
	}
	for _, tt := range tests {
		got := parseMemInfoValue(tt.input)
		if got != tt.want {
			t.Errorf("parseMemInfoValue(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestParseVMStatValue(t *testing.T) {
	tests := []struct {
		input string
		want  int64
	}{
		{"Pages free:                             12345.", 12345},
		{"Pages inactive:                          67890.", 67890},
		{"Pages active:                           100000.", 100000},
		{"BadLine", 0},
		{"NoValue:", 0},
	}
	for _, tt := range tests {
		got := parseVMStatValue(tt.input)
		if got != tt.want {
			t.Errorf("parseVMStatValue(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestIsDGXSpark(t *testing.T) {
	tests := []struct {
		name string
		hw   *HardwareInfo
		want bool
	}{
		{
			name: "nil hardware",
			hw:   nil,
			want: false,
		},
		{
			name: "DGX Spark detected",
			hw: &HardwareInfo{
				CPUName: "NVIDIA Grace",
				GPUs:    []GPUInfo{{Name: "NVIDIA GB10"}},
			},
			want: true,
		},
		{
			name: "Grace CPU without GB10",
			hw: &HardwareInfo{
				CPUName: "NVIDIA Grace",
				GPUs:    []GPUInfo{{Name: "NVIDIA A100"}},
			},
			want: false,
		},
		{
			name: "GB10 GPU without Grace",
			hw: &HardwareInfo{
				CPUName: "Intel Core i9",
				GPUs:    []GPUInfo{{Name: "NVIDIA GB10"}},
			},
			want: false,
		},
		{
			name: "no GPUs",
			hw: &HardwareInfo{
				CPUName: "NVIDIA Grace",
				GPUs:    nil,
			},
			want: false,
		},
		{
			name: "generic hardware",
			hw: &HardwareInfo{
				CPUName: "Intel Core i7-13700K",
				GPUs:    []GPUInfo{{Name: "NVIDIA GeForce RTX 4090"}},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsDGXSpark(tt.hw)
			if got != tt.want {
				t.Errorf("IsDGXSpark() = %v, want %v", got, tt.want)
			}
		})
	}
}

// floatClose checks whether two float64 values are within epsilon.
func floatClose(a, b, epsilon float64) bool {
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff < epsilon
}
