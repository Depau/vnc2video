package vnc2webm

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"io"
	"math"
	"os"
	"strconv"
	"vnc2webm/logger"
)

//go:generate stringer -type=TightCompression

type TightCompression uint8

const (
	TightCompressionBasic = 0
	TightCompressionFill  = 8
	TightCompressionJPEG  = 9
	TightCompressionPNG   = 10
)

//go:generate stringer -type=TightFilter

type TightFilter uint8

const (
	TightFilterCopy     = 0
	TightFilterPalette  = 1
	TightFilterGradient = 2
)

type TightEncoding struct {
	Image        image.Image
	decoders     []io.Reader
	decoderBuffs []*bytes.Reader
}

var instance *TightEncoding
var TightMinToCompress int = 12

func (*TightEncoding) Supported(Conn) bool {
	return true
}

func (*TightEncoding) Type() EncodingType { return EncTight }

func (*TightEncoding) GetInstance() *TightEncoding {
	if instance == nil {
		instance = &TightEncoding{}
	}
	return instance
}

func (enc *TightEncoding) Write(c Conn, rect *Rectangle) error {
	return nil
}

// Read unmarshal color from conn
func getTightColor(c io.Reader, pf *PixelFormat) (*color.RGBA64, error) {
	if pf.TrueColor == 0 {
		return nil, errors.New("support for non true color formats was not implemented")
	}
	order := pf.order()
	var pixel uint32
	isTightFormat := pf.TrueColor != 0 && pf.Depth == 24 && pf.BPP == 32 && pf.BlueMax <= 255 && pf.RedMax <= 255 && pf.GreenMax <= 255
	if isTightFormat {
		//tbytes := make([]byte, 3)
		tbytes, err := ReadBytes(3, c)
		if err != nil {
			return nil, err
		}
		rgb := color.RGBA64{
			R: uint16(tbytes[0]), //byte(col >> pf.RedShift & int32(pf.RedMax)),
			G: uint16(tbytes[1]), //byte(col >> pf.GreenShift & int32(pf.GreenMax)),
			B: uint16(tbytes[2]), //byte(col >> pf.BlueShift & int32(pf.BlueMax)),
			A: uint16(1),
		}
		return &rgb, nil
	}

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

	rgb := color.RGBA64{
		R: uint16((pixel >> pf.RedShift) & uint32(pf.RedMax)),
		G: uint16((pixel >> pf.GreenShift) & uint32(pf.GreenMax)),
		B: uint16((pixel >> pf.BlueShift) & uint32(pf.BlueMax)),
	}

	// else {
	// 	*clr = clr.cm[pixel]
	// 	clr.cmIndex = pixel
	// }
	return &rgb, nil
}

// func getTightColor(bytes []byte, pf *PixelFormat) color.RGBA {
// 	col := (int32(bytes[0])&0xff)<<16 | (int32(bytes[1])&0xff)<<8 | int32(bytes[2])&0xff
// 	rgb := color.RGBA{
// 		R: byte(col >> pf.RedShift & int32(pf.RedMax)),
// 		G: byte(col >> pf.GreenShift & int32(pf.GreenMax)),
// 		B: byte(col >> pf.BlueShift & int32(pf.BlueMax)),
// 		A: byte(1),
// 	}
// 	return rgb
// }

func calcTightBytePerPixel(pf *PixelFormat) int {
	bytesPerPixel := int(pf.BPP / 8)

	var bytesPerPixelTight int
	if 24 == pf.Depth && 32 == pf.BPP {
		bytesPerPixelTight = 3
	} else {
		bytesPerPixelTight = bytesPerPixel
	}
	return bytesPerPixelTight
}

func (enc *TightEncoding) resetDecoders(compControl uint8) {
	for i := 0; i < 4; i++ {
		if (compControl&1) != 0 && enc.decoders[i] != nil {
			enc.decoders[i] = nil //.(zlib.Resetter).Reset(nil,nil);
		}
		compControl >>= 1
	}
}

var counter int = 0

