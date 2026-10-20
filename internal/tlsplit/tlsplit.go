// Package tlsplit implements best-effort TLS ClientHello fragmentation for
// dialers.
//
// Some networks shape or block TLS connections based on the plaintext
// ClientHello. Writing the first handshake record across more than one TCP
// segment can defeat that. The splitter never changes the byte stream: it only
// divides the same ClientHello record across TCP writes.
//
//   - FirstByte splits any buffer that looks like a TLS handshake record
//     carrying a ClientHello (record type 22, major version 3, handshake type
//     1 at byte 5) into its first byte and the remainder, without parsing the
//     handshake.
//   - SNI parses the full ClientHello and splits around the first hostname
//     byte; a malformed, truncated or SNI-less hello falls back to a single
//     logical write.
//
// Empty segments are skipped, so a hostname that is the final byte of the
// record produces two writes rather than three. Buffers that do not look like
// a ClientHello are written unchanged.
package tlsplit

import (
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
)

// Mode selects how Wrap splits the first write on a connection.
type Mode int

const (
	// Off disables fragmentation; Wrap returns the connection unchanged.
	Off Mode = iota

	// FirstByte writes the first byte of a TLS ClientHello separately from
	// the rest of the record.
	FirstByte

	// SNI writes the record up to the first byte of the server hostname, that
	// byte, and the remainder as three separate writes.
	SNI
)

// ParseMode converts a configuration value into a Mode. It is
// case-insensitive and ignores surrounding whitespace.
func ParseMode(s string) (Mode, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "off":
		return Off, nil
	case "first-byte":
		return FirstByte, nil
	case "sni":
		return SNI, nil
	case "":
		return Off, fmt.Errorf("tlsplit: empty TLS split mode (want %q, %q or %q)", "off", "first-byte", "sni")
	default:
		return Off, fmt.Errorf("tlsplit: unknown TLS split mode %q (want %q, %q or %q)", s, "off", "first-byte", "sni")
	}
}

// Wrap returns a connection that fragments the first write when it is a TLS
// ClientHello. The returned connection delegates every other method to conn,
// so deadlines, close semantics and reads are unchanged. Wrap returns conn
// unchanged for Off and any unrecognized mode.
func Wrap(conn net.Conn, mode Mode) net.Conn {
	if mode != FirstByte && mode != SNI {
		return conn
	}
	return &splitConn{Conn: conn, mode: mode, first: true}
}

// splitConn fragments the first ClientHello written to it and passes every
// later write through untouched.
type splitConn struct {
	net.Conn
	mode Mode

	mu    sync.Mutex
	first bool
}

// Write inspects and possibly fragments the first write. Later writes are
// forwarded without splitting and with short writes completed. Writes are
// serialized so concurrent callers cannot interleave bytes inside a split
// record.
func (c *splitConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.first {
		return writeFull(c.Conn, p)
	}
	c.first = false

	if !isClientHello(p) {
		return writeFull(c.Conn, p)
	}

	switch c.mode {
	case FirstByte:
		return writeSegments(c.Conn, p, []segment{{0, 1}, {1, len(p)}})
	case SNI:
		start, ok := sniHostOffset(p)
		if !ok {
			return writeFull(c.Conn, p)
		}
		return writeSegments(c.Conn, p, []segment{
			{0, start},
			{start, start + 1},
			{start + 1, len(p)},
		})
	default:
		return writeFull(c.Conn, p)
	}
}

// segment is a half-open byte range within the write buffer.
type segment struct {
	start, end int
}

