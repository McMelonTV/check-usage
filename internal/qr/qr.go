// Package qr implements a zero-dependency ISO/IEC 18004 QR Code encoder
// (byte mode, error correction level L, versions 1-12) and compact terminal renderer.
package qr

import (
	"errors"
	"strings"
)

// Code is an encoded QR matrix of size Size x Size where dark modules are true.
type Code struct {
	Version int
	Size    int
	modules []bool
}

// At reports whether module (x, y) is dark. Coordinates outside the matrix are light.
func (c *Code) At(x, y int) bool {
	if x < 0 || y < 0 || x >= c.Size || y >= c.Size {
		return false
	}
	return c.modules[y*c.Size+x]
}

type versionSpec struct {
	version    int
	totalData  int
	ecPerBlock int
	group1Cnt  int
	group1Data int
	group2Cnt  int
	group2Data int
	alignment  []int
}

// Error correction level L tables from ISO/IEC 18004 (versions 1..12).
var specs = []versionSpec{
	{version: 1, totalData: 19, ecPerBlock: 7, group1Cnt: 1, group1Data: 19},
	{version: 2, totalData: 34, ecPerBlock: 10, group1Cnt: 1, group1Data: 34, alignment: []int{6, 18}},
	{version: 3, totalData: 55, ecPerBlock: 15, group1Cnt: 1, group1Data: 55, alignment: []int{6, 22}},
	{version: 4, totalData: 80, ecPerBlock: 20, group1Cnt: 1, group1Data: 80, alignment: []int{6, 26}},
	{version: 5, totalData: 108, ecPerBlock: 26, group1Cnt: 1, group1Data: 108, alignment: []int{6, 30}},
	{version: 6, totalData: 136, ecPerBlock: 18, group1Cnt: 2, group1Data: 68, alignment: []int{6, 34}},
	{version: 7, totalData: 156, ecPerBlock: 20, group1Cnt: 2, group1Data: 78, alignment: []int{6, 22, 38}},
	{version: 8, totalData: 194, ecPerBlock: 24, group1Cnt: 2, group1Data: 97, alignment: []int{6, 24, 42}},
	{version: 9, totalData: 232, ecPerBlock: 30, group1Cnt: 2, group1Data: 116, alignment: []int{6, 26, 46}},
	{version: 10, totalData: 274, ecPerBlock: 18, group1Cnt: 2, group1Data: 68, group2Cnt: 2, group2Data: 69, alignment: []int{6, 28, 50}},
	{version: 11, totalData: 324, ecPerBlock: 20, group1Cnt: 4, group1Data: 81, alignment: []int{6, 30, 54}},
	{version: 12, totalData: 370, ecPerBlock: 24, group1Cnt: 2, group1Data: 92, group2Cnt: 2, group2Data: 93, alignment: []int{6, 32, 58}},
}

// Encode encodes text as a byte-mode QR code at Error Correction Level L.
func Encode(text string) (*Code, error) {
	payload := []byte(text)
	var spec *versionSpec
	for i := range specs {
		countBits := 8
		if specs[i].version >= 10 {
			countBits = 16
		}
		neededBits := 4 + countBits + len(payload)*8
		if neededBits <= specs[i].totalData*8 {
			spec = &specs[i]
			break
		}
	}
	if spec == nil {
		return nil, errors.New("QR payload is too long")
	}

	data := buildDataCodewords(payload, *spec)
	codewords := interleaveBlocks(data, *spec)

	size := spec.version*4 + 17
	base := make([]bool, size*size)
	function := make([]bool, size*size)
	placeFunctionPatterns(base, function, size, *spec)
	placeDataBits(base, function, size, codewords)

	bestPenalty := int(^uint(0) >> 1)
	var bestModules []bool
	for mask := range 8 {
		candidate := make([]bool, len(base))
		copy(candidate, base)
		applyMask(candidate, function, size, mask)
		placeFormatBits(candidate, size, mask)
		if spec.version >= 7 {
			placeVersionBits(candidate, size, spec.version)
		}
		if p := penaltyScore(candidate, size); p < bestPenalty {
			bestPenalty = p
			bestModules = candidate
		}
	}

	return &Code{
		Version: spec.version,
		Size:    size,
		modules: bestModules,
	}, nil
}

