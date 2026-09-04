package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"  // register GIF decoder
	_ "image/jpeg" // register JPEG decoder
	"image/png"
	_ "image/png" // register PNG decoder
	"os"
	"path/filepath"
)

const (
	// Splash image types, stored at header offset 16.
	typeRaw   = 0 // raw BGR24 payload
	typeRLE24 = 1 // RLE24 compressed payload
)

// Get header
func GetImgHeader(size image.Point, compressed int, real_bytes uint32) []byte {
	SECTOR_SIZE_IN_BYTES := 512
	header := make([]byte, SECTOR_SIZE_IN_BYTES)

	width, height := size.X, size.Y
	real_size := (real_bytes + 511) / 512

	// magic
	header[0] = 'S'
	header[1] = 'P'
	header[2] = 'L'
	header[3] = 'A'
	header[4] = 'S'
	header[5] = 'H'
	header[6] = '!'
	header[7] = '!'

	// width
	header[8] = byte(width & 0xFF)
	header[9] = byte((width >> 8) & 0xFF)
	header[10] = byte((width >> 16) & 0xFF)
	header[11] = byte((width >> 24) & 0xFF)

	// height
	header[12] = byte(height & 0xFF)
	header[13] = byte((height >> 8) & 0xFF)
	header[14] = byte((height >> 16) & 0xFF)
	header[15] = byte((height >> 24) & 0xFF)

	// type
	header[16] = byte(compressed & 0xFF)
	//header[17]= 0
	//header[18]= 0
	//header[19]= 0

	// block number
	header[20] = byte(real_size & 0xFF)
	header[21] = byte((real_size >> 8) & 0xFF)
	header[22] = byte((real_size >> 16) & 0xFF)
	header[23] = byte((real_size >> 24) & 0xFF)

	output := new(bytes.Buffer)
	for _, i := range header {
		output.WriteByte(i)
	}
	content := output.Bytes()
	return content
}

type LineType struct {
	R byte
	G byte
	B byte
}

type RunLengthType struct {
	run  int
	line []LineType
}

func encode(line []LineType) []RunLengthType {
	count := 0
	lst := make([]RunLengthType, 0)
	repeat := -1
	run := make([]LineType, 0)
	total := len(line) - 1

	// iterate line[:total], so line[index+1] never goes out of range
	for index, current := range line[:total] {
		if current != line[index+1] {
			run = append(run, current)
			count += 1
			if repeat == 1 {
				entry := RunLengthType{run: count + 128, line: run}
				lst = append(lst, entry)
				count = 0
				run = make([]LineType, 0)
				repeat = -1
				if index == total-1 {
					run = append(run, line[index+1])
					entry := RunLengthType{run: 1, line: run}
					lst = append(lst, entry)
				}
			} else {
				repeat = 0

				if count == 128 {
					entry := RunLengthType{run: count, line: run}
					lst = append(lst, entry)
					count = 0
					run = make([]LineType, 0)
					repeat = -1
				}
				if index == total-1 {
					run = append(run, line[index+1])
					entry := RunLengthType{run: count + 1, line: run}
					lst = append(lst, entry)
				}
			}
		} else {
			if repeat == 0 {
				entry := RunLengthType{run: count, line: run}
				lst = append(lst, entry)
				count = 0
				run = make([]LineType, 0)
				repeat = -1
				if index == total-1 {
					run = append(run, line[index+1], line[index+1])
					entry := RunLengthType{run: 2 + 128, line: run}
					lst = append(lst, entry)
					break
				}
			}
			run = append(run, current)
			repeat = 1
			count += 1
			if count == 128 {
				entry := RunLengthType{run: 256, line: run}
				lst = append(lst, entry)
				count = 0
				run = make([]LineType, 0)
				repeat = -1
			}
			if index == total-1 {
				if count == 0 {
					run = append(run, line[index+1])
					entry := RunLengthType{run: 1, line: run}
					lst = append(lst, entry)
				} else {
					run = append(run, current)
					entry := RunLengthType{run: count + 1 + 128, line: run}
					lst = append(lst, entry)
				}
			}
		}
	}
	return lst
}

