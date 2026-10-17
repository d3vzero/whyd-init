package journal

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
)

// SystemReadable reports whether this user can read the system journal. Without it journalctl
// silently shows only the user's own messages, and every verdict would be wrong. It is true
// when no journal file is found (nothing to check: journalctl will say what's wrong).
func SystemReadable() bool {
	if useSyslog() {
		return syslogReadable()
	}
	for _, dir := range []string{"/var/log/journal", "/run/log/journal"} {
		files, _ := filepath.Glob(filepath.Join(dir, "*", "system.journal"))
		for _, f := range files {
			if fh, err := os.Open(f); err == nil {
				_ = fh.Close()
				return true
			}
		}
		if len(files) > 0 {
			return false
		}
	}
	return true
}

// ReadFile streams the entries of a recorded journal (journalctl -o json output, gzip-compressed
// or not): used by tests and by `whyd --file` to analyse a journal from another machine.
// The path "-" reads standard input.
func ReadFile(path string, fn func(Entry) error) error {
	var f io.Reader = os.Stdin
	if path != "-" {
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer func() { _ = file.Close() }()
		f = file
	}
	br := bufio.NewReader(f)
	if magic, _ := br.Peek(2); !bytes.Equal(magic, []byte{0x1f, 0x8b}) { // not gzip
		return Read(br, fn)
	}
	gz, err := gzip.NewReader(br)
	if err != nil {
		return err
	}
	defer func() { _ = gz.Close() }()
	return Read(gz, fn)
}