func (enc *TightEncoding) Read(c Conn, rect *Rectangle) error {

	var out *os.File
	var err error
	////////////
	if counter > 40 {
		os.Exit(1)
	}
	////////////
	pixelFmt := c.PixelFormat()
	bytesPixel := calcTightBytePerPixel(&pixelFmt)
	if enc.Image == nil {
		enc.Image = image.NewRGBA(image.Rect(0, 0, int(c.Width()), int(c.Height())))
	}

	//r.StartByteCollection()

	//r.StartByteCollection()
	// defer func() {
	// 	t.bytes = r.EndByteCollection()
	// }()

	compctl, err := ReadUint8(c)

	/////////////////
	if out == nil {
		out, err = os.Create("./output" + strconv.Itoa(counter) + "-" + strconv.Itoa(int(compctl)) + ".jpg")
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
	}
	defer func() { counter++ }()
	defer jpeg.Encode(out, enc.Image, nil)
	//////////////

	if err != nil {
		logger.Errorf("error in handling tight encoding: %v", err)
		return err
	}
	//logger.Debugf("bytesPixel= %d, subencoding= %d", bytesPixel, compctl)
	enc.resetDecoders(compctl)

	//move it to position (remove zlib flush commands)
	compType := compctl >> 4 & 0x0F

	//logger.Debugf("afterSHL:%d", compType)
	switch compType {
	case TightCompressionFill:
		logger.Debugf("--TIGHT_FILL: reading fill size=%d,counter=%d", bytesPixel, counter)
		//read color
		pf := c.PixelFormat()
		rectColor, err := getTightColor(c, &pf)
		if err != nil {
			logger.Errorf("error in reading tight encoding: %v", err)
			return err
		}

		//logger.Debugf("bytesPixel= %d, compctl= %d, color= %v", bytesPixel, compctl, rectColor)

		//imgRect := image.Rect(0, 0, int(c.Width()), int(c.Height()))
		// if enc.Image == nil {
		// 	enc.Image = image.NewRGBA(imgRect)
		// }

		c1 := color.RGBAModel.Convert(rectColor).(color.RGBA)

		dst := (enc.Image).(*image.RGBA) // enc.Image.(*image.RGBA)
		var x, y int

		for y = int(rect.Y); y < int(rect.Height+rect.Y); y++ {
			for x = int(rect.X); x < int(rect.Width+rect.X); x++ {
				offset := dst.PixOffset(x, y)
				dst.Pix[offset+0] = c1.R
				dst.Pix[offset+1] = c1.G
				dst.Pix[offset+2] = c1.B
				dst.Pix[offset+3] = c1.A

				//dst.Set(int(x), int(y), c1)
				//dst.Pix[y*uint16(dst.Bounds().Max.Y)+x] = []uint8{rectColor.R, rectColor.G, rectColor.B}
			}
		}
		enc.Image = dst

		//draw.Draw(dst, imgRect, &image.Uniform{rectColor}, image.ZP, draw.Src)

		if bytesPixel != 3 {
			return fmt.Errorf("non tight bytesPerPixel format, should be 3 bytes")
		}
		return nil
	case TightCompressionJPEG:
		logger.Debugf("--TIGHT_JPEG,counter=%d", counter)
		if pixelFmt.BPP == 8 {
			return errors.New("Tight encoding: JPEG is not supported in 8 bpp mode")
		}

		len, err := readTightLength(c)

		if err != nil {
			return err
		}
		//logger.Debugf("reading jpeg, size=%d\n", len)
		jpegBytes, err := ReadBytes(len, c)
		if err != nil {
			return err
		}
		//TODO: check if we can read jpeg directly from stream (this is safer for now)
		buff := bytes.NewBuffer(jpegBytes)
		img, err := jpeg.Decode(buff)
		if err != nil {
			logger.Error("problem while decoding jpeg:", err)
		}
		enc.Image = img

		return nil
	default:

		if compType > TightCompressionJPEG {
			logger.Error("Compression control byte is incorrect!")
		}

		enc.handleTightFilters(compctl, &pixelFmt, rect, c)

		return nil
	}
}

