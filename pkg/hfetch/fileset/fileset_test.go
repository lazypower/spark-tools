package fileset

import (
	"testing"

	ifs "github.com/lazypower/spark-tools/internal/fileset"
)

// The full behavior suite lives in internal/fileset; this locks the compat
// surface (alias identity + const delegation).

func TestWrapper_Aliases(t *testing.T) {
	var _ *ifs.Report = (*Report)(nil)
	var _ *ifs.Issue = (*Issue)(nil)
	if ProfileGGUF != ifs.ProfileGGUF || ProfileVLLM != ifs.ProfileVLLM {
		t.Error("profile consts must equal the internal authority")
	}
}
