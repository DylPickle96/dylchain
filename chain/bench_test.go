package chain

import (
	"fmt"
	"testing"
)

// BenchmarkClusterRun measures a full consensus run (empty blocks) at a few
// validator counts. Run with -cpuprofile to see where the time goes:
//
//	go test -run x -bench ClusterRun -benchmem -cpuprofile cpu.out ./chain
func BenchmarkClusterRun(b *testing.B) {
	const heights = 20
	for _, n := range []int{16, 64, 128} {
		set := NewValidatorSet(GenerateValidators(n)...)
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				cl := NewCluster(nil, set, 0)
				cl.Run(heights)
			}
			b.ReportMetric(float64(heights), "heights/op")
		})
	}
}
