package hardware

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// kfdNode is a fixture description of one KFD topology node.
type kfdNode struct {
	id    int
	props map[string]int64
	banks []map[string]int64
}

// writeKFDFixture materializes a fake KFD topology so detection can be tested
// without the hardware it describes.
func writeKFDFixture(t *testing.T, nodes []kfdNode) string {
	t.Helper()
	root := t.TempDir()
	for _, n := range nodes {
		dir := filepath.Join(root, strconv.Itoa(n.id))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir node: %v", err)
		}
		writeProps(t, filepath.Join(dir, "properties"), n.props)
		for i, b := range n.banks {
			bdir := filepath.Join(dir, "mem_banks", strconv.Itoa(i))
			if err := os.MkdirAll(bdir, 0o755); err != nil {
				t.Fatalf("mkdir bank: %v", err)
			}
			writeProps(t, filepath.Join(bdir, "properties"), b)
		}
	}
	return root
}

func writeProps(t *testing.T, path string, props map[string]int64) {
	t.Helper()
	var out string
	for k, v := range props {
		out += k + " " + strconv.FormatInt(v, 10) + "\n"
	}
	if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
		t.Fatalf("write props: %v", err)
	}
}

// strixHaloNodes is the topology this box actually publishes: a CPU node with
// no SIMDs, and one gfx1151 agent whose framebuffer heap is the ~62.5 GiB
// unified pool.
func strixHaloNodes() []kfdNode {
	return []kfdNode{
		{id: 0, props: map[string]int64{"simd_count": 0, "gfx_target_version": 0, "vendor_id": 0, "device_id": 0}},
		{
			id: 1,
			props: map[string]int64{
				"simd_count":         80,
				"gfx_target_version": 110501,
				"vendor_id":          4098,
				"device_id":          0x1586,
			},
			banks: []map[string]int64{
				{"heap_type": 1, "size_in_bytes": 67148926976},
			},
		},
	}
}

func TestDetectGPUsAMD_StrixHalo(t *testing.T) {
	gpus := detectGPUsAMDFrom(writeKFDFixture(t, strixHaloNodes()))

	if len(gpus) != 1 {
		t.Fatalf("expected exactly one GPU agent (the CPU node must not count), got %d", len(gpus))
	}
	g := gpus[0]
	if g.Vendor != VendorAMD {
		t.Errorf("vendor = %q, want %q", g.Vendor, VendorAMD)
	}
	if g.Compute != "gfx1151" {
		t.Errorf("compute = %q, want gfx1151", g.Compute)
	}
	if g.Name != "AMD Radeon 8060S Graphics" {
		t.Errorf("name = %q, want the marketing name for device 0x1586", g.Name)
	}
	// 67148926976 bytes is ~62.54 GiB -- the same free-memory figure vLLM
	// reports on this accelerator at startup.
	if g.MemoryGB < 62.4 || g.MemoryGB > 62.7 {
		t.Errorf("memoryGB = %.2f, want ~62.54 (the KFD framebuffer pool)", g.MemoryGB)
	}
}

// The DRM node on an APU reports a 512 MiB BIOS carve-out as "VRAM" while the
// real pool is two orders of magnitude larger. Detection must size against the
// framebuffer heap and ignore heaps it does not serve allocations from,
// otherwise every downstream memory decision under-provisions.
func TestDetectGPUsAMD_IgnoresNonFramebufferHeaps(t *testing.T) {
	nodes := strixHaloNodes()
	nodes[1].banks = []map[string]int64{
		{"heap_type": 0, "size_in_bytes": 137438953472}, // system memory: not ours
		{"heap_type": 1, "size_in_bytes": 67148926976},  // framebuffer: the real pool
		{"heap_type": 4, "size_in_bytes": 65536},        // LDS: not ours
	}
	gpus := detectGPUsAMDFrom(writeKFDFixture(t, nodes))
	if len(gpus) != 1 {
		t.Fatalf("expected one GPU, got %d", len(gpus))
	}
	if gpus[0].MemoryGB < 62.4 || gpus[0].MemoryGB > 62.7 {
		t.Errorf("memoryGB = %.2f, want only the framebuffer heap (~62.54)", gpus[0].MemoryGB)
	}
}