// RenderTerminal renders the QR code compactly using Unicode half-blocks with a
// crisp 2-module white quiet zone so phone cameras scan reliably on any theme.
func (c *Code) RenderTerminal(ansiColor bool) string {
	const quiet = 2
	total := c.Size + 2*quiet
	var out strings.Builder
	for y := 0; y < total; y += 2 {
		if ansiColor {
			out.WriteString("\x1b[47;30m")
		}
		for x := range total {
			top := c.At(x-quiet, y-quiet)
			bot := false
			if y+1 < total {
				bot = c.At(x-quiet, y+1-quiet)
			}
			// Under white background + black foreground (47;30), drawing foreground
			// produces dark modules and background produces light modules.
			// Without ANSI colors, invert so dark terminals still show light quiet zone.
			if !ansiColor {
				top = !top
				bot = !bot
			}
			switch {
			case top && bot:
				out.WriteRune('█')
			case top && !bot:
				out.WriteRune('▀')
			case !top && bot:
				out.WriteRune('▄')
			default:
				out.WriteRune(' ')
			}
		}
		if ansiColor {
			out.WriteString("\x1b[0m")
		}
		if y+2 < total {
			out.WriteByte('\n')
		}
	}
	return out.String()
}

type bitBuffer struct {
	bits []bool
}

func (b *bitBuffer) appendBits(value uint32, count int) {
	for i := count - 1; i >= 0; i-- {
		b.bits = append(b.bits, ((value>>uint(i))&1) != 0)
	}
}

func buildDataCodewords(payload []byte, spec versionSpec) []byte {
	var buf bitBuffer
	buf.appendBits(0b0100, 4) // Byte mode indicator
	countBits := 8
	if spec.version >= 10 {
		countBits = 16
	}
	buf.appendBits(uint32(len(payload)), countBits)
	for _, b := range payload {
		buf.appendBits(uint32(b), 8)
	}
	capacityBits := spec.totalData * 8
	terminator := min(4, capacityBits-len(buf.bits))
	buf.appendBits(0, terminator)
	for len(buf.bits)%8 != 0 {
		buf.appendBits(0, 1)
	}
	padBytes := []byte{0xEC, 0x11}
	for i := 0; len(buf.bits) < capacityBits; i++ {
		buf.appendBits(uint32(padBytes[i%2]), 8)
	}
	out := make([]byte, spec.totalData)
	for i := range out {
		var val byte
		for bit := range 8 {
			if buf.bits[i*8+bit] {
				val |= 1 << uint(7-bit)
			}
		}
		out[i] = val
	}
	return out
}

func interleaveBlocks(data []byte, spec versionSpec) []byte {
	totalBlocks := spec.group1Cnt + spec.group2Cnt
	dataBlocks := make([][]byte, 0, totalBlocks)
	ecBlocks := make([][]byte, 0, totalBlocks)
	gen := rsGenerator(spec.ecPerBlock)
	offset := 0
	appendGroup := func(count, length int) {
		for range count {
			block := data[offset : offset+length]
			offset += length
			dataBlocks = append(dataBlocks, block)
			ecBlocks = append(ecBlocks, rsRemainder(block, gen))
		}
	}
	appendGroup(spec.group1Cnt, spec.group1Data)
	appendGroup(spec.group2Cnt, spec.group2Data)

	maxDataLen := spec.group1Data
	if spec.group2Data > maxDataLen {
		maxDataLen = spec.group2Data
	}
	result := make([]byte, 0, len(data)+totalBlocks*spec.ecPerBlock)
	for i := range maxDataLen {
		for _, block := range dataBlocks {
			if i < len(block) {
				result = append(result, block[i])
			}
		}
	}
	for i := range spec.ecPerBlock {
		for _, block := range ecBlocks {
			result = append(result, block[i])
		}
	}
	return result
}

func gfMultiply(x, y byte) byte {
	var z byte
	for i := 7; i >= 0; i-- {
		hi := z & 0x80
		z <<= 1
		if hi != 0 {
			z ^= 0x1D
		}
		if (y>>uint(i))&1 != 0 {
			z ^= x
		}
	}
	return z
}

