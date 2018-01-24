package vnc2video

import (
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"io"
	"vnc2video/logger"
)

func Min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func FillRect(img draw.Image, rect *image.Rectangle, c color.Color) {
	for x := rect.Min.X; x < rect.Max.X; x++ {
		for y := rect.Min.Y; y < rect.Max.Y; y++ {
			img.Set(x, y, c)
		}
	}
}

func readRunLength(r io.Reader) (int, error) {
	runLen := 1

	mod, err := ReadUint8(r)
	if err != nil {
		logger.Errorf("renderZRLE: error while reading mod in plain RLE subencoding: %v", err)
		return 0, err
	}
	runLen += int(mod)

	for mod == 255 {
		//mod = fromZlib.read();
		mod, err = ReadUint8(r)
		if err != nil {
			logger.Errorf("renderZRLE: error while reading mod in-loop plain RLE subencoding: %v", err)
			return 0, err
		}
		runLen += int(mod)
	}
	return runLen, nil
}

// Read unmarshal color from conn
func ReadColor(c io.Reader, pf *PixelFormat) (*color.RGBA, error) {
	if pf.TrueColor == 0 {
		return nil, errors.New("support for non true color formats was not implemented")
	}
	order := pf.order()
	var pixel uint32

	switch pf.BPP {
	case 8:
		var px uint8
		if err := binary.Read(c, order, &px); err != nil {
			return nil, err
		}
		pixel = uint32(px)
	case 16:
		var px uint16
		if err := binary.Read(c, order, &px); err != nil {
			return nil, err
		}
		pixel = uint32(px)
	case 32:
		var px uint32
		if err := binary.Read(c, order, &px); err != nil {
			return nil, err
		}
		pixel = uint32(px)
	}

	rgb := color.RGBA{
		R: uint8((pixel >> pf.RedShift) & uint32(pf.RedMax)),
		G: uint8((pixel >> pf.GreenShift) & uint32(pf.GreenMax)),
		B: uint8((pixel >> pf.BlueShift) & uint32(pf.BlueMax)),
		A: 1,
	}

	return &rgb, nil
}

func DecodeRaw(reader io.Reader, pf *PixelFormat, rect *Rectangle, targetImage draw.Image) error {
	for y := 0; y < int(rect.Height); y++ {
		for x := 0; x < int(rect.Width); x++ {
			col, err := ReadColor(reader, pf)
			if err != nil {
				return err
			}

			targetImage.(draw.Image).Set(int(rect.X)+x, int(rect.Y)+y, col)
		}
	}

	return nil
}

func ReadUint8(r io.Reader) (uint8, error) {
	var myUint uint8
	if err := binary.Read(r, binary.BigEndian, &myUint); err != nil {
		return 0, err
	}

	return myUint, nil
}
func ReadUint16(r io.Reader) (uint16, error) {
	var myUint uint16
	if err := binary.Read(r, binary.BigEndian, &myUint); err != nil {
		return 0, err
	}

	return myUint, nil
}

func ReadUint32(r io.Reader) (uint32, error) {
	var myUint uint32
	if err := binary.Read(r, binary.BigEndian, &myUint); err != nil {
		return 0, err
	}

	return myUint, nil
}

func MakeRect(x, y, width, height int) image.Rectangle {
	return image.Rectangle{Min: image.Point{X: x, Y: y}, Max: image.Point{X: x + width, Y: y + height}}
}
func MakeRectFromVncRect(rect *Rectangle) image.Rectangle {
	return MakeRect(int(rect.X), int(rect.Y), int(rect.Width), int(rect.Height))
}