func encodeRLE24(img image.Image) []byte {
	width, height := img.Bounds().Dx(), img.Bounds().Dy()
	output := new(bytes.Buffer)
	for h := range height {
		line := make([]LineType, width)
		result := make([]RunLengthType, 0)
		for w := range width {
			r, g, b, _ := img.At(w, h).RGBA()
			line[w] = LineType{R: byte(r >> 8), G: byte(g >> 8), B: byte(b >> 8)}
		}
		result = encode(line)
		for _, rle := range result {
			count, pixel := rle.run, rle.line
			output.WriteByte(byte(count - 1))
			if count > 128 {
				output.WriteByte(pixel[0].B)
				output.WriteByte(pixel[0].G)
				output.WriteByte(pixel[0].R)
			} else {
				for item := range pixel {
					output.WriteByte(pixel[item].B)
					output.WriteByte(pixel[item].G)
					output.WriteByte(pixel[item].R)
				}
			}
		}
	}
	content := output.Bytes()
	return content
}

// Get payload data: BGR Interleaved
func GetImageBody(img image.Image, compressed int) []byte {
	if compressed == 1 {
		return encodeRLE24(img)
	} else {
		buffer := new(bytes.Buffer)
		bounds := img.Bounds()

		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				r, g, b, _ := img.At(x, y).RGBA()
				// Convert from 16-bit to 8-bit
				buffer.WriteByte(byte(b >> 8))
				buffer.WriteByte(byte(g >> 8))
				buffer.WriteByte(byte(r >> 8))
			}
		}
		return buffer.Bytes()
	}
}

func MakeLogoImage(logo string, out string, imgType int) error {
	fd, err := os.Open(logo)
	if err != nil {
		return fmt.Errorf("failed to open logo file: %w", err)
	}
	defer fd.Close()

	img, _, err := image.Decode(fd)
	if err != nil {
		return fmt.Errorf("failed to decode logo file: %w", err)
	}

	// imgType: 0 = raw BGR24, 1 = RLE24 compressed.
	body := GetImageBody(img, imgType)
	header := GetImgHeader(img.Bounds().Size(), imgType, uint32(len(body)))

	fdout, err := os.Create(out)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer fdout.Close()

	if _, err = fdout.Write(header); err != nil {
		return fmt.Errorf("failed to write header: %w", err)
	}
	if _, err = fdout.Write(body); err != nil {
		return fmt.Errorf("failed to write body: %w", err)
	}
	return nil
}

// =============================== unpack ===============================

const (
	defaultInFile = "logo.png"
	defaultImgOut = "splash.img"
	defaultPngOut = "splash.png"
)

// SplashHeader mirrors "struct logo_header" of the SPLASH!! format.
type SplashHeader struct {
	Width  int
	Height int
	Type   int
	Blocks int
}

// ParseSplashHeader validates the 512-byte header and extracts its fields.
func ParseSplashHeader(data []byte) (*SplashHeader, error) {
	if len(data) < 512 {
		return nil, fmt.Errorf("file too small: %d bytes (need >= 512)", len(data))
	}
	if string(data[:8]) != "SPLASH!!" {
		return nil, errors.New("bad magic: not a SPLASH!! image")
	}
	hdr := &SplashHeader{
		Width:  int(binary.LittleEndian.Uint32(data[8:12])),
		Height: int(binary.LittleEndian.Uint32(data[12:16])),
		Type:   int(data[16]),
		Blocks: int(binary.LittleEndian.Uint32(data[20:24])),
	}
	if hdr.Width <= 0 || hdr.Height <= 0 {
		return nil, fmt.Errorf("invalid dimensions: %dx%d", hdr.Width, hdr.Height)
	}
	if hdr.Type != typeRaw && hdr.Type != typeRLE24 {
		return nil, fmt.Errorf("unsupported image type %d (want 0=raw or 1=RLE24)", hdr.Type)
	}
	return hdr, nil
}

