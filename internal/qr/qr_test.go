package qr

import (
	"strings"
	"testing"
)

func TestFormatAndVersionBCHConstants(t *testing.T) {
	wantFormat := []uint32{0x77C4, 0x72F3, 0x7DAA, 0x789D, 0x662F, 0x6318, 0x6C41, 0x6976}
	for mask, want := range wantFormat {
		if got := formatBitsLevelL(mask); got != want {
			t.Fatalf("formatBitsLevelL(%d) = 0x%04X, want 0x%04X", mask, got, want)
		}
	}
	wantVersion := map[int]uint32{
		7: 0x07C94, 8: 0x085BC, 9: 0x09A99, 10: 0x0A4D3, 11: 0x0BBF6, 12: 0x0C762,
	}
	for ver, want := range wantVersion {
		if got := versionBits(ver); got != want {
			t.Fatalf("versionBits(%d) = 0x%05X, want 0x%05X", ver, got, want)
		}
	}
}

func TestEncodePairingURIAndFinderStructure(t *testing.T) {
	uri := "check-usage://pair?code=abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG&url=https%3A%2F%2Fhomelab.tail1234.ts.net"
	code, err := Encode(uri)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	if code.Version < 5 || code.Version > 8 {
		t.Fatalf("unexpected version %d for %d-byte URI", code.Version, len(uri))
	}
	// Verify all three 7x7 finder pattern centers are dark and outer rings are dark.
	finders := [][2]int{{3, 3}, {code.Size - 4, 3}, {3, code.Size - 4}}
	for _, f := range finders {
		if !code.At(f[0], f[1]) {
			t.Fatalf("finder center (%d,%d) not dark", f[0], f[1])
		}
		if code.At(f[0]-2, f[1]-2) {
			t.Fatalf("finder white ring (%d,%d) is dark", f[0]-2, f[1]-2)
		}
		if !code.At(f[0]-3, f[1]-3) {
			t.Fatalf("finder outer border (%d,%d) not dark", f[0]-3, f[1]-3)
		}
	}
	// Fixed dark module at (8, size-8).
	if !code.At(8, code.Size-8) {
		t.Fatal("fixed dark module missing")
	}

	rendered := code.RenderTerminal(true)
	lines := strings.Split(rendered, "\n")
	wantLines := (code.Size + 4 + 1) / 2
	if len(lines) != wantLines {
		t.Fatalf("rendered %d lines, want %d", len(lines), wantLines)
	}
	if !strings.HasPrefix(lines[0], "\x1b[47;30m") || !strings.HasSuffix(lines[0], "\x1b[0m") {
		t.Fatalf("expected ANSI white-on-black framing on first line: %q", lines[0])
	}
}

func TestReedSolomonKnownVector(t *testing.T) {
	// ISO/IEC 18004 standard Version 1-M/L example check:
	// "01234567" in byte mode, V1-L (19 data codewords, 7 ECC bytes).
	// Verify rsRemainder produces non-zero polynomial whose evaluation at alpha^0..alpha^6 is 0.
	data := buildDataCodewords([]byte("check-usage"), specs[0])
	gen := rsGenerator(specs[0].ecPerBlock)
	ecc := rsRemainder(data, gen)
	full := append(append([]byte(nil), data...), ecc...)
	var root byte = 1
	for i := range specs[0].ecPerBlock {
		var eval byte
		for _, c := range full {
			eval = gfMultiply(eval, root) ^ c
		}
		if eval != 0 {
			t.Fatalf("RS syndrome[%d] = %d, want 0", i, eval)
		}
		root = gfMultiply(root, 0x02)
	}
}

func TestEncodeLongV10Payload(t *testing.T) {
	// Exercises 16-bit byte length indicator and 2-group RS interleaving (Version 10).
	long := strings.Repeat("a", 240)
	code, err := Encode(long)
	if err != nil {
		t.Fatalf("Encode(240 bytes) error = %v", err)
	}
	if code.Version != 10 {
		t.Fatalf("version = %d, want 10", code.Version)
	}
}