func (enc *TightEncoding) handleTightFilters(compCtl uint8, pixelFmt *PixelFormat, rect *Rectangle, r Conn) {
	var STREAM_ID_MASK uint8 = 0x30
	var FILTER_ID_MASK uint8 = 0x40

	var filterid uint8
	var err error

	decoderId := (compCtl & STREAM_ID_MASK) >> 4

	for len(enc.decoders) < 4 {
		enc.decoders = append(enc.decoders, nil)
		enc.decoderBuffs = append(enc.decoderBuffs, nil)
	}

	if (compCtl & FILTER_ID_MASK) > 0 {
		filterid, err = ReadUint8(r)

		if err != nil {
			logger.Errorf("error in handling tight encoding, reading filterid: %v", err)
			return
		}
		//logger.Debugf("handleTightFilters: read filter: %d", filterid)
	}

	bytesPixel := calcTightBytePerPixel(pixelFmt)

	//logger.Debugf("handleTightFilters: filter: %d", filterid)

	lengthCurrentbpp := int(bytesPixel) * int(rect.Width) * int(rect.Height)

	switch filterid {
	case TightFilterPalette: //PALETTE_FILTER

		palette, err := enc.readTightPalette(r, bytesPixel)
		if err != nil {
			logger.Errorf("handleTightFilters: error in Reading Palette: %v", err)
			return
		}
		logger.Debugf("----PALETTE_FILTER,palette len=%d counter=%d, rect= %v", len(palette), counter, rect)

		//logger.Debugf("got palette: %v", palette)
		var dataLength int
		if len(palette) == 2 {
			dataLength = int(rect.Height) * ((int(rect.Width) + 7) / 8)
		} else {
			dataLength = int(rect.Width * rect.Height)
		}
		tightBytes, err := enc.ReadTightData(dataLength, r, int(decoderId))
		//logger.Debugf("got tightBytes: %v", tightBytes)
		if err != nil {
			logger.Errorf("handleTightFilters: error in handling tight encoding, reading palette filter data: %v", err)
			return
		}
		//logger.Errorf("handleTightFilters: got tight data: %v", tightBytes)

		myImg := enc.Image.(draw.Image)
		bytePos := 0
		bitPos := 0
		var palettePos int
		for i := 0; i < int(rect.Height); i++ {
			for j := 0; j < int(rect.Width); j++ {
				if len(palette) == 2 {
					currByte := tightBytes[bytePos]
					palettePos = int(currByte&byte(math.Pow(2.0, float64(bitPos)))) >> uint(bitPos)
					//logger.Debugf("palletPos=%d, bitpos=%d, bytepos=%d", palettePos, bitPos, bytePos)
					bytePos = bytePos + int((bitPos+1.0)/8.0)
					bitPos = (bitPos + 1) % 8
					//logger.Debugf("next: bitpos=%d, bytepos=%d", bitPos, bytePos)
				} else {
					palettePos = int(tightBytes[bytePos])
					bytePos++
				}
				myImg.Set(int(rect.X)+j, int(rect.Y)+i, palette[palettePos])
				//logger.Debugf("(%d,%d): pos: %d col:%d", int(rect.X)+j, int(rect.Y)+i, palettePos, palette[palettePos])
			}
		}
		//enc.Image = myImg
	case TightFilterGradient: //GRADIENT_FILTER
		logger.Debugf("----GRADIENT_FILTER: bytesPixel=%d, counter=%d", bytesPixel, counter)
		//logger.Debugf("usegrad: %d\n", filterid)
		data, err := enc.ReadTightData(lengthCurrentbpp, r, int(decoderId))
		if err != nil {
			logger.Errorf("handleTightFilters: error in handling tight encoding, Reading GRADIENT_FILTER: %v", err)
			return
		}
		enc.decodeGradData(rect, data)

	case TightFilterCopy: //BASIC_FILTER
		//lengthCurrentbpp1 := int(pixelFmt.BPP/8) * int(rect.Width) * int(rect.Height)
		logger.Debugf("----BASIC_FILTER: bytesPixel=%d, counter=%d", bytesPixel, counter)

		tightBytes, err := enc.ReadTightData(lengthCurrentbpp, r, int(decoderId))
		if err != nil {
			logger.Errorf("handleTightFilters: error in handling tight encoding, Reading BASIC_FILTER: %v", err)
			return
		}
		logger.Debugf("tightBytes len= %d", len(tightBytes))
		enc.drawTightBytes(tightBytes, rect)
	default:
		logger.Errorf("handleTightFilters: Bad tight filter id: %d", filterid)
		return
	}

	return
}
func (enc *TightEncoding) decodeGradData(rect *Rectangle, buffer []byte) {

	logger.Debugf("putting gradient size: %v on image: %v", rect, enc.Image.Bounds())

	prevRow := make([]byte, rect.Width*3+3) //new byte[w * 3];
	thisRow := make([]byte, rect.Width*3+3) //new byte[w * 3];

	bIdx := 0
	dst := (enc.Image).(*image.RGBA) // enc.Image.(*image.RGBA)

	for i := 0; i < int(rect.Height); i++ {
		for j := 3; j < int(rect.Width*3+3); j += 3 {
			d := int(0xff&prevRow[j]) + // "upper" pixel (from prev row)
				int(0xff&thisRow[j-3]) - // prev pixel
				int(0xff&prevRow[j-3]) // "diagonal" prev pixel
			if d < 0 {
				d = 0
			}
			if d > 255 {
				d = 255
			}
			red := int(buffer[bIdx]) + d
			thisRow[j] = byte(red & 255)

			d = int(0xff&prevRow[j+1]) +
				int(0xff&thisRow[j+1-3]) -
				int(0xff&prevRow[j+1-3])
			if d < 0 {
				d = 0
			}
			if d > 255 {
				d = 255
			}
			green := int(buffer[bIdx+1]) + d
			thisRow[j+1] = byte(green & 255)

			d = int(0xff&prevRow[j+2]) +
				int(0xff&thisRow[j+2-3]) -
				int(0xff&prevRow[j+2-3])
			if d < 0 {
				d = 0
			}
			if d > 255 {
				d = 255
			}
			blue := int(buffer[bIdx+2]) + d
			thisRow[j+2] = byte(blue & 255)

			bIdx += 3
		}

		for idx := 3; idx < (len(thisRow) - 3); idx += 3 {
			myColor := color.RGBA{R: (thisRow[idx]), G: (thisRow[idx+1]), B: (thisRow[idx+2]), A: 1}
			dst.SetRGBA(idx/3+int(rect.X)-1, int(rect.Y)+i, myColor)
			//logger.Debugf("putting pixel: idx=%d, pos=(%d,%d), col=%v", idx, idx/3+int(rect.X), int(rect.Y)+i, myColor)

		}

		// exchange thisRow and prevRow:
		tempRow := thisRow
		thisRow = prevRow
		prevRow = tempRow
	}
}