func TestDetectGPUsAMD_CPUOnlyTopology(t *testing.T) {
	nodes := []kfdNode{
		{id: 0, props: map[string]int64{"simd_count": 0, "gfx_target_version": 0}},
	}
	if gpus := detectGPUsAMDFrom(writeKFDFixture(t, nodes)); len(gpus) != 0 {
		t.Errorf("a topology with only a CPU node must report no GPUs, got %d", len(gpus))
	}
}

func TestDetectGPUsAMD_NoTopology(t *testing.T) {
	if gpus := detectGPUsAMDFrom(filepath.Join(t.TempDir(), "absent")); gpus != nil {
		t.Errorf("a box with no amdgpu topology must report nil, not an error path, got %v", gpus)
	}
}

// Node directories are numeric, and ReadDir hands them back lexically. Indices
// must follow numeric order so node 10 does not land between 1 and 2.
func TestDetectGPUsAMD_NumericNodeOrder(t *testing.T) {
	mk := func(id int, dev int64) kfdNode {
		return kfdNode{
			id:    id,
			props: map[string]int64{"simd_count": 80, "gfx_target_version": 110501, "vendor_id": 4098, "device_id": dev},
			banks: []map[string]int64{{"heap_type": 1, "size_in_bytes": 1073741824}},
		}
	}
	nodes := []kfdNode{
		{id: 0, props: map[string]int64{"simd_count": 0, "gfx_target_version": 0}},
		mk(1, 0x1), mk(2, 0x2), mk(10, 0xa),
	}
	gpus := detectGPUsAMDFrom(writeKFDFixture(t, nodes))
	if len(gpus) != 3 {
		t.Fatalf("expected 3 GPU agents, got %d", len(gpus))
	}
	for i, want := range []string{"AMD GPU (gfx1151)", "AMD GPU (gfx1151)", "AMD GPU (gfx1151)"} {
		if gpus[i].Index != i {
			t.Errorf("gpu %d has Index %d; indices must be sequential", i, gpus[i].Index)
		}
		if gpus[i].Name != want {
			t.Errorf("gpu %d name = %q, want %q", i, gpus[i].Name, want)
		}
	}
}

// A non-AMD agent in this topology must not be claimed as an AMD GPU.
func TestDetectGPUsAMD_SkipsForeignVendor(t *testing.T) {
	nodes := strixHaloNodes()
	nodes[1].props["vendor_id"] = 0x10de
	if gpus := detectGPUsAMDFrom(writeKFDFixture(t, nodes)); len(gpus) != 0 {
		t.Errorf("a non-AMD vendor id must be skipped, got %d GPUs", len(gpus))
	}
}

func TestFormatGFXTarget(t *testing.T) {
	// The step is a hex digit, which is the whole reason gfx90a is not
	// "gfx9010". These are the encodings we care about across the AMD line.
	cases := []struct {
		in   int64
		want string
	}{
		{110501, "gfx1151"}, // Strix Halo (this box)
		{110000, "gfx1100"}, // RDNA3 dGPU
		{100300, "gfx1030"}, // RDNA2
		{90402, "gfx942"},   // MI300X
		{90010, "gfx90a"},   // MI200 -- step 10 renders as 'a'
		{0, ""},
		{-1, ""},
	}
	for _, c := range cases {
		if got := formatGFXTarget(c.in); got != c.want {
			t.Errorf("formatGFXTarget(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestAMDGPUName_FallsBackToGFX(t *testing.T) {
	if got := amdGPUName(0xdead, "gfx1201"); got != "AMD GPU (gfx1201)" {
		t.Errorf("unknown device should still name its gfx target, got %q", got)
	}
	if got := amdGPUName(0xdead, ""); got != "AMD GPU" {
		t.Errorf("with no gfx target the name should degrade cleanly, got %q", got)
	}
}
