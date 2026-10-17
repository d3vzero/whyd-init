package journal

// Syslog backend, for systems without journald (Artix with dinit or runit). It reads the files
// that dotfiles-aro.minimal (LOG_PERSIST=yes) has syslog-ng write to /var/log/aro: one message
// per line, "ISODATE<TAB>LEVEL<TAB>PROGRAM<TAB>PID<TAB>MESSAGE". The service aro-bootlog marks
// each boot ("boot <id> kernel <version>") and each clean shutdown ("shutdown clean <id> ...");
// after a crash it copies the kernel's pstore records and says so at the start of the next boot
// ("pstore: ... <dir>").
//
// A boot starts at the "syslog-ng starting up" line before its marker: the marker comes a moment
// later, after the first lines of the boot are already written. The end markers are turned into
// what the analysis already understands: a clean shutdown becomes journald's "Journal stopped"
// at the end of the boot, a pstore record a "Kernel panic - not syncing:" line.

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SyslogDir is where the syslog files are read from (WHYD_SYSLOG_DIR overrides it).
var SyslogDir = "/var/log/aro"

// useSyslog reports whether to read the syslog files instead of running journalctl:
// WHYD_BACKEND=syslog|journal decides, otherwise syslog when there is no journalctl and the
// directory exists.
func useSyslog() bool {
	if d := os.Getenv("WHYD_SYSLOG_DIR"); d != "" {
		SyslogDir = d
	}
	switch os.Getenv("WHYD_BACKEND") {
	case "syslog":
		return true
	case "journal":
		return false
	}
	if _, err := exec.LookPath("journalctl"); err == nil {
		return false
	}
	fi, err := os.Stat(SyslogDir)
	return err == nil && fi.IsDir()
}

// AccessHint says how to get read access to the system log, for the backend in use.
func AccessHint() string {
	if useSyslog() {
		return "you can't read the system log in " + SyslogDir + ".\n" +
			"Run whyd with sudo, or add yourself to the log group and log in again:\n" +
			"  sudo usermod -aG log $USER"
	}
	return "you can only read your own messages, not the system journal.\n" +
		"Run whyd with sudo, or add yourself to the systemd-journal group and log in again:\n" +
		"  sudo usermod -aG systemd-journal $USER"
}

type syslogBoot struct {
	boot    Boot
	entries []Entry
}

var sysCache struct {
	once  sync.Once
	boots []syslogBoot
	err   error
}

func syslogBoots() ([]syslogBoot, error) {
	sysCache.once.Do(func() { sysCache.boots, sysCache.err = loadSyslog(SyslogDir, currentBootID()) })
	return sysCache.boots, sysCache.err
}

func currentBootID() string {
	b, _ := os.ReadFile("/proc/sys/kernel/random/boot_id")
	return strings.TrimSpace(string(b))
}