func (enc *TightEncoding) decodeGradientData(rect *Rectangle, buf []byte) {
	logger.Debugf("putting gradient on image: %v", enc.Image.Bounds())
	var dx, dy, c int
	prevRow := make([]byte, rect.Width*3) //new byte[w * 3];
	thisRow := make([]byte, rect.Width*3) //new byte[w * 3];
	pix := make([]byte, 3)
	est := make([]int, 3)

	dst := (enc.Image).(*image.RGBA) // enc.Image.(*image.RGBA)
	//offset := int(rect.Y)*dst.Bounds().Max.X + int(rect.X)

	for dy = 0; dy < int(rect.Height); dy++ {
		//offset := dst.PixOffset(x, y)
		/* First pixel in a row */
		for c = 0; c < 3; c++ {
			pix[c] = byte(prevRow[c] + buf[dy*int(rect.Width)*3+c])
			thisRow[c] = pix[c]
		}
		//logger.Debugf("putting pixel:%d,%d,%d at offset: %d, pixArrayLen= %v, rect=x:%d,y:%d,w:%d,h:%d, Yposition=%d", pix[0], pix[1], pix[2], offset, len(dst.Pix), rect.X, rect.Y, rect.Width, rect.Height, dy)
		myColor := color.RGBA{R: (pix[0]), G: (pix[1]), B: (pix[2]), A: 1}
		dst.SetRGBA(int(rect.X), dy+int(rect.Y), myColor)

		/* Remaining pixels of a row */
		for dx = 1; dx < int(rect.Width); dx++ {
			for c = 0; c < 3; c++ {
				est[c] = int((prevRow[dx*3+c] & 0xFF) + (pix[c] & 0xFF) - (prevRow[(dx-1)*3+c] & 0xFF))
				if est[c] > 0xFF {
					est[c] = 0xFF
				} else if est[c] < 0x00 {
					est[c] = 0x00
				}
				pix[c] = (byte)(byte(est[c]) + buf[(dy*int(rect.Width)+dx)*3+c])
				thisRow[dx*3+c] = pix[c]
			}
			//logger.Debugf("putting pixel:%d,%d,%d at offset: %d, pixArrayLen= %v, rect=x:%d,y:%d,w:%d,h:%d, Yposition=%d", pix[0], pix[1], pix[2], offset, len(dst.Pix), x, y, w, h, dy)
			myColor := color.RGBA{R: pix[0], G: (pix[1]), B: (pix[2]), A: 1}
			dst.SetRGBA(dx+int(rect.X), dy+int(rect.Y), myColor)

		}

		copy(prevRow, thisRow)
	}
	enc.Image = dst
}

