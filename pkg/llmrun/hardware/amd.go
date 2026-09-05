package hardware

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// AMD GPUs are detected through the kernel's KFD (HSA) topology in sysfs rather
// than by shelling out to rocm-smi, which is the AMD analogue of the nvidia-smi
// probe. The sysfs route is deliberate:
//
//   - It needs no ROCm userspace. On an appliance host the ROCm stack usually
//     lives inside the inference container, so a rocm-smi probe would report "no
//     GPU" on the very box that has one. The amdgpu kernel driver publishes this
//     topology regardless.
//   - It is pure Go with no exec and no cgo, which the repo requires.
//   - It reports the *device* rather than a vendor tool's opinion of it.
const defaultKFDTopologyRoot = "/sys/class/kfd/kfd/topology/nodes"

// kfdTopologyRoot is a variable so tests can point detection at a fixture tree.
var kfdTopologyRoot = defaultKFDTopologyRoot

// heapTypeFramebufferPublic is the HSA heap type for the memory pool an agent
// serves allocations from (HSA_HEAPTYPE_FRAME_BUFFER_PUBLIC). It is the pool to
// size a model against.
//
// On an APU this is the number that matters and it is NOT what the DRM node
// reports. A Strix Halo box publishes mem_info_vram_total = 512 MiB (the BIOS
// carve-out) while the KFD framebuffer heap reports the real ~62.5 GiB unified
// pool -- the same figure vLLM prints as free device memory at startup. Sizing a
// model off the DRM value would under-provision by more than 100x, so the KFD
// heap is the authority here.
const heapTypeFramebufferPublic = 1

// vendorIDAMD is PCI vendor 0x1002, as KFD reports it (decimal 4098).
const vendorIDAMD = 4098

// amdMarketingNames maps a PCI device ID to its marketing name, for the ones we
// have confirmed on real hardware. KFD's own `name` file is not usable for this
// -- on Strix Halo it reads "ip discovery" -- and there is no product_name node
// on an APU, so a small verified table plus an honest fallback beats guessing.
var amdMarketingNames = map[int64]string{
	0x1586: "AMD Radeon 8060S Graphics", // Strix Halo (Ryzen AI MAX+ 395)
}

// detectGPUsAMD reports the AMD GPU agents the kernel exposes. It returns nil
// (not an error) when the topology is absent, which is the normal case on a box
// with no amdgpu driver loaded.
func detectGPUsAMD() []GPUInfo {
	return detectGPUsAMDFrom(kfdTopologyRoot)
}

// detectGPUsAMDFrom is the testable core of detectGPUsAMD, reading from an
// arbitrary topology root.
func detectGPUsAMDFrom(root string) []GPUInfo {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}

	// Node directories are numeric. Sort them numerically rather than
	// lexically so node 10 does not sort between 1 and 2 and shuffle the
	// reported GPU indices.
	nodes := make([]int, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		n, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		nodes = append(nodes, n)
	}
	sort.Ints(nodes)

	var gpus []GPUInfo
	for _, n := range nodes {
		dir := filepath.Join(root, strconv.Itoa(n))
		props, err := parseKFDProperties(filepath.Join(dir, "properties"))
		if err != nil {
			continue
		}

		// Every KFD topology has a CPU node alongside the GPU agents. The CPU
		// node carries no SIMDs and no gfx target, so requiring both is what
		// separates a real accelerator from the host.
		if props["simd_count"] <= 0 || props["gfx_target_version"] <= 0 {
			continue
		}
		// Guard against a non-AMD agent appearing in this topology.
		if v, ok := props["vendor_id"]; ok && v != vendorIDAMD {
			continue
		}

		gfx := formatGFXTarget(props["gfx_target_version"])
		gpu := GPUInfo{
			Index:    len(gpus),
			Vendor:   VendorAMD,
			DeviceID: props["device_id"],
			Name:     amdGPUName(props["device_id"], gfx),
			MemoryGB: float64(sumFramebufferBytes(dir)) / (1024 * 1024 * 1024),
			Compute:  gfx,
		}
		gpus = append(gpus, gpu)
	}
	return gpus
}

// parseKFDProperties reads a KFD `properties` file, whose every line is a
// "<key> <integer>" pair.
func parseKFDProperties(path string) (map[string]int64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	props := make(map[string]int64)
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		v, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			continue
		}
		props[fields[0]] = v
	}
	return props, nil
}

// sumFramebufferBytes totals the framebuffer heaps a node serves allocations
// from. Summing (rather than taking the first) keeps the number right for an
// agent that publishes its pool as more than one bank.
func sumFramebufferBytes(nodeDir string) int64 {
	banks, err := os.ReadDir(filepath.Join(nodeDir, "mem_banks"))
	if err != nil {
		return 0
	}
	var total int64
	for _, b := range banks {
		if !b.IsDir() {
			continue
		}
		props, err := parseKFDProperties(filepath.Join(nodeDir, "mem_banks", b.Name(), "properties"))
		if err != nil {
			continue
		}
		if props["heap_type"] == heapTypeFramebufferPublic {
			total += props["size_in_bytes"]
		}
	}
	return total
}

// formatGFXTarget renders KFD's gfx_target_version as the gfx name the ROCm
// toolchain uses. The encoding is major*10000 + minor*100 + step, where minor
// and step are rendered as single hex digits -- which is why gfx90a (step 10)
// comes back from 90010 rather than as "gfx9010".
//
//	110501 -> gfx1151 (Strix Halo)
//	 90402 -> gfx942  (MI300X)
//	 90010 -> gfx90a  (MI200)
//	110000 -> gfx1100 (RDNA3)
func formatGFXTarget(v int64) string {
	if v <= 0 {
		return ""
	}
	major := v / 10000
	minor := (v / 100) % 100
	step := v % 100
	return fmt.Sprintf("gfx%d%x%x", major, minor, step)
}

// amdGPUName returns the marketing name for a device ID when we have confirmed
// one, and otherwise names the part by its gfx target. The fallback is
// deliberately not "Unknown": "AMD GPU (gfx1151)" still tells an operator
// exactly which kernels the device wants.
func amdGPUName(deviceID int64, gfx string) string {
	if name, ok := amdMarketingNames[deviceID]; ok {
		return name
	}
	if gfx != "" {
		return "AMD GPU (" + gfx + ")"
	}
	return "AMD GPU"
}