func syslogReadable() bool {
	files, _ := filepath.Glob(filepath.Join(SyslogDir, "*.log"))
	if len(files) == 0 {
		_, err := os.ReadDir(SyslogDir)
		return err == nil // empty but readable: ListBoots says there's nothing yet
	}
	f, err := os.Open(files[0])
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

// parseSyslogLine reads one line; ok is false for a line that isn't ours.
func parseSyslogLine(line string) (Entry, bool) {
	f := strings.SplitN(line, "\t", 5)
	if len(f) < 5 {
		return Entry{}, false
	}
	t, err := time.Parse(time.RFC3339, f[0])
	if err != nil {
		return Entry{}, false
	}
	e := Entry{Time: t, Priority: -1, Source: f[2], Message: strings.TrimRight(f[4], " ")}
	if p, err := strconv.Atoi(f[1]); err == nil {
		e.Priority = p
	}
	e.PID, _ = strconv.Atoi(f[3])
	e.Kernel = f[2] == "kernel"
	return e, true
}

// loadSyslog reads every file of dir and cuts it into boots, oldest first. current is the
// running boot's ID: the last boot is the running one (index 0) only when its marker says so.
func loadSyslog(dir, current string) ([]syslogBoot, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.log"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files) // named by date: oldest first
	var all []Entry
	for _, name := range files {
		f, err := os.Open(name)
		if err != nil {
			return nil, err
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
		for sc.Scan() {
			if e, ok := parseSyslogLine(sc.Text()); ok {
				all = append(all, e)
			}
		}
		_ = f.Close()
		if err := sc.Err(); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
	}

	type mark struct {
		start int    // first line of the boot
		id    string // boot ID from the marker
	}
	var marks []mark
	up, prev := -1, -1
	for i, e := range all {
		if e.Source == "syslog-ng" && strings.Contains(e.Message, "starting up") {
			up = i
		}
		if e.Source == "aro-boot" && strings.HasPrefix(e.Message, "boot ") {
			start := i
			if up > prev {
				start = up
			}
			id := ""
			if f := strings.Fields(e.Message); len(f) > 1 {
				id = f[1]
			}
			marks = append(marks, mark{start, id})
			prev = i
		}
	}
	if len(marks) == 0 {
		return nil, errors.New("no boot recorded yet in " + dir +
			" (the aro-bootlog service writes one at each boot)")
	}

	boots := make([]syslogBoot, 0, len(marks))
	for k, m := range marks {
		end := len(all)
		if k+1 < len(marks) {
			end = marks[k+1].start
		}
		entries := append([]Entry(nil), all[m.start:end]...)
		if len(entries) == 0 {
			continue
		}
		b := Boot{ID: m.id, First: entries[0].Time, Last: entries[len(entries)-1].Time}
		clean := false
		for _, e := range entries {
			if e.Source == "aro-boot" && strings.HasPrefix(e.Message, "shutdown clean") {
				clean = true
			}
		}
		panicMsg := ""
		if k+1 < len(marks) && !clean {
			next := all[marks[k+1].start:]
			for i := 0; i < len(next) && i < 80; i++ {
				if next[i].Source == "aro-boot" && strings.HasPrefix(next[i].Message, "pstore:") {
					panicMsg = pstorePanic(next[i].Message)
				}
			}
		}
		switch {
		case panicMsg != "":
			entries = append(entries, Entry{Time: b.Last, Priority: 0, Source: "kernel", Kernel: true,
				Message: "Kernel panic - not syncing: " + panicMsg})
		case clean:
			entries = append(entries, Entry{Time: b.Last, Priority: 6, Source: "systemd-journald",
				Message: "Journal stopped"})
		}
		boots = append(boots, syslogBoot{boot: b, entries: entries})
	}

	// index like journalctl -b: 0 the running boot, -1 the one before, ...
	last := 0
	if boots[len(boots)-1].boot.ID != current {
		last = -1 // the running boot has no marker yet: every boot here has ended
	}
	for i := range boots {
		boots[i].boot.Index = last - (len(boots) - 1 - i)
	}
	return boots, nil
}

// pstorePanic finds the panic line in the records copied by aro-bootlog; the message ends
// with their directory.
func pstorePanic(msg string) string {
	f := strings.Fields(msg)
	dir := f[len(f)-1]
	files, _ := filepath.Glob(filepath.Join(dir, "*"))
	for _, name := range files {
		data, err := os.ReadFile(name)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			if i := strings.Index(line, "Kernel panic - not syncing: "); i >= 0 {
				return strings.TrimSpace(line[i+len("Kernel panic - not syncing: "):])
			}
		}
	}
	return "crash record kept in " + dir
}

func listBootsSyslog() ([]Boot, error) {
	sb, err := syslogBoots()
	if err != nil {
		return nil, err
	}
	out := make([]Boot, len(sb))
	for i, b := range sb {
		out[i] = b.boot
	}
	return out, nil
}

func readBootSyslog(b Boot, fn func(Entry) error) error {
	sb, err := syslogBoots()
	if err != nil {
		return err
	}
	for _, s := range sb {
		if s.boot.ID != b.ID || !s.boot.First.Equal(b.First) {
			continue
		}
		for _, e := range s.entries {
			if err := fn(e); err != nil {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("boot %s is no longer in %s", b.ID, SyslogDir)
}
