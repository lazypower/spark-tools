// Package seam holds cross-boundary contract ("seam") tests.
//
// Unit tests verify a package against its own mocks. Seam tests verify the
// CONTRACT between two components — or between our mocks and the real protocol
// they stand in for — which is exactly where this repo's bugs have hidden:
//
//   - a fix landing in one copy of a duplicated adapter but not the other,
//   - a mock that doesn't match real HuggingFace response shapes,
//   - one tool writing shared state (the hfetch registry) that another tool
//     reads and miscategorizes.
//
// A failing seam test is a tripwire: it names a broken contract and where it
// broke. Some tests here are intentionally RED — they document a contract we
// have currently violated and stay red until it is honored.
package seam