func rsGenerator(degree int) []byte {
	result := make([]byte, degree)
	result[degree-1] = 1
	var root byte = 1
	for range degree {
		for j := range degree {
			result[j] = gfMultiply(result[j], root)
			if j+1 < degree {
				result[j] ^= result[j+1]
			}
		}
		root = gfMultiply(root, 0x02)
	}
	return result
}

func rsRemainder(data, gen []byte) []byte {
	rem := make([]byte, len(gen))
	for _, b := range data {
		factor := b ^ rem[0]
		copy(rem, rem[1:])
		rem[len(rem)-1] = 0
		for i, coef := range gen {
			rem[i] ^= gfMultiply(coef, factor)
		}
	}
	return rem
}

func setFunc(modules, function []bool, size, x, y int, dark bool) {
	if x < 0 || y < 0 || x >= size || y >= size {
		return
	}
	modules[y*size+x] = dark
	function[y*size+x] = true
}

func placeFunctionPatterns(modules, function []bool, size int, spec versionSpec) {
	// Timing patterns
	for i := range size {
		setFunc(modules, function, size, 6, i, i%2 == 0)
		setFunc(modules, function, size, i, 6, i%2 == 0)
	}

	// Finder patterns + separators
	placeFinder := func(cx, cy int) {
		for dy := -4; dy <= 4; dy++ {
			for dx := -4; dx <= 4; dx++ {
				dist := max(absInt(dx), absInt(dy))
				dark := dist != 2 && dist != 4
				setFunc(modules, function, size, cx+dx, cy+dy, dark)
			}
		}
	}
	placeFinder(3, 3)
	placeFinder(size-4, 3)
	placeFinder(3, size-4)

	// Alignment patterns
	for i, cy := range spec.alignment {
		for j, cx := range spec.alignment {
			if (i == 0 && j == 0) || (i == 0 && j == len(spec.alignment)-1) || (i == len(spec.alignment)-1 && j == 0) {
				continue
			}
			for dy := -2; dy <= 2; dy++ {
				for dx := -2; dx <= 2; dx++ {
					dist := max(absInt(dx), absInt(dy))
					setFunc(modules, function, size, cx+dx, cy+dy, dist != 1)
				}
			}
		}
	}

	// Reserve format info areas (filled after masking)
	for i := range 9 {
		if i != 6 {
			setFunc(modules, function, size, 8, i, false)
			setFunc(modules, function, size, i, 8, false)
		}
	}
	for i := range 8 {
		setFunc(modules, function, size, size-1-i, 8, false)
		setFunc(modules, function, size, 8, size-1-i, false)
	}
	// Fixed dark module
	setFunc(modules, function, size, 8, size-8, true)

	// Reserve version info areas for versions >= 7
	if spec.version >= 7 {
		for i := range 6 {
			for j := range 3 {
				setFunc(modules, function, size, size-11+j, i, false)
				setFunc(modules, function, size, i, size-11+j, false)
			}
		}
	}
}

func placeDataBits(modules, function []bool, size int, codewords []byte) {
	bitIndex := 0
	totalBits := len(codewords) * 8
	for right := size - 1; right >= 1; right -= 2 {
		if right == 6 {
			right = 5
		}
		for vert := range size {
			for j := range 2 {
				x := right - j
				upward := ((right + 1) & 2) == 0
				y := vert
				if upward {
					y = size - 1 - vert
				}
				if !function[y*size+x] {
					if bitIndex < totalBits {
						dark := ((codewords[bitIndex>>3] >> uint(7-(bitIndex&7))) & 1) != 0
						modules[y*size+x] = dark
						bitIndex++
					}
				}
			}
		}
	}
}

func applyMask(modules, function []bool, size, mask int) {
	for y := range size {
		for x := range size {
			if function[y*size+x] {
				continue
			}
			var invert bool
			switch mask {
			case 0:
				invert = (x+y)%2 == 0
			case 1:
				invert = y%2 == 0
			case 2:
				invert = x%3 == 0
			case 3:
				invert = (x+y)%3 == 0
			case 4:
				invert = (x/3+y/2)%2 == 0
			case 5:
				invert = (x*y)%2+(x*y)%3 == 0
			case 6:
				invert = ((x*y)%2+(x*y)%3)%2 == 0
			case 7:
				invert = ((x+y)%2+(x*y)%3)%2 == 0
			}
			if invert {
				modules[y*size+x] = !modules[y*size+x]
			}
		}
	}
}