func ReadBytes(count int, r io.Reader) ([]byte, error) {
	buff := make([]byte, count)

	lengthRead, err := io.ReadFull(r, buff)

	//lengthRead, err := r.Read(buff)
	if lengthRead != count {
		logger.Errorf("RfbReadHelper.ReadBytes unable to read bytes: lengthRead=%d, countExpected=%d", lengthRead, count)
		return nil, errors.New("RfbReadHelper.ReadBytes unable to read bytes")
	}

	//err := binary.Read(r, binary.BigEndian, &buff)

	if err != nil {
		logger.Errorf("RfbReadHelper.ReadBytes error while reading bytes: ", err)
		//if err := binary.Read(d.conn, binary.BigEndian, &buff); err != nil {
		return nil, err
	}

	return buff, nil
}

func (enc *TightEncoding) readTightPalette(connReader Conn, bytesPixel int) (color.Palette, error) {

	colorCount, err := ReadUint8(connReader)
	if err != nil {
		logger.Errorf("handleTightFilters: error in handling tight encoding, reading TightFilterPalette: %v", err)
		return nil, err
	}

	paletteSize := colorCount + 1 // add one more
	//logger.Debugf("----PALETTE_FILTER: paletteSize=%d bytesPixel=%d\n", paletteSize, bytesPixel)
	//complete palette
	paletteColorBytes, err := ReadBytes(int(paletteSize)*bytesPixel, connReader)
	if err != nil {
		logger.Errorf("handleTightFilters: error in handling tight encoding, reading TightFilterPalette.paletteSize: %v", err)
		return nil, err
	}
	var paletteColors color.Palette = make([]color.Color, 0)
	for i := 0; i < int(paletteSize)*bytesPixel; i += 3 {
		col := color.RGBA{R: paletteColorBytes[i], G: paletteColorBytes[i+1], B: paletteColorBytes[i+2], A: 1}
		paletteColors = append(paletteColors, col)
	}
	return paletteColors, nil
}

func ReadUint8(r Conn) (uint8, error) {
	var myUint uint8
	if err := binary.Read(r, binary.BigEndian, &myUint); err != nil {
		return 0, err
	}

	return myUint, nil
}

func (enc *TightEncoding) ReadTightData(dataSize int, c Conn, decoderId int) ([]byte, error) {
	if int(dataSize) < TightMinToCompress {
		return ReadBytes(int(dataSize), c)
	}
	zlibDataLen, err := readTightLength(c)
	//logger.Debugf("RfbReadHelper.ReadTightData: compactlen=%d", zlibDataLen)
	if err != nil {
		return nil, err
	}
	zippedBytes, err := ReadBytes(zlibDataLen, c)
	if err != nil {
		return nil, err
	}
	var r io.Reader
	if enc.decoders[decoderId] == nil {
		b := bytes.NewReader(zippedBytes)
		r, err = zlib.NewReader(b)
		enc.decoders[decoderId] = r
		enc.decoderBuffs[decoderId] = b
	} else {
		b := enc.decoderBuffs[decoderId]
		b.Reset(zippedBytes) //set the underlaying buffer to new content (not resetting the decoder zlib stream)
		r = enc.decoders[decoderId]
	}

	retBytes := make([]byte, dataSize)
	count, err := io.ReadFull(r, retBytes)
	if err != nil {
		return nil, err
	}
	if count != dataSize {
		return nil, errors.New("ReadTightData: reading inflating zip didn't produce expected number of bytes")
	}
	return retBytes, nil
}

type TightCC struct {
	Compression TightCompression
	Filter      TightFilter
}

func readTightCC(c Conn) (*TightCC, error) {
	var ccb uint8 // compression control byte
	if err := binary.Read(c, binary.BigEndian, &ccb); err != nil {
		return nil, err
	}
	cmp := TightCompression(ccb >> 4)
	switch cmp {
	case TightCompressionBasic:
		return &TightCC{TightCompressionBasic, TightFilterCopy}, nil
	case TightCompressionFill:
		return &TightCC{TightCompressionFill, TightFilterCopy}, nil
	case TightCompressionPNG:
		return &TightCC{TightCompressionPNG, TightFilterCopy}, nil
	}
	return nil, fmt.Errorf("unknown tight compression %d", cmp)
}

func writeTightCC(c Conn, tcc *TightCC) error {
	var ccb uint8 // compression control byte
	switch tcc.Compression {
	case TightCompressionFill:
		ccb = setBit(ccb, 7)
	case TightCompressionJPEG:
		ccb = setBit(ccb, 7)
		ccb = setBit(ccb, 4)
	case TightCompressionPNG:
		ccb = setBit(ccb, 7)
		ccb = setBit(ccb, 5)
	}
	return binary.Write(c, binary.BigEndian, ccb)
}

