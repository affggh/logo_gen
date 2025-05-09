package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"log"
	"os"
	"path"
	"strings"
)

const HEADER_SIZE int = 512

var SUPPORT_RLE24_COMPRESSIONT bool = true

type SplashHdr struct {
	Magic  [8]byte
	Width  uint32
	Height uint32
	Type   uint32
	Blocks uint32
	_      [HEADER_SIZE - 24]byte
}

func (s *SplashHdr) Decode(data []byte) error {
	if len(data) < HEADER_SIZE {
		return errors.New("header size mismatch")
	}

	_, err := binary.Decode(data[:HEADER_SIZE], binary.LittleEndian, s)

	return err
}

func (s *SplashHdr) Encode() []byte {
	buf := make([]byte, HEADER_SIZE)

	_, err := binary.Encode(buf, binary.LittleEndian, s)
	if err != nil {
		log.Println("Could not encode new data")
	}

	return buf
}

func GetImage(file string) (image.Image, error) {
	fd, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer fd.Close()

	ext := strings.ToLower(path.Ext(file))

	var img image.Image
	switch ext {
	case ".png":
		img, err = png.Decode(fd)
	case ".jpg", ".jpeg":
		img, err = jpeg.Decode(fd)
	default:
		img, _, err = image.Decode(fd)
	}

	if err != nil {
		return nil, err
	}

	//log.Println("Parsing image format", format)

	return img, nil
}

// Get BGR format image
func GetImageRaw(img image.Image) []byte {
	size := img.Bounds().Size()
	data := make([]byte, size.X*size.Y*3)

	curr := 0
	for h := 0; h < size.Y; h++ {
		for w := 0; w < size.X; w++ {
			// convert to BGR
			r, g, b, _ := img.At(w, h).RGBA()

			data[curr+2] = uint8(r >> 0x8)
			data[curr+1] = uint8(g >> 0x8)
			data[curr] = uint8(b >> 0x8)
		}
	}

	return data
}

func GetImageFileRaw(file string) ([]byte, error) {
	img, err := GetImage(file)
	if err != nil {
		return nil, err
	}

	return GetImageRaw(img), nil
}

type Entry struct {
	Count int
	Pix   []uint32
}

func EncodeLine(line []uint32) []Entry {
	count := 0
	lst := make([]Entry, 0)
	repeat := -1
	run := make([]uint32, 0)
	total := len(line) - 1

	for index, current := range line[:len(line)-1] {
		if current != line[index+1] {
			run = append(run, current)
			count += 1
			if repeat == 1 {
				lst = append(lst, Entry{count + 128, run})
				count = 0
				run = make([]uint32, 0)
				repeat = -1
				if index == total-1 {
					run = []uint32{line[index+1]}
					lst = append(lst, Entry{1, run})
				}
			} else {
				repeat = 0

				if count == 128 {
					lst = append(lst, Entry{128, run})
					count = 0
					run = make([]uint32, 0)
					repeat = -1
				}
				if index == total-1 {
					run = append(run, line[index+1])
					lst = append(lst, Entry{count + 1, run})
				}
			}
		} else {
			if repeat == 0 {
				lst = append(lst, Entry{count, run})
				count = 0
				run = make([]uint32, 0)
				repeat = -1
				if index == total-1 {
					run = append(run, line[index+1], line[index+1])
					lst = append(lst, Entry{2 + 128, run})
					break
				}
			}
			run = append(run, current)
			repeat = 1
			count++
			if count == 128 {
				lst = append(lst, Entry{256, run})
				count = 0
				run = make([]uint32, 0)
				repeat = -1
			}
			if index == total-1 {
				if count == 0 {
					run = []uint32{line[index+1]}
					lst = append(lst, Entry{1, run})
				} else {
					run = append(run, current)
					lst = append(lst, Entry{count + 1 + 128, run})
				}
			}

		}
	}
	return lst
}

func EncodeRLE24(img image.Image) []byte {
	buf := new(bytes.Buffer)

	size := img.Bounds().Size()
	width, height := size.X, size.Y

	for h := 0; h < height; h++ {
		line := make([]uint32, 0)
		result := make([]Entry, 0)
		for w := 0; w < width; w++ {
			r, g, b, _ := img.At(w, h).RGBA()
			line = append(line, (r>>8)<<16|(g>>8)<<8|b>>8)
		}
		result = EncodeLine(line)
		for _, entry := range result {
			buf.WriteByte(uint8(entry.Count - 1))
			if entry.Count > 128 {
				buf.WriteByte(uint8(entry.Pix[0] & 0xFF))
				buf.WriteByte(uint8((entry.Pix[0] >> 8) & 0xFF))
				buf.WriteByte(uint8((entry.Pix[0] >> 16) & 0xFF))
			} else {
				for _, item := range entry.Pix {
					buf.WriteByte(uint8(item & 0xFF))
					buf.WriteByte(uint8((item >> 8) & 0xFF))
					buf.WriteByte(uint8((item >> 16) & 0xFF))
				}
			}
		}
	}
	return buf.Bytes()
}

