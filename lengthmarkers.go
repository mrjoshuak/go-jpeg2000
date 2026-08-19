package jpeg2000

import (
	"encoding/binary"
	"fmt"

	"github.com/mrjoshuak/go-jpeg2000/internal/codestream"
)

// Packet length markers: PLT in the tile-part header (A.7.2) and TLM in the
// main header (A.7.1).
//
// Both exist for the same reason. A packet's position in a codestream depends
// on the length of every packet before it, and a packet's length is only known
// by parsing its header. So locating packet N means reading packets 0..N-1 —
// a chain of small dependent reads, which is the wrong shape for storage that
// charges per round trip. PLT lists the lengths outright, so a reader that has
// the tile-part header can compute every packet's offset by summation, and TLM
// does the same for tile-parts in the main header. Together they turn index
// construction into one ranged read near the front of the file instead of a
// walk over all of it.

// pltIplt encodes one packet length in the variable-length form Iplt uses:
// seven bits per byte, most significant group first, with the high bit set on
// every byte but the last.
func pltIplt(n int, dst []byte) []byte {
	if n < 0 {
		n = 0
	}
	// Emit the groups most significant first, which means finding the top
	// non-zero group before writing anything.
	shift := 0
	for n>>uint(shift+7) != 0 {
		shift += 7
	}
	for ; shift > 0; shift -= 7 {
		dst = append(dst, byte((n>>uint(shift))&0x7F)|0x80)
	}
	return append(dst, byte(n&0x7F))
}

// ipltLen returns how many bytes pltIplt will write for n.
func ipltLen(n int) int {
	if n < 0 {
		n = 0
	}
	c := 1
	for n >>= 7; n != 0; n >>= 7 {
		c++
	}
	return c
}

// generatePLT returns the PLT marker segments describing lengths, in order.
//
// A marker segment carries a 16-bit length, so a tile-part with many packets
// needs several PLT segments, numbered by Zplt. A single packet's encoded
// length is never split across two segments: the standard permits it, and a
// reader that assumes otherwise is a reader this library would then depend on
// being lenient.
func generatePLT(lengths []int) []byte {
	if len(lengths) == 0 {
		return nil
	}
	const maxSeg = 65535 // Lplt is 16 bits, counting itself

	var out []byte
	zplt := 0
	i := 0
	for i < len(lengths) && zplt <= 255 {
		// Lplt + Zplt = 3 bytes of overhead before any Iplt.
		body := make([]byte, 0, 256)
		for i < len(lengths) {
			n := ipltLen(lengths[i])
			if 3+len(body)+n > maxSeg {
				break
			}
			body = pltIplt(lengths[i], body)
			i++
		}
		if len(body) == 0 {
			// A single length longer than a whole segment cannot happen —
			// Iplt for the largest int32 is five bytes — but returning here
			// rather than looping forever is what keeps that true by
			// construction.
			break
		}
		seg := make([]byte, 0, 5+len(body))
		seg = binary.BigEndian.AppendUint16(seg, uint16(codestream.PLT))
		seg = binary.BigEndian.AppendUint16(seg, uint16(3+len(body)))
		seg = append(seg, byte(zplt))
		seg = append(seg, body...)
		out = append(out, seg...)
		zplt++
	}
	return out
}

// parsePLT reads the packet lengths from one PLT marker segment body, which is
// everything after Lplt: a Zplt byte followed by the Iplt list.
func parsePLT(body []byte) (zplt int, lengths []int, err error) {
	if len(body) < 1 {
		return 0, nil, fmt.Errorf("PLT segment is empty")
	}
	zplt = int(body[0])
	n := 0
	started := false
	for _, b := range body[1:] {
		// Guard the accumulator: a segment of 0x80 bytes would otherwise
		// shift a length past any bound the caller could check.
		if n > (1<<31-1)>>7 {
			return 0, nil, fmt.Errorf("PLT packet length overflows")
		}
		n = n<<7 | int(b&0x7F)
		started = true
		if b&0x80 == 0 {
			lengths = append(lengths, n)
			n, started = 0, false
		}
	}
	if started {
		return 0, nil, fmt.Errorf("PLT segment ends inside a packet length")
	}
	return zplt, lengths, nil
}

// generateTLM returns a TLM marker segment listing every tile-part's index and
// length, in the order the tile-parts appear.
//
// Stlm is written as 0x50: a one-byte tile index (ST=1, bits 4-5) and a
// four-byte length (SP=1, bit 6). ST=0 would be shorter but only works when the
// tile-parts appear in tile order with exactly one each, and a reader cannot
// tell which convention was meant from the marker alone.
//
// The bit positions matter and are easy to get wrong: 0x60 also looks like
// "ST and SP set", but it puts 2 in the ST field, promising a two-byte tile
// index that is not there. OpenJPEG reports that as "TLM marker not of
// expected size" and ignores the marker, which is a warning on stderr rather
// than a decode failure — so nothing about the image looks wrong.
func generateTLM(tileIdx []int, lengths []uint32) []byte {
	if len(tileIdx) == 0 || len(tileIdx) != len(lengths) {
		return nil
	}
	const perEntry = 5 // ST=1 byte + SP=4 bytes
	// Ltlm counts itself, Ztlm and Stlm: 4 bytes before the entries.
	if 4+perEntry*len(tileIdx) > 65535 {
		return nil
	}
	out := make([]byte, 0, 6+perEntry*len(tileIdx))
	out = binary.BigEndian.AppendUint16(out, uint16(codestream.TLM))
	out = binary.BigEndian.AppendUint16(out, uint16(4+perEntry*len(tileIdx)))
	out = append(out, 0)    // Ztlm: one TLM segment
	out = append(out, 0x50) // Stlm: ST=1 (bits 4-5), SP=1 (bit 6)
	for i := range tileIdx {
		out = append(out, byte(tileIdx[i]))
		out = binary.BigEndian.AppendUint32(out, lengths[i])
	}
	return out
}