// decodeRLE24 expands the RLE24 payload into width*height RGB24 pixels.
//
// The payload is organised per scanline; every segment starts with a count
// byte b, from which the real run length is n = b+1:
//
//	n <= 128  -> literal segment: n distinct pixels follow (3 bytes each)
//	n >  128  -> run segment: one pixel follows, repeated (n-128) times
//
// Pixels are stored as B,G,R. The encoder never splits a segment across two
// scanlines, so we simply decode each row until it is full.
func decodeRLE24(payload []byte, width, height int) ([]byte, error) {
	rgb := make([]byte, width*height*3)
	pos := 0
	for y := 0; y < height; y++ {
		base := y * width * 3
		for x := 0; x < width; {
			if pos >= len(payload) {
				return nil, fmt.Errorf("truncated RLE24 payload at row %d, pixel %d (offset %d)", y, x, pos)
			}
			n := int(payload[pos]) + 1
			pos++
			if n > 128 { // run segment: one pixel repeated (n-128) times
				if pos+3 > len(payload) {
					return nil, fmt.Errorf("truncated RLE24 run at row %d, pixel %d (offset %d)", y, x, pos)
				}
				b, g, r := payload[pos], payload[pos+1], payload[pos+2]
				pos += 3
				for i := 0; i < n-128; i++ {
					if x >= width {
						return nil, fmt.Errorf("RLE24 run overflows row %d at pixel %d", y, x)
					}
					off := base + x*3
					rgb[off], rgb[off+1], rgb[off+2] = r, g, b
					x++
				}
			} else { // literal segment: n pixels follow
				need := n * 3
				if pos+need > len(payload) {
					return nil, fmt.Errorf("truncated RLE24 literal at row %d, pixel %d (offset %d)", y, x, pos)
				}
				for i := 0; i < n; i++ {
					if x >= width {
						return nil, fmt.Errorf("RLE24 literal overflows row %d at pixel %d", y, x)
					}
					off := base + x*3
					// stored as B,G,R -> convert to R,G,B
					rgb[off+2], rgb[off+1], rgb[off] = payload[pos], payload[pos+1], payload[pos+2]
					pos += 3
					x++
				}
			}
		}
	}
	return rgb, nil
}

// decodeRaw converts a raw BGR payload into width*height RGB24 pixels.
func decodeRaw(payload []byte, width, height int) ([]byte, error) {
	want := width * height * 3
	if len(payload) < want {
		return nil, fmt.Errorf("raw payload truncated: need %d bytes, have %d", want, len(payload))
	}
	rgb := make([]byte, want)
	for i := 0; i < want; i += 3 {
		// stored as B,G,R -> convert to R,G,B
		rgb[i+2], rgb[i+1], rgb[i] = payload[i], payload[i+1], payload[i+2]
	}
	return rgb, nil
}

// SavePNG writes an RGB24 buffer into a PNG file.
func SavePNG(filename string, rgb []byte, width, height int) error {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			off := (y*width + x) * 3
			img.SetRGBA(x, y, color.RGBA{R: rgb[off], G: rgb[off+1], B: rgb[off+2], A: 255})
		}
	}
	out, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer out.Close()
	if err := png.Encode(out, img); err != nil {
		return fmt.Errorf("failed to encode PNG: %w", err)
	}
	return nil
}

// UnpackSplash reads a splash image and writes the decoded image as a PNG.
func UnpackSplash(infile string, outfile string) error {
	data, err := os.ReadFile(infile)
	if err != nil {
		return fmt.Errorf("failed to open splash image: %w", err)
	}
	hdr, err := ParseSplashHeader(data)
	if err != nil {
		return err
	}
	payload := data[512:]
	var rgb []byte
	if hdr.Type == typeRLE24 {
		rgb, err = decodeRLE24(payload, hdr.Width, hdr.Height)
	} else {
		rgb, err = decodeRaw(payload, hdr.Width, hdr.Height)
	}
	if err != nil {
		return err
	}
	return SavePNG(outfile, rgb, hdr.Width, hdr.Height)
}