func GetImageBody(img image.Image, compresseed bool) []byte {
	//background := GetImageRaw(img)

	if compresseed {
		return EncodeRLE24(img)
	} else {
		return GetImageRaw(img) // Already BGR format
	}
}

func GetImageHeader(size image.Point, compressed bool, real_bytes int) []byte {
	header := SplashHdr{
		Magic:  [8]byte{'S', 'P', 'L', 'A', 'S', 'H', '!', '!'},
		Width:  uint32(size.X),
		Height: uint32(size.Y),
		Type: func() uint32 {
			if compressed {
				return 1
			}
			return 0
		}(),
		Blocks: uint32((real_bytes + 511) / 512),
	}
	buffer := make([]byte, binary.Size(header))
	binary.Encode(buffer, binary.LittleEndian, &header)
	return buffer
}

func MakeLogoImage(logo, out string) {
	img, err := GetImage(logo)
	println("Parsing image:", logo, "->", out)
	println("Width:", img.Bounds().Size().X, "Height", img.Bounds().Size().Y)
	println("SUPPORT_RLE24_COMPRESIONT:", SUPPORT_RLE24_COMPRESSIONT)
	if err != nil {
		log.Fatalln(err)
	}
	fd, err := os.Create(out)
	if err != nil {
		log.Fatalln(err)
	}
	defer fd.Close()

	body := GetImageBody(img, SUPPORT_RLE24_COMPRESSIONT)
	fd.Write(GetImageHeader(img.Bounds().Size(), SUPPORT_RLE24_COMPRESSIONT, len(body)))
	fd.Write(body)

	println("Done!")
	//fd.Truncate(int64(HEADER_SIZE + ((len(body)+511)/512)*512))
}

func BGR2Img(data []byte, width, height int) image.Image {
	if len(data) < width*height*3 {
		log.Fatalln("Size not equle: except:", width*height*3, "buf:", len(data))
	}
	cur := 0
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for h := 0; h < height; h++ {
		for w := 0; w < width; w++ {
			img.Set(w, h, color.RGBA{
				data[cur+2],
				data[cur+1],
				data[cur],
				0xFF,
			})
			cur += 3
		}
	}
	return img
}

func ExtractLogoImage(splash, out string) {
	save_img := func(img image.Image, outfile string) {
		od, err := os.Create(outfile)
		if err != nil {
			log.Fatalln(err)
		}
		err = png.Encode(od, img)
		if err != nil {
			log.Fatalln(err)
		}
	}

	fd, err := os.Open(splash)
	if err != nil {
		log.Fatalln(err)
	}
	defer fd.Close()

	hdr := SplashHdr{}
	err = binary.Read(fd, binary.LittleEndian, &hdr)
	if err != nil {
		log.Fatalln(err)
	}

	println("Parsing:", splash, "->", out)
	println("Width:", hdr.Width, "Height", hdr.Height)
	println("Encoded:", hdr.Type == 1)

	data, err := io.ReadAll(fd)
	if err != nil {
		log.Fatalln(err)
	}

	offset := 0
	data_len := len(data)
	buffer := new(bytes.Buffer)
	if hdr.Type == 1 { // RLE24 data
		for offset < data_len {
			count := int(data[offset]) + 1
			offset++
			if count > 128 {
				repeatCount := count - 128
				if offset+3 > data_len {
					log.Fatalf("unexpected end of data during RLE repeat")
				}
				buffer.Write(bytes.Repeat(data[offset:offset+3], repeatCount))
				offset += 3
			} else {
				byteCount := count * 3
				if offset+byteCount > data_len {
					log.Fatalf("unexpected end of data during RLE raw block")
				}
				buffer.Write(data[offset : offset+byteCount])
				offset += byteCount
			}
		}
	} else { // Raw data
		buffer.Write(data)
	}

	img := BGR2Img(buffer.Bytes(), int(hdr.Width), int(hdr.Height))

	save_img(img, out)
	println("Done!")
}

func main() {
	if len(os.Args) < 2 {
		println("Usage:")
		println(os.Args[0], "encode", "logo.png", "splash.img")
		println(os.Args[0], "decode", "splash.img", "logo.png")

		println("You can set environment:RLE24=0 to make image raw")
	}

	if os.Getenv("RLE24") == "0" {
		SUPPORT_RLE24_COMPRESSIONT = false
	}

	if len(os.Args) > 1 {
		command := os.Args[1]

		switch command {
		case "encode":
			if len(os.Args) == 4 {
				MakeLogoImage(os.Args[2], os.Args[3])
			} else {
				println("Invalid use!")
				os.Exit(1)
			}
		case "decode":
			if len(os.Args) == 4 {
				ExtractLogoImage(os.Args[2], os.Args[3])
			} else {
				println("Invalid use!")
				os.Exit(1)
			}
		}
	}
}
