package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"rsc.io/qr"
)

const qrTestLink = "https://agent.example/#pair=ABCD-1234"

var ansiSequence = regexp.MustCompile("\x1b\\[[0-9;]*m")

func visibleWidth(line string) int {
	return utf8.RuneCountInString(ansiSequence.ReplaceAllString(line, ""))
}

func decodePlainModules(t *testing.T, out string, quiet int) [][]bool {
	t.Helper()
	var light [][]bool
	for _, line := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		top, bottom := []bool{}, []bool{}
		for _, r := range line {
			switch r {
			case '█':
				top, bottom = append(top, true), append(bottom, true)
			case '▀':
				top, bottom = append(top, true), append(bottom, false)
			case '▄':
				top, bottom = append(top, false), append(bottom, true)
			case ' ':
				top, bottom = append(top, false), append(bottom, false)
			default:
				t.Fatalf("unexpected glyph %q", r)
			}
		}
		light = append(light, top, bottom)
	}
	return modulesFromLight(light, quiet)
}

func decodeColourModules(t *testing.T, out string, quiet int) [][]bool {
	t.Helper()
	var light [][]bool
	for _, line := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		top, bottom := []bool{}, []bool{}
		fg, bg := "", ""
		rest := line
		for rest != "" {
			if loc := ansiSequence.FindStringIndex(rest); loc != nil && loc[0] == 0 {
				params := strings.Split(strings.TrimSuffix(strings.TrimPrefix(rest[:loc[1]], "\x1b["), "m"), ";")
				for i := 0; i < len(params); i++ {
					switch {
					case params[i] == "0":
						fg, bg = "", ""
					case params[i] == "38" && i+2 < len(params):
						fg = params[i+2]
						i += 2
					case params[i] == "48" && i+2 < len(params):
						bg = params[i+2]
						i += 2
					}
				}
				rest = rest[loc[1]:]
				continue
			}
			r, size := utf8.DecodeRuneInString(rest)
			rest = rest[size:]
			if r != '▀' {
				t.Fatalf("unexpected glyph %q", r)
			}
			top, bottom = append(top, fg == "231"), append(bottom, bg == "231")
		}
		light = append(light, top, bottom)
	}
	return modulesFromLight(light, quiet)
}

func modulesFromLight(light [][]bool, quiet int) [][]bool {
	size := len(light[0]) - 2*quiet
	modules := make([][]bool, size)
	for y := range size {
		modules[y] = make([]bool, size)
		for x := range size {
			modules[y][x] = !light[y+quiet][x+quiet]
		}
	}
	return modules
}

func encodedModules(t *testing.T, text string) [][]bool {
	t.Helper()
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		t.Fatal(err)
	}
	modules := make([][]bool, code.Size)
	for y := range code.Size {
		modules[y] = make([]bool, code.Size)
		for x := range code.Size {
			modules[y][x] = code.Black(x, y)
		}
	}
	return modules
}

func checkQRGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *updateGolden {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("%s changed; run with -update and review the diff.\ngot:\n%s\nwant:\n%s", name, got, want)
	}
}

func TestRemoteQRPlainGolden(t *testing.T) {
	got, err := renderQR(qrTestLink, false)
	if err != nil {
		t.Fatal(err)
	}
	checkQRGolden(t, "qr.plain.golden", got)
}

func TestRemoteQRColourGolden(t *testing.T) {
	got, err := renderQR(qrTestLink, true)
	if err != nil {
		t.Fatal(err)
	}
	checkQRGolden(t, "qr.colour.golden", got)
}

func TestRemoteQRColourPaintsDarkModulesBlackOnWhiteWhateverTheTheme(t *testing.T) {
	got, err := renderQR(qrTestLink, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "38;5;16") || !strings.Contains(got, "48;5;231") {
		t.Fatalf("no explicit black and white in %q", got)
	}
	for _, line := range strings.Split(strings.TrimSuffix(got, "\n"), "\n") {
		if !strings.HasSuffix(line, "\x1b[0m") {
			t.Fatalf("line does not reset colours: %q", line)
		}
	}
	first := strings.SplitN(got, "\n", 2)[0]
	if strings.Contains(first, "16") {
		t.Fatalf("quiet zone row is not white: %q", first)
	}
}

func TestRemoteQRLinesAllHaveTheSameVisibleWidth(t *testing.T) {
	width, err := qrWidth(qrTestLink)
	if err != nil {
		t.Fatal(err)
	}
	for _, colour := range []bool{false, true} {
		got, err := renderQR(qrTestLink, colour)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(strings.TrimSuffix(got, "\n"), "\n") {
			if visibleWidth(line) != width {
				t.Fatalf("colour %v line %d is %d wide, want %d", colour, i, visibleWidth(line), width)
			}
		}
	}
}

func TestRemoteQRRenderedModulesDecodeBackToTheEncodedMatrix(t *testing.T) {
	want := encodedModules(t, qrTestLink)
	plain, err := renderQR(qrTestLink, false)
	if err != nil {
		t.Fatal(err)
	}
	coloured, err := renderQR(qrTestLink, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := decodePlainModules(t, plain, qrQuietZone); !reflect.DeepEqual(got, want) {
		t.Error("plain output does not decode to the encoded matrix")
	}
	if got := decodeColourModules(t, coloured, qrQuietZone); !reflect.DeepEqual(got, want) {
		t.Error("coloured output does not decode to the encoded matrix")
	}
}

func TestRemoteQRTargetUsesPlainBlocksWhenNotATerminal(t *testing.T) {
	noEnv := func(string) string { return "" }
	if got := qrTargetFor(&bytes.Buffer{}, noEnv); got.Colour || got.Columns != 0 {
		t.Fatalf("buffer target %+v", got)
	}
	f, err := os.Create(filepath.Join(t.TempDir(), "out"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if got := qrTargetFor(f, noEnv); got.Colour {
		t.Fatalf("file target %+v", got)
	}
}

func TestRemoteQRColumnsComeFromTheColumnsVariable(t *testing.T) {
	if got := columnsFrom(func(string) string { return "57" }); got != 57 {
		t.Fatalf("columns %d", got)
	}
	if got := columnsFrom(func(string) string { return "wide" }); got != 0 {
		t.Fatalf("columns %d", got)
	}
}

func TestRemoteQRTooNarrowTerminalWarnsInsteadOfDrawingAWrappedCode(t *testing.T) {
	var out, errOut bytes.Buffer
	width, err := qrWidth(qrTestLink)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeQR(&out, &errOut, qrTestLink, qrTarget{Columns: width - 1}); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Fatalf("drew a wrapped code:\n%s", out.String())
	}
	msg := errOut.String()
	if !strings.Contains(msg, fmt.Sprint(width-1)) || !strings.Contains(msg, fmt.Sprint(width)) || !strings.Contains(msg, "link") {
		t.Fatalf("warning %q", msg)
	}
}

func TestRemoteQRFittingTerminalDrawsTheCodeWithoutWarning(t *testing.T) {
	var out, errOut bytes.Buffer
	width, err := qrWidth(qrTestLink)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeQR(&out, &errOut, qrTestLink, qrTarget{Columns: width}); err != nil {
		t.Fatal(err)
	}
	if out.Len() == 0 || errOut.Len() != 0 {
		t.Fatalf("out %d bytes, warning %q", out.Len(), errOut.String())
	}
}