// =============================== CLI ===============================

func progName() string {
	return filepath.Base(os.Args[0])
}

func showUsage() {
	fmt.Printf(`usage:
  %[1]s pack   [logo.png]   [-o splash.img] [-t raw|rle]   encode an image into a splash image
  %[1]s unpack [splash.img] [-o splash.png]                 decode a splash image into a PNG
  %[1]s help                                                show this help

options:
  -o, --out <file>   output file (default: splash.img / splash.png)
  -t, --type <fmt>   pack payload format: raw or rle (default: rle)

with no arguments, "%[1]s" behaves like "%[1]s pack logo.png"
`, progName())
}

// parseFlags extracts options (-o/--out, -t/--type, plus their =value forms)
// from args and returns the output path, the pack format and remaining
// positional arguments. An empty format means "use the default (rle)".
func parseFlags(args []string) (out string, format string, rest []string, err error) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-o" || a == "--out":
			if i+1 >= len(args) {
				return "", "", nil, errors.New("option " + a + " requires a file path")
			}
			out = args[i+1]
			i++
		case len(a) > 3 && a[:3] == "-o=":
			out = a[3:]
		case len(a) > 6 && a[:6] == "--out=":
			out = a[6:]
		case a == "-t" || a == "--type":
			if i+1 >= len(args) {
				return "", "", nil, errors.New("option " + a + " requires a value (raw or rle)")
			}
			format = args[i+1]
			i++
		case len(a) > 3 && a[:3] == "-t=":
			format = a[3:]
		case len(a) > 7 && a[:7] == "--type=":
			format = a[7:]
		case a[0] == '-':
			return "", "", nil, fmt.Errorf("unknown option %q", a)
		default:
			rest = append(rest, a)
		}
	}
	return out, format, rest, nil
}

// parseImgType converts a format name into the splash image type constant.
func parseImgType(format string) (int, error) {
	switch format {
	case "raw", "0":
		return typeRaw, nil
	case "rle", "rle24", "1":
		return typeRLE24, nil
	default:
		return 0, fmt.Errorf("invalid pack type %q (want raw or rle)", format)
	}
}

func runPack(args []string) int {
	out, format, rest, err := parseFlags(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		showUsage()
		return 2
	}
	if len(rest) > 1 {
		fmt.Fprintln(os.Stderr, "error: too many arguments")
		showUsage()
		return 2
	}
	in := defaultInFile
	if len(rest) == 1 {
		in = rest[0]
	}
	if out == "" {
		out = defaultImgOut
	}
	// Default to RLE24, matching the reference implementation.
	imgType := typeRLE24
	if format != "" {
		imgType, err = parseImgType(format)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 2
		}
	}
	if err := MakeLogoImage(in, out, imgType); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	typeName := "raw"
	if imgType == typeRLE24 {
		typeName = "rle24"
	}
	fmt.Printf("packed %s -> %s (%s)\n", in, out, typeName)
	return 0
}

func runUnpack(args []string) int {
	out, _, rest, err := parseFlags(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		showUsage()
		return 2
	}
	if len(rest) > 1 {
		fmt.Fprintln(os.Stderr, "error: too many arguments")
		showUsage()
		return 2
	}
	in := defaultImgOut
	if len(rest) == 1 {
		in = rest[0]
	}
	if out == "" {
		out = defaultPngOut
	}
	if err := UnpackSplash(in, out); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	fmt.Printf("unpacked %s -> %s\n", in, out)
	return 0
}

func main() {
	args := os.Args[1:]

	// Optional leading sub-command.
	if len(args) > 0 {
		switch args[0] {
		case "pack":
			os.Exit(runPack(args[1:]))
		case "unpack", "extract":
			os.Exit(runUnpack(args[1:]))
		case "help", "-h", "--help":
			showUsage()
			return
		}
	}

	// Legacy / default form: logo_gen [logo.png]  ==  pack.
	os.Exit(runPack(args))
}
