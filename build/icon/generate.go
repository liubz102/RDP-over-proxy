//go:build ignore

// Command generate renders the app icon from the SVG drawings in build/icon
// and writes build/appicon.png (1024 px) and build/windows/icon.ico. The build
// puts icon.ico into the exe (window, taskbar, Explorer) and main.go gives it
// to the tray.
//
// Sizes up to 64 px come from their own pixel-aligned drawings
// (appicon-<size>.svg) and the rest from appicon.svg. Microsoft Edge in
// headless mode draws them, so the images look exactly like the SVGs do in a
// browser.
//
// Run it from the repository root after changing a drawing:
//
//	wails3 task common:generate:icons
//
// which runs go run build/icon/generate.go. Use -edge to point at another
// msedge.exe or chrome.exe.
package main

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"html"
	"image"
	"image/color"
	"image/png"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const (
	sourceDir = "build/icon"
	pngPath   = "build/appicon.png"
	icoPath   = "build/windows/icon.ico"
	pngSize   = 1024
)

// icoSizes are the images in icon.ico; Windows picks the nearest one for
// each place. 16 to 32 cover the title bar, the taskbar and the tray at 100%
// to 200% display scaling, 40 to 96 Alt+Tab and Explorer's views, and 256
// Explorer's largest view.
var icoSizes = []int{16, 20, 24, 32, 40, 48, 64, 96, 256}

// drawing is the SVG file used for an image of the given size.
func drawing(size int) string {
	if size <= 64 {
		return fmt.Sprintf("appicon-%d.svg", size)
	}
	return "appicon.svg"
}

func main() {
	log.SetFlags(0)
	edge := flag.String("edge", findEdge(), "path to msedge.exe (or chrome.exe)")
	flag.Parse()
	if *edge == "" {
		log.Fatal("Microsoft Edge not found; pass its path with -edge")
	}
	if _, err := os.Stat(filepath.Join(sourceDir, "appicon.svg")); err != nil {
		log.Fatalf("run this from the repository root: %v", err)
	}

	sizes := append(append([]int{}, icoSizes...), pngSize)
	images, err := render(*edge, sizes)
	if err != nil {
		log.Fatal(err)
	}

	big, err := encodePNG(images[pngSize])
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(pngPath, big, 0o644); err != nil {
		log.Fatal(err)
	}
	ico, err := encodeICO(images, icoSizes)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(icoPath, ico, 0o644); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote %s (%d px) and %s (%s px)", pngPath, pngSize, icoPath, joinInts(icoSizes))
}

func findEdge() string {
	for _, env := range []string{"ProgramFiles(x86)", "ProgramFiles"} {
		if dir := os.Getenv(env); dir != "" {
			path := filepath.Join(dir, `Microsoft\Edge\Application\msedge.exe`)
			if _, err := os.Stat(path); err == nil {
				return path
			}
		}
	}
	return ""
}

// render has the browser draw each size onto a canvas and print the PNGs into
// the page, which --dump-dom writes to stdout once the page has loaded.
func render(browser string, sizes []int) (map[int]image.Image, error) {
	tmp, err := os.MkdirTemp("", "appicon-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)

	var page strings.Builder
	page.WriteString("<!doctype html><meta charset=\"utf-8\">\n")
	for _, size := range sizes {
		svg, err := os.ReadFile(filepath.Join(sourceDir, drawing(size)))
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(&page, "<img data-size=\"%d\" src=\"data:image/svg+xml;base64,%s\">\n",
			size, base64.StdEncoding.EncodeToString(withSize(svg, size)))
	}
	// The load event waits for every image; the handler runs before the DOM
	// is dumped.
	page.WriteString(`<pre id="out"></pre>
<script>
addEventListener("load", () => {
  const lines = [];
  for (const img of document.images) {
    const size = Number(img.dataset.size);
    const canvas = document.createElement("canvas");
    canvas.width = canvas.height = size;
    canvas.getContext("2d").drawImage(img, 0, 0, size, size);
    lines.push(size + " " + canvas.toDataURL("image/png").split(",")[1]);
  }
  document.getElementById("out").textContent = lines.join("\n");
});
</script>
`)
	pagePath := filepath.Join(tmp, "page.html")
	if err := os.WriteFile(pagePath, []byte(page.String()), 0o644); err != nil {
		return nil, err
	}

	pageURL := url.URL{Scheme: "file", Path: "/" + filepath.ToSlash(pagePath)}
	cmd := exec.Command(browser, "--headless=new", "--disable-gpu", "--no-first-run",
		"--no-default-browser-check", "--user-data-dir="+filepath.Join(tmp, "profile"),
		"--dump-dom", pageURL.String())
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	// Output returns once every process holding the stdout pipe has exited,
	// including the ones msedge.exe hands the work to.
	dom, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s: %v\n%s", filepath.Base(browser), err, stderr.Bytes())
	}

	out := regexp.MustCompile(`(?s)<pre id="out">(.*?)</pre>`).FindSubmatch(dom)
	if out == nil || len(bytes.TrimSpace(out[1])) == 0 {
		return nil, fmt.Errorf("%s printed no images\n%s", filepath.Base(browser), stderr.Bytes())
	}
	images := map[int]image.Image{}
	for _, line := range strings.Split(html.UnescapeString(string(out[1])), "\n") {
		sizeText, data, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok {
			continue
		}
		size, err := strconv.Atoi(sizeText)
		if err != nil {
			return nil, fmt.Errorf("unexpected output line %q", line)
		}
		raw, err := base64.StdEncoding.DecodeString(data)
		if err != nil {
			return nil, fmt.Errorf("image %d: %v", size, err)
		}
		img, err := png.Decode(bytes.NewReader(raw))
		if err != nil {
			return nil, fmt.Errorf("image %d: %v", size, err)
		}
		if b := img.Bounds(); b.Dx() != size || b.Dy() != size {
			return nil, fmt.Errorf("image %d came out %dx%d", size, b.Dx(), b.Dy())
		}
		images[size] = img
	}
	for _, size := range sizes {
		if images[size] == nil {
			return nil, fmt.Errorf("%s did not draw the %d px image", filepath.Base(browser), size)
		}
	}
	return images, nil
}

