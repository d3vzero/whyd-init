package report

import (
	"errors"
	"io"
	"testing"
)

type failWriter struct{ calls int }

func (f *failWriter) Write([]byte) (int, error) {
	f.calls++
	return 0, io.ErrClosedPipe
}

func TestWriteErrorReturned(t *testing.T) {
	r := freeze(t)
	red := newRedactor([]string{"archlinux"}, []string{"user"})
	for name, write := range map[string]func(io.Writer) error{
		"Markdown": func(w io.Writer) error { return Markdown(w, r, red) },
		"Text":     func(w io.Writer) error { return Text(w, r) },
	} {
		fw := &failWriter{}
		if err := write(fw); !errors.Is(err, io.ErrClosedPipe) {
			t.Errorf("%s: got %v, want %v", name, err, io.ErrClosedPipe)
		}
		if fw.calls != 1 {
			t.Errorf("%s: %d writes, want 1 (none after the first error)", name, fw.calls)
		}
	}
}
