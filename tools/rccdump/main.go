// Command rccdump extracts Qt resource (rcc) trees that are compiled into an
// ELF binary, such as reMarkable's /usr/bin/xochitl.
//
// Qt only writes the "qres" magic header when a resource is produced as a
// standalone .rcc file. When rcc output is compiled into the executable, the
// three arrays it generates (qt_resource_data, qt_resource_name,
// qt_resource_struct) end up in .rodata as anonymous blobs with no magic and no
// symbols in a stripped binary. rccdump therefore locates each of the three
// sections by shape, from the format alone:
//
//	tree   an array of fixed-size nodes, big-endian:
//	         u32 name offset, u16 flags,
//	         directory (flags&2): u32 child count, u32 first child index
//	         file:               u16 territory, u16 language, u32 data offset
//	         u64 last-modified   (format version >= 2 only)
//	       node 0 is the root: name offset 0, flags 2, first child 1.
//	names  a run of entries: u16 length (UTF-16 code units), u32 hash,
//	       then length*2 bytes of UTF-16BE. The hash is Qt's own 28-bit
//	       string hash, which makes it cheap to verify a candidate entry.
//	data   a run of entries: u32 length, then length bytes of payload.
//	       flags&1 => zlib, with a u32 big-endian uncompressed size in front
//	       of the stream; flags&4 => zstd, framed, with no size prefix.
//
// Written from the format description only; no code from any other project.
//
// Usage:
//
//	rccdump -out DIR FILE...
package main

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/klauspost/compress/zstd"
)

const (
	flagZlib = 0x01
	flagDir  = 0x02
	flagZstd = 0x04

	// How far from the tree to look for the matching names section. rcc emits
	// the sections adjacently, but padding and section ordering vary.
	nameSearchWindow = 1 << 18
)

type node struct {
	nameOff uint32
	flags   uint16
	// directory
	childCount uint32
	childFirst uint32
	// file
	dataOff uint32
}

type resource struct {
	treeAt  int
	nameAt  int
	dataAt  int
	nodeLen int
	files   []file
}

type file struct {
	path    string
	dataOff uint32
	flags   uint16
}

type scanner struct {
	d []byte
}

// qtHash is the string hash rcc stores alongside each name entry.
func qtHash(s []uint16) uint32 {
	var h uint32
	for _, c := range s {
		h = (h << 4) + uint32(c)
		h ^= (h & 0xf0000000) >> 23
		h &= 0x0fffffff
	}
	return h
}

func (s *scanner) be16(p int) uint16 { return binary.BigEndian.Uint16(s.d[p:]) }
func (s *scanner) be32(p int) uint32 { return binary.BigEndian.Uint32(s.d[p:]) }

// nameAt decodes a names-section entry, returning the decoded name and the
// offset just past it. allowEmpty permits the zero-length root name.
func (s *scanner) nameAt(p int, allowEmpty bool) (string, int, bool) {
	if p < 0 || p+6 > len(s.d) {
		return "", 0, false
	}
	n := int(s.be16(p))
	if n > 200 || (n == 0 && !allowEmpty) {
		return "", 0, false
	}
	end := p + 6 + 2*n
	if end > len(s.d) {
		return "", 0, false
	}
	units := make([]uint16, n)
	var sb strings.Builder
	for i := 0; i < n; i++ {
		c := s.be16(p + 6 + 2*i)
		// Resource paths are ASCII in practice; the check prunes candidates
		// cheaply before the hash is computed.
		if c < 0x20 || c > 0x7e {
			return "", 0, false
		}
		units[i] = c
		sb.WriteByte(byte(c))
	}
	if qtHash(units) != s.be32(p+2) {
		return "", 0, false
	}
	return sb.String(), end, true
}

// treeRoots returns offsets that look like node 0 of a resource tree.
func (s *scanner) treeRoots() []int {
	var out []int
	for p := 0; p+22 <= len(s.d); p++ {
		if s.be32(p) != 0 || s.be16(p+4) != flagDir {
			continue
		}
		if cc := s.be32(p + 6); cc < 1 || cc > 8000 {
			continue
		}
		if s.be32(p+10) != 1 {
			continue
		}
		out = append(out, p)
	}
	return out
}

func (s *scanner) node(treeAt, nodeLen, i int) (node, bool) {
	p := treeAt + nodeLen*i
	if p+nodeLen > len(s.d) {
		return node{}, false
	}
	n := node{nameOff: s.be32(p), flags: s.be16(p + 4)}
	if n.flags&flagDir != 0 {
		n.childCount = s.be32(p + 6)
		n.childFirst = s.be32(p + 10)
	} else {
		n.dataOff = s.be32(p + 10)
	}
	return n, true
}

