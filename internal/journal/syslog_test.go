package journal

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// a log as dotfiles-aro.minimal writes it: three boots, the first ends in a kernel panic kept by
// pstore, the second shuts down cleanly, the third is running
func TestLoadSyslog(t *testing.T) {
	dir := t.TempDir()
	ps := filepath.Join(dir, "pstore", "x")
	if err := os.MkdirAll(ps, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ps, "dmesg-efi-1"),
		[]byte("Kernel panic - not syncing: Fatal exception\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	log := "" +
		"2026-10-13T19:29:09+07:00\t5\taro-boot\t9\tshutdown clean OLD uptime 5s\n" +
		"2026-10-13T19:29:36+07:00\t5\tsyslog-ng\t1\tsyslog-ng starting up; version='4.12.0'\n" +
		"2026-10-13T19:29:36+07:00\t6\tlogin\t2\tLOGIN ON tty1 BY AI-Trainer\n" +
		"2026-10-13T19:29:37+07:00\t5\taro-boot\t3\tboot B1 kernel 7.2.9\n" +
		"2026-10-13T20:00:00+07:00\t4\tkernel\t\tNVRM: Xid 79\n" +
		"2026-10-13T20:10:00+07:00\t5\tsyslog-ng\t1\tsyslog-ng starting up; version='4.12.0'\n" +
		"2026-10-13T20:10:01+07:00\t5\taro-boot\t3\tboot B2 kernel 7.2.9\n" +
		fmt.Sprintf("2026-10-13T20:10:01+07:00\t2\taro-boot\t3\tpstore: saved in %s\n", ps) +
		"2026-10-13T21:00:00+07:00\t5\taro-boot\t3\tshutdown clean B2 uptime 1s\n" +
		"2026-10-13T21:00:01+07:00\t6\tNetworkManager\t5\tstopping\n" +
		"2026-10-13T21:01:00+07:00\t5\tsyslog-ng\t1\tsyslog-ng starting up; version='4.12.0'\n" +
		"2026-10-13T21:01:01+07:00\t5\taro-boot\t3\tboot B3 kernel 7.2.9\n" +
		"not a log line\n"
	if err := os.WriteFile(filepath.Join(dir, "2026-10-13.log"), []byte(log), 0o644); err != nil {
		t.Fatal(err)
	}
	boots, err := loadSyslog(dir, "B3")
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		id    string
		index int
		lines int
		last  string
	}{
		{"B1", -2, 5, "Kernel panic - not syncing: Fatal exception"}, // starts at "starting up"
		{"B2", -1, 6, "Journal stopped"},
		{"B3", 0, 2, "boot B3 kernel 7.2.9"},
	}
	if len(boots) != len(want) {
		t.Fatalf("got %d boots, want %d", len(boots), len(want))
	}
	for i, w := range want {
		b := boots[i]
		last := b.entries[len(b.entries)-1]
		if b.boot.ID != w.id || b.boot.Index != w.index || len(b.entries) != w.lines || last.Message != w.last {
			t.Errorf("boot %d: id %s index %d lines %d last %q; want %+v",
				i, b.boot.ID, b.boot.Index, len(b.entries), last.Message, w)
		}
	}
	if e := boots[0].entries[3]; !e.Kernel || e.Priority != 4 || e.Source != "kernel" {
		t.Errorf("kernel line read as %+v", e)
	}
	// the running boot has no marker yet: every boot has ended
	if boots, _ := loadSyslog(dir, "OTHER"); boots[2].boot.Index != -1 {
		t.Errorf("without the running boot, the last index is %d, want -1", boots[2].boot.Index)
	}
}
