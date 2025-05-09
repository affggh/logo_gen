package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"log"
	"os"
	"path"
	"strings"
)

const HEADER_SIZE int = 512
const SUPPORT_RLE24_COMPRESSIONT bool = true

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
					lst = append(lst, Entry{count, run})
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
		Blocks: uint32(real_bytes),
	}
	buffer := make([]byte, binary.Size(header))
	binary.Encode(buffer, binary.LittleEndian, &header)
	return buffer
}

func MakeLogoImage(logo, out string) {
	img, err := GetImage(logo)
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
}

func main() {
	MakeLogoImage("logo.png", "splash.img")
}