// walk reads every node reachable from the root. A well-formed tree numbers its
// nodes densely from 0, which is the check that rejects false roots.
func (s *scanner) walk(treeAt, nodeLen int) (map[int]node, bool) {
	nodes := map[int]node{}
	stack := []int{0}
	max := 0
	for len(stack) > 0 {
		i := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if _, seen := nodes[i]; seen {
			continue
		}
		n, ok := s.node(treeAt, nodeLen, i)
		if !ok {
			return nil, false
		}
		nodes[i] = n
		if i > max {
			max = i
		}
		if n.flags&flagDir != 0 {
			if n.childCount > 8000 || int(n.childFirst) <= i || n.childFirst > 200000 {
				return nil, false
			}
			for c := int(n.childFirst); c < int(n.childFirst)+int(n.childCount); c++ {
				stack = append(stack, c)
			}
		}
	}
	if len(nodes) != max+1 {
		return nil, false
	}
	return nodes, true
}

// findNames locates the base of the names section for a parsed tree: the one
// offset at which every name offset in the tree decodes to a valid entry.
func (s *scanner) findNames(nodes map[int]node, treeAt, treeEnd int) (int, bool) {
	offs := map[uint32]bool{}
	for _, n := range nodes {
		offs[n.nameOff] = true
	}
	sorted := make([]uint32, 0, len(offs))
	for o := range offs {
		sorted = append(sorted, o)
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	try := func(base int) bool {
		for _, o := range sorted {
			if _, _, ok := s.nameAt(base+int(o), o == 0); !ok {
				return false
			}
		}
		return true
	}
	for b := treeAt - 6; b >= treeAt-nameSearchWindow && b >= 0; b-- {
		if try(b) {
			return b, true
		}
	}
	for b := treeEnd; b < treeEnd+nameSearchWindow && b < len(s.d); b++ {
		if try(b) {
			return b, true
		}
	}
	return 0, false
}

// entryPlausible checks a candidate data entry against the flags the tree
// records for it. The compression magic is what disambiguates a base for the
// small resources, where the offset chain alone is too weak a constraint.
func (s *scanner) entryPlausible(base int, f file) bool {
	p := base + int(f.dataOff)
	if p < 0 || p+4 > len(s.d) {
		return false
	}
	n := int(s.be32(p))
	if n < 0 || p+4+n > len(s.d) {
		return false
	}
	raw := s.d[p+4 : p+4+n]
	switch {
	case f.flags&flagZstd != 0:
		return len(raw) >= 4 && binary.LittleEndian.Uint32(raw) == 0xFD2FB528
	case f.flags&flagZlib != 0:
		// u32 uncompressed size, then a zlib stream.
		return len(raw) >= 6 && raw[4] == 0x78
	default:
		return true
	}
}

// findData locates the base of the data section. Entries are laid out
// back-to-back, so the gap between the two lowest data offsets in the tree is
// 4 plus the length stored at the lower one - a value that can be searched for
// without knowing the base, and then confirmed against the whole chain.
func (s *scanner) findData(files []file) (int, bool) {
	set := map[uint32]bool{}
	for _, f := range files {
		set[f.dataOff] = true
	}
	offs := make([]uint32, 0, len(set))
	for o := range set {
		offs = append(offs, o)
	}
	sort.Slice(offs, func(i, j int) bool { return offs[i] < offs[j] })

	valid := func(base int) bool {
		if base < 0 {
			return false
		}
		for k := 0; k+1 < len(offs); k++ {
			p := base + int(offs[k])
			if p+4 > len(s.d) || offs[k]+4+s.be32(p) != offs[k+1] {
				return false
			}
		}
		for _, f := range files {
			if !s.entryPlausible(base, f) {
				return false
			}
		}
		return true
	}

	if len(offs) >= 2 && offs[1] >= offs[0]+4 {
		var want [4]byte
		binary.BigEndian.PutUint32(want[:], offs[1]-offs[0]-4)
		for pos := 0; ; {
			j := bytes.Index(s.d[pos:], want[:])
			if j < 0 {
				break
			}
			at := pos + j
			pos = at + 1
			if base := at - int(offs[0]); valid(base) {
				return base, true
			}
		}
		return 0, false
	}

	// A single data entry gives no chain to follow; fall back to locating the
	// one position whose compression magic and length agree with the tree.
	var magic []byte
	switch {
	case files[0].flags&flagZstd != 0:
		magic = []byte{0x28, 0xB5, 0x2F, 0xFD}
	case files[0].flags&flagZlib != 0:
		magic = []byte{0x78}
	default:
		return 0, false
	}
	found, count := 0, 0
	skip := 4
	if files[0].flags&flagZlib != 0 {
		skip = 8
	}
	for pos := 0; ; {
		j := bytes.Index(s.d[pos:], magic)
		if j < 0 {
			break
		}
		at := pos + j
		pos = at + 1
		base := at - skip - int(offs[0])
		if valid(base) {
			found, count = base, count+1
			if count > 1 {
				return 0, false
			}
		}
	}
	return found, count == 1
}

func (s *scanner) resources() []resource {
	var out []resource
	for _, treeAt := range s.treeRoots() {
		for _, nodeLen := range []int{22, 14} {
			nodes, ok := s.walk(treeAt, nodeLen)
			if !ok {
				continue
			}
			nameAt, ok := s.findNames(nodes, treeAt, treeAt+nodeLen*len(nodes))
			if !ok {
				continue
			}
			var files []file
			var visit func(i int, prefix string) bool
			visit = func(i int, prefix string) bool {
				n := nodes[i]
				name, _, ok := s.nameAt(nameAt+int(n.nameOff), true)
				if !ok {
					return false
				}
				path := prefix
				if i != 0 {
					path = prefix + "/" + name
				}
				if n.flags&flagDir != 0 {
					for c := int(n.childFirst); c < int(n.childFirst)+int(n.childCount); c++ {
						if !visit(c, path) {
							return false
						}
					}
					return true
				}
				files = append(files, file{path: path, dataOff: n.dataOff, flags: n.flags})
				return true
			}
			if !visit(0, "") || len(files) == 0 {
				continue
			}
			dataAt, ok := s.findData(files)
			if !ok {
				dataAt = -1
			}
			out = append(out, resource{treeAt: treeAt, nameAt: nameAt, dataAt: dataAt, nodeLen: nodeLen, files: files})
			break
		}
	}
	return out
}

func (s *scanner) payload(r resource, f file, dec *zstd.Decoder) ([]byte, error) {
	p := r.dataAt + int(f.dataOff)
	if p+4 > len(s.d) {
		return nil, fmt.Errorf("data offset out of range")
	}
	n := int(s.be32(p))
	if p+4+n > len(s.d) {
		return nil, fmt.Errorf("data length out of range")
	}
	raw := s.d[p+4 : p+4+n]
	switch {
	case f.flags&flagZstd != 0:
		return dec.DecodeAll(raw, nil)
	case f.flags&flagZlib != 0:
		if len(raw) < 4 {
			return nil, fmt.Errorf("short zlib entry")
		}
		zr, err := zlib.NewReader(bytes.NewReader(raw[4:]))
		if err != nil {
			return nil, err
		}
		defer zr.Close()
		return io.ReadAll(zr)
	default:
		return append([]byte(nil), raw...), nil
	}
}

func main() {
	out := flag.String("out", "", "directory to write the extracted tree into (required)")
	list := flag.Bool("list", false, "only list the resource paths found")
	flag.Parse()
	if flag.NArg() == 0 || (*out == "" && !*list) {
		fmt.Fprintln(os.Stderr, "usage: rccdump -out DIR FILE...")
		flag.PrintDefaults()
		os.Exit(2)
	}
	dec, err := zstd.NewReader(nil)
	if err != nil {
		fatal(err)
	}
	defer dec.Close()

	var written, failed, total int
	for _, path := range flag.Args() {
		d, err := os.ReadFile(path)
		if err != nil {
			fatal(err)
		}
		s := &scanner{d: d}
		res := s.resources()
		var nf int
		for _, r := range res {
			nf += len(r.files)
		}
		fmt.Printf("%s: %d resources, %d files\n", path, len(res), nf)
		total += nf
		for _, r := range res {
			for _, f := range r.files {
				if *list {
					fmt.Println(f.path)
					continue
				}
				if r.dataAt < 0 {
					failed++
					fmt.Fprintf(os.Stderr, "no data section: %s\n", f.path)
					continue
				}
				b, err := s.payload(r, f, dec)
				if err != nil {
					failed++
					fmt.Fprintf(os.Stderr, "%s: %v\n", f.path, err)
					continue
				}
				dst := filepath.Join(*out, filepath.FromSlash(strings.TrimPrefix(f.path, "/")))
				if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
					fatal(err)
				}
				if err := os.WriteFile(dst, b, 0o644); err != nil {
					fatal(err)
				}
				written++
			}
		}
	}
	if !*list {
		fmt.Printf("total: %d files, %d written, %d failed\n", total, written, failed)
	}
	if failed > 0 {
		os.Exit(1)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "rccdump:", err)
	os.Exit(1)
}