func formatBitsLevelL(mask int) uint32 {
	// Format data: EC level L = 01, followed by 3-bit mask
	data := uint32((0b01 << 3) | mask)
	rem := data
	for range 10 {
		rem = (rem << 1) ^ ((rem >> 9) * 0x537)
	}
	return ((data << 10) | rem) ^ 0x5412
}

func placeFormatBits(modules []bool, size, mask int) {
	bits := formatBitsLevelL(mask)
	bit := func(i int) bool { return ((bits >> uint(i)) & 1) != 0 }

	// First copy around top-left finder
	for i := range 6 {
		modules[i*size+8] = bit(i)
	}
	modules[7*size+8] = bit(6)
	modules[8*size+8] = bit(7)
	modules[8*size+7] = bit(8)
	for i := 9; i < 15; i++ {
		modules[8*size+(14-i)] = bit(i)
	}

	// Second copy around top-right and bottom-left finders
	for i := range 8 {
		modules[8*size+(size-1-i)] = bit(i)
	}
	for i := 8; i < 15; i++ {
		modules[(size-15+i)*size+8] = bit(i)
	}
	modules[(size-8)*size+8] = true
}

func versionBits(version int) uint32 {
	rem := uint32(version)
	for range 12 {
		rem = (rem << 1) ^ ((rem >> 11) * 0x1F25)
	}
	return (uint32(version) << 12) | rem
}

func placeVersionBits(modules []bool, size, version int) {
	bits := versionBits(version)
	for i := range 18 {
		dark := ((bits >> uint(i)) & 1) != 0
		a := size - 11 + i%3
		b := i / 3
		modules[b*size+a] = dark
		modules[a*size+b] = dark
	}
}

func penaltyScore(modules []bool, size int) int {
	score := 0
	// Rule 1: runs of 5+ same-colored modules in rows and columns
	for y := range size {
		runColor := modules[y*size]
		runLen := 1
		for x := 1; x < size; x++ {
			if modules[y*size+x] == runColor {
				runLen++
				if runLen == 5 {
					score += 3
				} else if runLen > 5 {
					score++
				}
			} else {
				runColor = modules[y*size+x]
				runLen = 1
			}
		}
	}
	for x := range size {
		runColor := modules[x]
		runLen := 1
		for y := 1; y < size; y++ {
			if modules[y*size+x] == runColor {
				runLen++
				if runLen == 5 {
					score += 3
				} else if runLen > 5 {
					score++
				}
			} else {
				runColor = modules[y*size+x]
				runLen = 1
			}
		}
	}
	// Rule 2: 2x2 blocks of same color
	for y := range size - 1 {
		for x := range size - 1 {
			c := modules[y*size+x]
			if modules[y*size+x+1] == c && modules[(y+1)*size+x] == c && modules[(y+1)*size+x+1] == c {
				score += 3
			}
		}
	}
	// Rule 3: finder-like patterns in rows and columns
	matchFinderLike := func(line []bool) int {
		hits := 0
		for i := 0; i+11 <= len(line); i++ {
			// 10111010000 or 00001011101
			if line[i] && !line[i+1] && line[i+2] && line[i+3] && line[i+4] && !line[i+5] && line[i+6] && !line[i+7] && !line[i+8] && !line[i+9] && !line[i+10] {
				hits += 40
			}
			if !line[i] && !line[i+1] && !line[i+2] && !line[i+3] && line[i+4] && !line[i+5] && line[i+6] && line[i+7] && line[i+8] && !line[i+9] && line[i+10] {
				hits += 40
			}
		}
		return hits
	}
	col := make([]bool, size)
	for y := range size {
		score += matchFinderLike(modules[y*size : (y+1)*size])
	}
	for x := range size {
		for y := range size {
			col[y] = modules[y*size+x]
		}
		score += matchFinderLike(col)
	}
	// Rule 4: balance of dark modules
	dark := 0
	for _, m := range modules {
		if m {
			dark++
		}
	}
	total := size * size
	k := absInt(dark*20-total*10) / total
	score += k * 10
	return score
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
