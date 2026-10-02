//go:build linux && amd64

package collector

import (
	"debug/elf"
	"encoding/binary"
	"fmt"
	"os"
	"strings"
)

// buildID reads the GNU build-id note from an ELF file as a lowercase hex
// string, e.g. "d12c3ee7733f93c45a2171eed9ac4471edbd01b1".
func buildID(path string) (string, error) {
	f, err := elf.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	for _, s := range f.Sections {
		if s.Name != ".note.gnu.build-id" {
			continue
		}
		data, err := s.Data()
		if err != nil || len(data) < 12 {
			continue
		}
		namesz := binary.LittleEndian.Uint32(data[0:4])
		descsz := binary.LittleEndian.Uint32(data[4:8])
		off := 12 + align4(namesz)
		if int(off)+int(descsz) > len(data) {
			continue
		}
		return fmt.Sprintf("%x", data[off:off+descsz]), nil
	}
	return "", fmt.Errorf("%s: no .note.gnu.build-id section", path)
}

func align4(n uint32) uint32 { return (n + 3) &^ 3 }

// debugFileFor returns the split-debug file Ubuntu/Debian's dbgsym
// packages install for a binary (keyed by build-id), if present.
func debugFileFor(path string) (string, bool) {
	id, err := buildID(path)
	if err != nil || len(id) < 3 {
		return "", false
	}
	p := fmt.Sprintf("/usr/lib/debug/.build-id/%s/%s.debug", id[:2], id[2:])
	if st, err := os.Stat(p); err == nil && !st.IsDir() {
		return p, true
	}
	return "", false
}

// symbolAddress looks for a function named exactly `name` in path's own
// symbol table, then (since distro packages are commonly stripped) in its
// matching dbgsym split-debug file. LTO builds commonly rename internal
// static functions to "name.lto_priv.N" — a prefix match on that pattern
// is accepted too, skipping ".cold" entries (GCC's cold/unlikely-path
// split of the same function, not the real entry point).
func symbolAddress(path, name string) (uint64, bool) {
	candidates := []string{path}
	if dbg, ok := debugFileFor(path); ok {
		candidates = append(candidates, dbg)
	}
	for _, p := range candidates {
		f, err := elf.Open(p)
		if err != nil {
			continue
		}
		syms, _ := f.Symbols()
		dsyms, _ := f.DynamicSymbols()
		f.Close()
		all := append(syms, dsyms...)
		var fallback uint64
		found := false
		for _, s := range all {
			if s.Value == 0 {
				continue
			}
			if s.Name == name {
				return s.Value, true
			}
			if !found && strings.HasPrefix(s.Name, name+".") && !strings.Contains(s.Name, ".cold") {
				fallback, found = s.Value, true
			}
		}
		if found {
			return fallback, true
		}
	}
	return 0, false
}