var (
	rootTag   = regexp.MustCompile(`<svg\b[^>]*>`)
	sizeAttrs = regexp.MustCompile(`\s(?:width|height)="[^"]*"`)
)

// withSize sets the root element's width and height, so the browser draws
// the SVG at that size instead of scaling a drawing made at another one.
func withSize(svg []byte, size int) []byte {
	return rootTag.ReplaceAllFunc(svg, func(tag []byte) []byte {
		tag = sizeAttrs.ReplaceAll(tag, nil)
		return []byte(fmt.Sprintf(`<svg width="%d" height="%d"`, size, size) + string(tag[len("<svg"):]))
	})
}

func encodePNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	if err := enc.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// encodeICO writes the 256 px image as PNG and the others as 32-bit bitmaps,
// the layout Windows' own icons use and every icon reader understands.
func encodeICO(images map[int]image.Image, sizes []int) ([]byte, error) {
	data := make([][]byte, len(sizes))
	for i, size := range sizes {
		var err error
		if size >= 256 {
			data[i], err = encodePNG(images[size])
		} else {
			data[i], err = dib(images[size])
		}
		if err != nil {
			return nil, err
		}
	}

	var buf bytes.Buffer
	le := binary.LittleEndian
	header := struct{ Reserved, Type, Count uint16 }{0, 1, uint16(len(sizes))}
	if err := binary.Write(&buf, le, header); err != nil {
		return nil, err
	}
	offset := 6 + 16*len(sizes)
	for i, size := range sizes {
		dim := uint8(size)
		if size >= 256 {
			dim = 0 // 0 means 256
		}
		entry := struct {
			Width, Height, Colors, Reserved uint8
			Planes, BitCount                uint16
			Bytes, Offset                   uint32
		}{dim, dim, 0, 0, 1, 32, uint32(len(data[i])), uint32(offset)}
		if err := binary.Write(&buf, le, entry); err != nil {
			return nil, err
		}
		offset += len(data[i])
	}
	for _, d := range data {
		buf.Write(d)
	}
	return buf.Bytes(), nil
}

// dib is an icon image in bitmap form: a BITMAPINFOHEADER, the pixels as
// BGRA rows from the bottom up, then the 1-bit AND mask (set where the pixel
// is fully transparent), which only very old readers look at.
func dib(img image.Image) ([]byte, error) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w > 255 || h > 255 {
		return nil, errors.New("bitmap icon images must be smaller than 256 px")
	}
	maskStride := (w + 31) / 32 * 4
	var buf bytes.Buffer
	header := struct {
		Size                 uint32
		Width, Height        int32
		Planes, BitCount     uint16
		Compression, Image   uint32
		XPerMeter, YPerMeter int32
		Used, Important      uint32
	}{40, int32(w), int32(2 * h), 1, 32, 0, uint32(w*h*4 + maskStride*h), 0, 0, 0, 0}
	if err := binary.Write(&buf, binary.LittleEndian, header); err != nil {
		return nil, err
	}
	pixel := func(x, y int) color.NRGBA {
		return color.NRGBAModel.Convert(img.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA)
	}
	for y := h - 1; y >= 0; y-- {
		for x := range w {
			c := pixel(x, y)
			buf.Write([]byte{c.B, c.G, c.R, c.A})
		}
	}
	row := make([]byte, maskStride)
	for y := h - 1; y >= 0; y-- {
		clear(row)
		for x := range w {
			if pixel(x, y).A == 0 {
				row[x/8] |= 0x80 >> (x % 8)
			}
		}
		buf.Write(row)
	}
	return buf.Bytes(), nil
}

func joinInts(v []int) string {
	s := make([]string, len(v))
	for i, n := range v {
		s[i] = strconv.Itoa(n)
	}
	return strings.Join(s, ", ")
}