func TestMatrixRoundTripDecodeAllVersions(t *testing.T) {
	payloads := []string{
		"hi",
		"https://homelab.tail1234.ts.net",
		"check-usage://pair?code=abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG&url=https%3A%2F%2Fhomelab.tail1234.ts.net",
		strings.Repeat("x", 160), // Version 8 (2 blocks + version bits)
		strings.Repeat("y", 250), // Version 10 (16-bit length + 2 groups)
		strings.Repeat("z", 340), // Version 12 (2 groups)
	}
	for _, want := range payloads {
		code, err := Encode(want)
		if err != nil {
			t.Fatalf("Encode(%d bytes) error = %v", len(want), err)
		}
		spec := specs[code.Version-1]
		size := code.Size

		// Read format bits from both copies and confirm they match a valid Level-L mask.
		var fmt1, fmt2 uint32
		for i := range 6 {
			if code.At(8, i) {
				fmt1 |= 1 << uint(i)
			}
		}
		if code.At(8, 7) {
			fmt1 |= 1 << 6
		}
		if code.At(8, 8) {
			fmt1 |= 1 << 7
		}
		if code.At(7, 8) {
			fmt1 |= 1 << 8
		}
		for i := 9; i < 15; i++ {
			if code.At(14-i, 8) {
				fmt1 |= 1 << uint(i)
			}
		}
		for i := range 8 {
			if code.At(size-1-i, 8) {
				fmt2 |= 1 << uint(i)
			}
		}
		for i := 8; i < 15; i++ {
			if code.At(8, size-15+i) {
				fmt2 |= 1 << uint(i)
			}
		}
		if fmt1 != fmt2 {
			t.Fatalf("version %d: format copies disagree: 0x%04X vs 0x%04X", code.Version, fmt1, fmt2)
		}
		rawFmt := fmt1 ^ 0x5412
		if (rawFmt >> 13) != 0b01 {
			t.Fatalf("version %d: EC level bits = %02b, want 01 (L)", code.Version, rawFmt>>13)
		}
		mask := int((rawFmt >> 10) & 0b111)
		if formatBitsLevelL(mask) != fmt1 {
			t.Fatalf("version %d: format BCH mismatch", code.Version)
		}

		// Verify version bits on both copies for V7+.
		if code.Version >= 7 {
			var v1, v2 uint32
			for i := range 18 {
				a := size - 11 + i%3
				b := i / 3
				if code.At(a, b) {
					v1 |= 1 << uint(i)
				}
				if code.At(b, a) {
					v2 |= 1 << uint(i)
				}
			}
			if v1 != v2 || v1 != versionBits(code.Version) {
				t.Fatalf("version %d: version bits mismatch v1=0x%X v2=0x%X want=0x%X", code.Version, v1, v2, versionBits(code.Version))
			}
		}

		// Rebuild function mask and extract unmasked bits in standard zigzag order.
		dummy := make([]bool, size*size)
		fn := make([]bool, size*size)
		placeFunctionPatterns(dummy, fn, size, spec)
		totalBlocks := spec.group1Cnt + spec.group2Cnt
		totalCodewords := spec.totalData + totalBlocks*spec.ecPerBlock
		cw := make([]byte, totalCodewords)
		bitIdx := 0
		for right := size - 1; right >= 1; right -= 2 {
			if right == 6 {
				right = 5
			}
			for vert := range size {
				for j := range 2 {
					x := right - j
					y := vert
					if ((right + 1) & 2) == 0 {
						y = size - 1 - vert
					}
					if fn[y*size+x] {
						continue
					}
					if bitIdx >= totalCodewords*8 {
						break
					}
					bit := code.At(x, y)
					var inv bool
					switch mask {
					case 0:
						inv = (x+y)%2 == 0
					case 1:
						inv = y%2 == 0
					case 2:
						inv = x%3 == 0
					case 3:
						inv = (x+y)%3 == 0
					case 4:
						inv = (x/3+y/2)%2 == 0
					case 5:
						inv = (x*y)%2+(x*y)%3 == 0
					case 6:
						inv = ((x*y)%2+(x*y)%3)%2 == 0
					case 7:
						inv = ((x+y)%2+(x*y)%3)%2 == 0
					}
					if inv {
						bit = !bit
					}
					if bit {
						cw[bitIdx>>3] |= 1 << uint(7-(bitIdx&7))
					}
					bitIdx++
				}
			}
		}

		// De-interleave data and ECC blocks, verify RS syndromes for every block.
		blockLens := make([]int, 0, totalBlocks)
		for range spec.group1Cnt {
			blockLens = append(blockLens, spec.group1Data)
		}
		for range spec.group2Cnt {
			blockLens = append(blockLens, spec.group2Data)
		}
		dataBlocks := make([][]byte, totalBlocks)
		for i, l := range blockLens {
			dataBlocks[i] = make([]byte, l)
		}
		pos := 0
		maxLen := spec.group1Data
		if spec.group2Data > maxLen {
			maxLen = spec.group2Data
		}
		for i := range maxLen {
			for b := range totalBlocks {
				if i < blockLens[b] {
					dataBlocks[b][i] = cw[pos]
					pos++
				}
			}
		}
		ecBlocks := make([][]byte, totalBlocks)
		for i := range totalBlocks {
			ecBlocks[i] = make([]byte, spec.ecPerBlock)
		}
		for i := range spec.ecPerBlock {
			for b := range totalBlocks {
				ecBlocks[b][i] = cw[pos]
				pos++
			}
		}
		var stream []byte
		for b := range totalBlocks {
			full := append(append([]byte(nil), dataBlocks[b]...), ecBlocks[b]...)
			var root byte = 1
			for s := range spec.ecPerBlock {
				var eval byte
				for _, c := range full {
					eval = gfMultiply(eval, root) ^ c
				}
				if eval != 0 {
					t.Fatalf("version %d block %d RS syndrome[%d] = %d, want 0", code.Version, b, s, eval)
				}
				root = gfMultiply(root, 0x02)
			}
			stream = append(stream, dataBlocks[b]...)
		}

		// Decode byte-mode header and payload.
		readBits := func(offset, count int) int {
			val := 0
			for i := range count {
				bi := offset + i
				val = (val << 1) | int((stream[bi>>3]>>uint(7-(bi&7)))&1)
			}
			return val
		}
		if mode := readBits(0, 4); mode != 0b0100 {
			t.Fatalf("version %d: mode = %04b, want 0100", code.Version, mode)
		}
		countBits := 8
		if code.Version >= 10 {
			countBits = 16
		}
		n := readBits(4, countBits)
		got := make([]byte, n)
		for i := range n {
			got[i] = byte(readBits(4+countBits+i*8, 8))
		}
		if string(got) != want {
			t.Fatalf("version %d: decoded %q, want %q", code.Version, string(got), want)
		}
	}
}
