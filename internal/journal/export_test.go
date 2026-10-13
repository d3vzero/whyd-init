package journal

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestReadExport(t *testing.T) {
	var b bytes.Buffer
	b.WriteString("__REALTIME_TIMESTAMP=1791536032538710\nPRIORITY=3\n_COMM=sunshine\n_PID=55026\nMESSAGE=plain\n\n")
	// a message with a newline and an escape is written in binary form
	msg := "line one\n\x1b[1mline two"
	b.WriteString("PRIORITY=6\nSYSLOG_IDENTIFIER=kernel\n_COMM=swapper\n_TRANSPORT=kernel\nMESSAGE\n")
	_ = binary.Write(&b, binary.LittleEndian, uint64(len(msg)))
	b.WriteString(msg + "\nMESSAGE=second copy, ignored\n\n")
	var got []Entry
	if err := ReadExport(&b, func(e Entry) error { got = append(got, e); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("%d entries", len(got))
	}
	if e := got[0]; e.Message != "plain" || e.Priority != 3 || e.Source != "sunshine" || e.PID != 55026 || e.Time.UnixMicro() != 1791536032538710 {
		t.Errorf("first entry %+v", e)
	}
	if e := got[1]; e.Message != msg || e.Source != "kernel" || !e.Kernel || e.Priority != 6 {
		t.Errorf("second entry %+v", e)
	}
}