// writeSegments writes each non-empty segment in order and returns the number
// of bytes consumed from p. A failure in the middle of a segment reports the
// bytes already written before the error.
func writeSegments(conn net.Conn, p []byte, segments []segment) (int, error) {
	total := 0
	for _, seg := range segments {
		if seg.start >= seg.end {
			continue
		}
		n, err := writeFull(conn, p[seg.start:seg.end])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

// writeFull completes short writes, returning the number of bytes written
// from p and the first error encountered. An empty buffer returns (0, nil)
// without calling conn.
func writeFull(conn net.Conn, p []byte) (int, error) {
	written := 0
	for written < len(p) {
		n, err := conn.Write(p[written:])
		if n < 0 || n > len(p)-written {
			return written, io.ErrShortWrite
		}
		written += n
		if err != nil {
			return written, err
		}
		if n == 0 {
			return written, io.ErrShortWrite
		}
	}
	return written, nil
}

// isClientHello reports whether p starts with an unencrypted TLS handshake
// record carrying a ClientHello: record type 22, major version 3, and
// handshake type 1.
func isClientHello(p []byte) bool {
	return len(p) >= 6 && p[0] == 0x16 && p[1] == 0x03 && p[5] == 0x01
}

// sniHostOffset returns the offset of the first byte of the server hostname
// in a ClientHello record. It parses only as far as the server_name extension
// and reports false on any malformed or incomplete input.
func sniHostOffset(p []byte) (int, bool) {
	if len(p) < 9 || !isClientHello(p) {
		return 0, false
	}

	recordLen := int(p[3])<<8 | int(p[4])
	if recordLen < 4 || 5+recordLen > len(p) {
		return 0, false
	}
	handshakeLen := int(p[6])<<16 | int(p[7])<<8 | int(p[8])
	if 4+handshakeLen > recordLen {
		return 0, false
	}

	// ClientHello body: client_version, random, session id, cipher suites,
	// compression methods, extensions.
	r := reader{b: p[9 : 9+handshakeLen]}
	if !r.skip(2 + 32) {
		return 0, false
	}
	sessionIDLen, ok := r.u8()
	if !ok || !r.skip(int(sessionIDLen)) {
		return 0, false
	}
	cipherSuitesLen, ok := r.u16()
	if !ok || !r.skip(cipherSuitesLen) {
		return 0, false
	}
	compressionLen, ok := r.u8()
	if !ok || !r.skip(int(compressionLen)) {
		return 0, false
	}
	extensionsLen, ok := r.u16()
	if !ok || extensionsLen > len(r.b)-r.i {
		return 0, false
	}

	extensionsEnd := r.i + extensionsLen
	for r.i+4 <= extensionsEnd {
		extType, _ := r.u16()
		extLen, _ := r.u16()
		if extLen > extensionsEnd-r.i {
			return 0, false
		}
		dataStart := r.i
		data := r.b[dataStart : dataStart+extLen]
		r.i += extLen
		if extType != 0 {
			continue
		}
		hostOffset, ok := serverNameHostOffset(data)
		if !ok {
			return 0, false
		}
		// r.b starts at p[9]; dataStart is relative to r.b.
		return 9 + dataStart + hostOffset, true
	}
	return 0, false
}

// serverNameHostOffset returns the offset of the first host_name entry inside
// the body of a server_name extension.
func serverNameHostOffset(data []byte) (int, bool) {
	if len(data) < 2 {
		return 0, false
	}
	listLen := int(data[0])<<8 | int(data[1])
	if listLen > len(data)-2 {
		return 0, false
	}
	end := 2 + listLen

	for i := 2; i+3 <= end; {
		nameType := data[i]
		nameLen := int(data[i+1])<<8 | int(data[i+2])
		i += 3
		if nameLen > end-i {
			return 0, false
		}
		if nameType == 0 {
			if nameLen == 0 {
				return 0, false
			}
			return i, true
		}
		i += nameLen
	}
	return 0, false
}

// reader is a bounds-checked cursor over a byte slice.
type reader struct {
	b []byte
	i int
}

// skip advances past n bytes and reports whether they are available.
func (r *reader) skip(n int) bool {
	if n < 0 || n > len(r.b)-r.i {
		return false
	}
	r.i += n
	return true
}

// u8 reads one byte.
func (r *reader) u8() (byte, bool) {
	if r.i >= len(r.b) {
		return 0, false
	}
	v := r.b[r.i]
	r.i++
	return v, true
}

// u16 reads a big-endian uint16.
func (r *reader) u16() (int, bool) {
	if len(r.b)-r.i < 2 {
		return 0, false
	}
	v := int(r.b[r.i])<<8 | int(r.b[r.i+1])
	r.i += 2
	return v, true
}