type TightPixel struct {
	R uint8
	G uint8
	B uint8
}

func writeTightLength(c Conn, l int) error {
	var buf []uint8

	buf = append(buf, uint8(l&0x7F))
	if l > 0x7F {
		buf[0] |= 0x80
		buf = append(buf, uint8((l>>7)&0x7F))
		if l > 0x3FFF {
			buf[1] |= 0x80
			buf = append(buf, uint8((l>>14)&0xFF))
		}
	}
	return binary.Write(c, binary.BigEndian, buf)
}

func readTightLength(c Conn) (int, error) {
	var length int
	var err error
	var b uint8

	if err = binary.Read(c, binary.BigEndian, &b); err != nil {
		return 0, err
	}

	length = int(b) & 0x7F
	if (b & 0x80) == 0 {
		return length, nil
	}

	if err = binary.Read(c, binary.BigEndian, &b); err != nil {
		return 0, err
	}
	length |= (int(b) & 0x7F) << 7
	if (b & 0x80) == 0 {
		return length, nil
	}

	if err = binary.Read(c, binary.BigEndian, &b); err != nil {
		return 0, err
	}
	length |= (int(b) & 0xFF) << 14

	return length, nil
}

/**
 * Draw byte array bitmap data (for Tight)
 */
func (enc *TightEncoding) drawTightBytes(bytes []byte, rect *Rectangle) {
	bytesPos := 0
	myImg := (enc.Image).(draw.Image)
	logger.Debugf("drawTightBytes: len(bytes)= %d, %v", len(bytes), rect)

	for ly := rect.Y; ly < rect.Y+rect.Height; ly++ {
		for lx := rect.X; lx < rect.X+rect.Width; lx++ {
			color := color.RGBA{R: bytes[bytesPos], G: bytes[bytesPos+1], B: bytes[bytesPos+2], A: 1}
			//logger.Debugf("drawTightBytes: setting pixel= (%d,%d): %v", int(lx), int(ly), color)
			myImg.Set(int(lx), int(ly), color)

			bytesPos += 3
		}
	}
	//enc.Image = myImg
}

//     /**
//      * Draw paletted byte array bitmap data
//      *
//      * @param buffer  bitmap data
//      * @param rect    bitmap location and dimensions
//      * @param palette colour palette
//      * @param paletteSize number of colors in palette
//      */
// 	 func (enc *TightPngEncoding) drawBytesWithPalette( buffer []byte, rect *Rectangle,  palette []int, int paletteSize) {
// 		//create palette:
// 		var imgPalette []color.Color = make([]int, len(palette))
// 		for i:=0;len(palette);i++{
// 			col := color.RGBA{
// 				R:,G:,B:,A:0
// 			}
// 			imgPalette[i]=col
// 		 }

// 		//lock.lock();
// 		img:=image.Paletted{

// 		}
// 		// 2 colors
// 		thisWidth := enc.Image.Bounds().Max.Y
//         if paletteSize == 2 {
//             var  dx, dy, n int;
//              i := rect.y * thisWidth + rect.x
//              rowBytes := (rect.width + 7) / 8
//              var b byte;

//             for dy = 0; dy < rect.height; dy++ {
//                 for dx = 0; dx < rect.width / 8; dx++ {
//                     b = buffer[dy * rowBytes + dx];
//                     for n = 7; n >= 0; n-- {
// 						color := palette[b >> n & 1]
// 						enc.Image.(draw.Image).Set(0, 0, color.RGBA{R: tpx.R, G: tpx.G, B: tpx.B, A: 1})
//                         //pixels[i++] = palette[b >> n & 1];
//                     }
//                 }
//                 for n = 7; n >= 8 - rect.width % 8; n-- {
//                     pixels[i++] = palette[buffer[dy * rowBytes + dx] >> n & 1];
//                 }
//                 i += this.width - rect.width;
//             }
//         } else {
//             // 3..255 colors (assuming bytesPixel == 4).
//             int i = 0;
//             for (int ly = rect.y; ly < rect.y + rect.height; ++ly) {
//                 for (int lx = rect.x; lx < rect.x + rect.width; ++lx) {
//                     int pixelsOffset = ly * this.width + lx;
//                     pixels[pixelsOffset] = palette[buffer[i++] & 0xFF];
//                 }
//             }
//         }
//         //lock.unlock();
// 	}
