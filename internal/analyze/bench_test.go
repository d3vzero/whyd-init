package analyze

import (
	"testing"

	"github.com/d3vzero/whyd-init/internal/journal"
	"github.com/d3vzero/whyd-init/internal/rules"
)

func BenchmarkRun(b *testing.B) {
	set, err := rules.LoadDefault()
	if err != nil {
		b.Fatal(err)
	}
	for b.Loop() {
		if _, err := Run(Input{Rules: set, Read: func(fn func(journal.Entry) error) error {
			return journal.ReadFile("../../testdata/boot-unclean-oom.jsonl.gz", fn)
		}}); err != nil {
			b.Fatal(err)
		}
	}
}
