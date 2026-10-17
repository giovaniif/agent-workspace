package main

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"strconv"
	"strings"
)

var glyphs = map[rune][7]string{
	'A': {".###.", "#...#", "#...#", "#####", "#...#", "#...#", "#...#"},
	'B': {"####.", "#...#", "#...#", "####.", "#...#", "#...#", "####."},
	'C': {".####", "#....", "#....", "#....", "#....", "#....", ".####"},
	'D': {"####.", "#...#", "#...#", "#...#", "#...#", "#...#", "####."},
	'E': {"#####", "#....", "#....", "####.", "#....", "#....", "#####"},
	'F': {"#####", "#....", "#....", "####.", "#....", "#....", "#...."},
	'G': {".####", "#....", "#....", "#..##", "#...#", "#...#", ".###."},
	'H': {"#...#", "#...#", "#...#", "#####", "#...#", "#...#", "#...#"},
	'I': {"#####", "..#..", "..#..", "..#..", "..#..", "..#..", "#####"},
	'J': {"..###", "...#.", "...#.", "...#.", "#..#.", "#..#.", ".##.."},
	'K': {"#...#", "#..#.", "#.#..", "##...", "#.#..", "#..#.", "#...#"},
	'L': {"#....", "#....", "#....", "#....", "#....", "#....", "#####"},
	'M': {"#...#", "##.##", "#.#.#", "#.#.#", "#...#", "#...#", "#...#"},
	'N': {"#...#", "##..#", "#.#.#", "#..##", "#...#", "#...#", "#...#"},
	'O': {".###.", "#...#", "#...#", "#...#", "#...#", "#...#", ".###."},
	'P': {"####.", "#...#", "#...#", "####.", "#....", "#....", "#...."},
	'Q': {".###.", "#...#", "#...#", "#...#", "#.#.#", "#..#.", ".##.#"},
	'R': {"####.", "#...#", "#...#", "####.", "#.#..", "#..#.", "#...#"},
	'S': {".####", "#....", "#....", ".###.", "....#", "....#", "####."},
	'T': {"#####", "..#..", "..#..", "..#..", "..#..", "..#..", "..#.."},
	'U': {"#...#", "#...#", "#...#", "#...#", "#...#", "#...#", ".###."},
	'V': {"#...#", "#...#", "#...#", "#...#", "#...#", ".#.#.", "..#.."},
	'W': {"#...#", "#...#", "#...#", "#.#.#", "#.#.#", "##.##", "#...#"},
	'X': {"#...#", "#...#", ".#.#.", "..#..", ".#.#.", "#...#", "#...#"},
	'Y': {"#...#", "#...#", ".#.#.", "..#..", "..#..", "..#..", "..#.."},
	'Z': {"#####", "....#", "...#.", "..#..", ".#...", "#....", "#####"},
	'0': {".###.", "#...#", "#..##", "#.#.#", "##..#", "#...#", ".###."},
	'1': {"..#..", ".##..", "..#..", "..#..", "..#..", "..#..", ".###."},
	'2': {".###.", "#...#", "....#", "...#.", "..#..", ".#...", "#####"},
	'3': {"####.", "....#", "....#", ".###.", "....#", "....#", "####."},
	'4': {"...#.", "..##.", ".#.#.", "#..#.", "#####", "...#.", "...#."},
	'5': {"#####", "#....", "####.", "....#", "....#", "#...#", ".###."},
	'6': {".###.", "#....", "#....", "####.", "#...#", "#...#", ".###."},
	'7': {"#####", "....#", "...#.", "..#..", ".#...", ".#...", ".#..."},
	'8': {".###.", "#...#", "#...#", ".###.", "#...#", "#...#", ".###."},
	'9': {".###.", "#...#", "#...#", ".####", "....#", "....#", ".###."},
	':': {".....", "..#..", "..#..", ".....", "..#..", "..#..", "....."},
	'.': {".....", ".....", ".....", ".....", ".....", "..#..", "..#.."},
	',': {".....", ".....", ".....", ".....", "..#..", "..#..", ".#..."},
	'-': {".....", ".....", ".....", "#####", ".....", ".....", "....."},
	'/': {"....#", "....#", "...#.", "..#..", ".#...", "#....", "#...."},
	'(': {"...#.", "..#..", ".#...", ".#...", ".#...", "..#..", "...#."},
	')': {".#...", "..#..", "...#.", "...#.", "...#.", "..#..", ".#..."},
	'#': {".#.#.", ".#.#.", "#####", ".#.#.", "#####", ".#.#.", ".#.#."},
	'?': {".###.", "#...#", "....#", "...#.", "..#..", ".....", "..#.."},
	'!': {"..#..", "..#..", "..#..", "..#..", "..#..", ".....", "..#.."},
}

const glyphScale = 3

func drawText(img *image.RGBA, x, y int, text string, c color.Color) {
	for _, ch := range strings.ToUpper(text) {
		g, ok := glyphs[ch]
		if ok {
			for gy, row := range g {
				for gx, px := range row {
					if px == '#' {
						draw.Draw(img, image.Rect(x+gx*glyphScale, y+gy*glyphScale, x+(gx+1)*glyphScale, y+(gy+1)*glyphScale), image.NewUniform(c), image.Point{}, draw.Src)
					}
				}
			}
		}
		x += 6 * glyphScale
	}
}

func parseRect(s string) (image.Rectangle, error) {
	parts := strings.Split(s, ",")
	if len(parts) != 4 {
		return image.Rectangle{}, fmt.Errorf("want x0,y0,x1,y1, got %q", s)
	}
	var n [4]int
	for i, p := range parts {
		v, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			return image.Rectangle{}, fmt.Errorf("want numbers in %q", s)
		}
		n[i] = v
	}
	return image.Rect(n[0], n[1], n[2], n[3]), nil
}

func readPNG(p string) (image.Image, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return png.Decode(f)
}

func annotate(e *env, args []string, r *result) {
	o, ok := parse(args, []string{"before", "after", "crop", "box", "caption-before", "caption-after", "out"}, nil, r)
	if !ok {
		return
	}
	before, after, out := o.get("before", ""), o.get("after", ""), o.get("out", "")
	if before == "" || after == "" || out == "" {
		r.bad("annotate needs --before, --after and --out", "go run ./scripts/verify annotate --before b.png --after a.png --crop 0,0,780,400 --box 10,10,200,60 --out pair.png")
		return
	}
	imgs := make([]image.Image, 2)
	for i, p := range []string{before, after} {
		im, err := readPNG(p)
		if err != nil {
			r.fail("cannot read "+p+": "+err.Error(), "give a PNG")
			return
		}
		imgs[i] = im
	}
	crop := imgs[0].Bounds().Intersect(imgs[1].Bounds())
	if c := o.get("crop", ""); c != "" {
		rc, err := parseRect(c)
		if err != nil {
			r.bad(err.Error(), "--crop x0,y0,x1,y1")
			return
		}
		if !rc.In(imgs[0].Bounds()) || !rc.In(imgs[1].Bounds()) {
			r.fail(fmt.Sprintf("the crop %v is not inside both images (%v, %v)", rc, imgs[0].Bounds(), imgs[1].Bounds()), "use a crop inside the smaller image")
			return
		}
		crop = rc
	}
	var box image.Rectangle
	if b := o.get("box", ""); b != "" {
		rc, err := parseRect(b)
		if err != nil {
			r.bad(err.Error(), "--box x0,y0,x1,y1 in image coordinates")
			return
		}
		box = rc
	}
	const gap, bar = 16, 40
	w, h := crop.Dx(), crop.Dy()
	canvas := image.NewRGBA(image.Rect(0, 0, w*2+gap*3, h+bar+gap*2))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.RGBA{0xef, 0xf1, 0xf5, 0xff}), image.Point{}, draw.Src)
	red := color.RGBA{0xd2, 0x0f, 0x39, 0xff}
	text := color.RGBA{0x4c, 0x4f, 0x69, 0xff}
	captions := []string{o.get("caption-before", "Before"), o.get("caption-after", "After")}
	for i, im := range imgs {
		x0 := gap + i*(w+gap)
		y0 := bar + gap
		draw.Draw(canvas, image.Rect(x0, y0, x0+w, y0+h), im, crop.Min, draw.Src)
		drawText(canvas, x0, gap/2+4, captions[i], text)
		if !box.Empty() {
			b := box.Sub(crop.Min).Add(image.Pt(x0, y0)).Intersect(image.Rect(x0, y0, x0+w, y0+h))
			for t := 0; t < 3; t++ {
				draw.Draw(canvas, image.Rect(b.Min.X, b.Min.Y+t, b.Max.X, b.Min.Y+t+1), image.NewUniform(red), image.Point{}, draw.Src)
				draw.Draw(canvas, image.Rect(b.Min.X, b.Max.Y-t-1, b.Max.X, b.Max.Y-t), image.NewUniform(red), image.Point{}, draw.Src)
				draw.Draw(canvas, image.Rect(b.Min.X+t, b.Min.Y, b.Min.X+t+1, b.Max.Y), image.NewUniform(red), image.Point{}, draw.Src)
				draw.Draw(canvas, image.Rect(b.Max.X-t-1, b.Min.Y, b.Max.X-t, b.Max.Y), image.NewUniform(red), image.Point{}, draw.Src)
			}
		}
	}
	f, err := os.Create(out)
	if err != nil {
		r.fail(err.Error(), "check --out")
		return
	}
	if err := png.Encode(f, canvas); err != nil {
		_ = f.Close()
		r.fail(err.Error(), "check --out")
		return
	}
	_ = f.Close()
	sum, _ := fileHash(out)
	r.set("out", out)
	r.set("sha256", sum)
	r.set("size", []int{canvas.Bounds().Dx(), canvas.Bounds().Dy()})
}
