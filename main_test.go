package main

import (
	"bytes"
	"io"
	"math"
	"strings"
	"testing"
	"time"
)

/*
 * test
 */

func Test_editor(t *testing.T) {
	type keystroke struct {
		input    string
		expected string
	}

	test := func(t *testing.T, row, col int, content string, keystrokes []keystroke) {
		term := newvirtterm(row, col)
		in := newvirtstdin()
		go editor(term, strings.NewReader(content), in)

		for i, tc := range keystrokes {
			for _, r := range []rune(tc.input) {
				in.input(string(r))
			}
			time.Sleep(1 * time.Millisecond) // wait for terminal in another goroutine finishes its work

			got := term.String()
			if got != tc.expected {
				t.Fatalf("(test %v)\n========== expected ==========\n%v\n==========\n========== got ==========\n%v\n==========", i, tc.expected, got)
			}
		}
	}

	t.Run("cursor movement", func(t *testing.T) {
		t.Run("hjkl basic", func(t *testing.T) {
			content := heredoc(`
				1a1b1c1d1e1f
				2a2b2c2d2e2f
				3a3b3c3d3e3f
				4a4b4c4d4e4f
				5a5b5c5d5e5f
				6a6b6c6d6e6f
				7a7b7c7d7e7f
				8a8b8c8d8e8f
			`)

			keystrokes := []keystroke{
				{
					input: "",
					expected: heredoc(`
						#a1b1c1d1e
						2a2b2c2d2e
						3a3b3c3d3e
						4a4b4c4d4e
						5a5b5c5d5e
						6a6b6c6d6e
						mode: NOR
						
					`),
				},
				{
					input: "h",
					expected: heredoc(`
						#a1b1c1d1e
						2a2b2c2d2e
						3a3b3c3d3e
						4a4b4c4d4e
						5a5b5c5d5e
						6a6b6c6d6e
						mode: NOR
						
					`),
				},
				{
					input: "k",
					expected: heredoc(`
						#a1b1c1d1e
						2a2b2c2d2e
						3a3b3c3d3e
						4a4b4c4d4e
						5a5b5c5d5e
						6a6b6c6d6e
						mode: NOR
						
					`),
				},
				{
					input: "l",
					expected: heredoc(`
						1#1b1c1d1e
						2a2b2c2d2e
						3a3b3c3d3e
						4a4b4c4d4e
						5a5b5c5d5e
						6a6b6c6d6e
						mode: NOR
						
					`),
				},
				{
					input: "k",
					expected: heredoc(`
						1#1b1c1d1e
						2a2b2c2d2e
						3a3b3c3d3e
						4a4b4c4d4e
						5a5b5c5d5e
						6a6b6c6d6e
						mode: NOR
						
					`),
				},
				{
					input: "h",
					expected: heredoc(`
						#a1b1c1d1e
						2a2b2c2d2e
						3a3b3c3d3e
						4a4b4c4d4e
						5a5b5c5d5e
						6a6b6c6d6e
						mode: NOR
						
					`),
				},
				{
					input: "lllll",
					expected: heredoc(`
						1a1b1#1d1e
						2a2b2c2d2e
						3a3b3c3d3e
						4a4b4c4d4e
						5a5b5c5d5e
						6a6b6c6d6e
						mode: NOR
						
					`),
				},
				{
					input: "llll",
					expected: heredoc(`
						1a1b1c1d1#
						2a2b2c2d2e
						3a3b3c3d3e
						4a4b4c4d4e
						5a5b5c5d5e
						6a6b6c6d6e
						mode: NOR
						
					`),
				},
				{
					input: "l",
					// right scroll
					expected: heredoc(`
						a1b1c1d1e#
						a2b2c2d2e2
						a3b3c3d3e3
						a4b4c4d4e4
						a5b5c5d5e5
						a6b6c6d6e6
						mode: NOR
						
					`),
				},
				{
					input: "l",
					expected: heredoc(`
						1b1c1d1e1#
						2b2c2d2e2f
						3b3c3d3e3f
						4b4c4d4e4f
						5b5c5d5e5f
						6b6c6d6e6f
						mode: NOR
						
					`),
				},
				{
					// stops at the right edge
					input: "l",
					expected: heredoc(`
						1b1c1d1e1#
						2b2c2d2e2f
						3b3c3d3e3f
						4b4c4d4e4f
						5b5c5d5e5f
						6b6c6d6e6f
						mode: NOR
						
					`),
				},
				{
					input: "j",
					expected: heredoc(`
						1b1c1d1e1f
						2b2c2d2e2#
						3b3c3d3e3f
						4b4c4d4e4f
						5b5c5d5e5f
						6b6c6d6e6f
						mode: NOR
						
					`),
				},
				{
					input: "j",
					expected: heredoc(`
						1b1c1d1e1f
						2b2c2d2e2f
						3b3c3d3e3#
						4b4c4d4e4f
						5b5c5d5e5f
						6b6c6d6e6f
						mode: NOR
						
					`),
				},
				{
					input: "j",
					expected: heredoc(`
						1b1c1d1e1f
						2b2c2d2e2f
						3b3c3d3e3f
						4b4c4d4e4#
						5b5c5d5e5f
						6b6c6d6e6f
						mode: NOR
						
					`),
				},
				{
					input: "j",
					expected: heredoc(`
						1b1c1d1e1f
						2b2c2d2e2f
						3b3c3d3e3f
						4b4c4d4e4f
						5b5c5d5e5#
						6b6c6d6e6f
						mode: NOR
						
					`),
				},
				{
					input: "j",
					expected: heredoc(`
						1b1c1d1e1f
						2b2c2d2e2f
						3b3c3d3e3f
						4b4c4d4e4f
						5b5c5d5e5f
						6b6c6d6e6#
						mode: NOR
						
					`),
				},
				{
					// down scroll
					input: "j",
					expected: heredoc(`
						2b2c2d2e2f
						3b3c3d3e3f
						4b4c4d4e4f
						5b5c5d5e5f
						6b6c6d6e6f
						7b7c7d7e7#
						mode: NOR
						
					`),
				},
				{
					input: "j",
					expected: heredoc(`
						3b3c3d3e3f
						4b4c4d4e4f
						5b5c5d5e5f
						6b6c6d6e6f
						7b7c7d7e7f
						8b8c8d8e8#
						mode: NOR
						
					`),
				},
				{
					// stops at the bottom line
					input: "j",
					expected: heredoc(`
						3b3c3d3e3f
						4b4c4d4e4f
						5b5c5d5e5f
						6b6c6d6e6f
						7b7c7d7e7f
						8b8c8d8e8#
						mode: NOR
						
					`),
				},
				{
					input: "h",
					expected: heredoc(`
						3b3c3d3e3f
						4b4c4d4e4f
						5b5c5d5e5f
						6b6c6d6e6f
						7b7c7d7e7f
						8b8c8d8e#f
						mode: NOR
						
					`),
				},
				{
					input: "hhh",
					expected: heredoc(`
						3b3c3d3e3f
						4b4c4d4e4f
						5b5c5d5e5f
						6b6c6d6e6f
						7b7c7d7e7f
						8b8c8#8e8f
						mode: NOR
						
					`),
				},
				{
					input: "kkk",
					expected: heredoc(`
						3b3c3d3e3f
						4b4c4d4e4f
						5b5c5#5e5f
						6b6c6d6e6f
						7b7c7d7e7f
						8b8c8d8e8f
						mode: NOR
						
					`),
				},
				{
					input: "hhhhh",
					expected: heredoc(`
						3b3c3d3e3f
						4b4c4d4e4f
						#b5c5d5e5f
						6b6c6d6e6f
						7b7c7d7e7f
						8b8c8d8e8f
						mode: NOR
						
					`),
				},
				{
					input: "h",
					expected: heredoc(`
						a3b3c3d3e3
						a4b4c4d4e4
						#5b5c5d5e5
						a6b6c6d6e6
						a7b7c7d7e7
						a8b8c8d8e8
						mode: NOR
						
					`),
				},
				{
					input: "h",
					expected: heredoc(`
						3a3b3c3d3e
						4a4b4c4d4e
						#a5b5c5d5e
						6a6b6c6d6e
						7a7b7c7d7e
						8a8b8c8d8e
						mode: NOR
						
					`),
				},
				{
					// stops at the left edge
					input: "h",
					expected: heredoc(`
						3a3b3c3d3e
						4a4b4c4d4e
						#a5b5c5d5e
						6a6b6c6d6e
						7a7b7c7d7e
						8a8b8c8d8e
						mode: NOR
						
					`),
				},
				{
					input: "kk",
					expected: heredoc(`
						#a3b3c3d3e
						4a4b4c4d4e
						5a5b5c5d5e
						6a6b6c6d6e
						7a7b7c7d7e
						8a8b8c8d8e
						mode: NOR
						
					`),
				},
				{
					input: "k",
					expected: heredoc(`
						#a2b2c2d2e
						3a3b3c3d3e
						4a4b4c4d4e
						5a5b5c5d5e
						6a6b6c6d6e
						7a7b7c7d7e
						mode: NOR
						
					`),
				},
				{
					input: "k",
					expected: heredoc(`
						#a1b1c1d1e
						2a2b2c2d2e
						3a3b3c3d3e
						4a4b4c4d4e
						5a5b5c5d5e
						6a6b6c6d6e
						mode: NOR
						
					`),
				},
			}

			test(t, 8, 10, content, keystrokes)
		})

		t.Run("hjkl with with tab", func(t *testing.T) {
			content := heredoc(`
				1a1b1c1d1e1f
					2c2d2e2f
						3e3f
				
						5e5f
						6e6f
					7c7d7e7f
				8a8b8c8d8e8f
			`)

			keystrokes := []keystroke{
				{
					input: "",
					expected: heredoc(`
						#a1b1c1d1e
						    2c2d2e
						        3e
						
						        5e
						        6e
						mode: NOR
						
					`),
				},
				{
					input: "j",
					expected: heredoc(`
						1a1b1c1d1e
						#   2c2d2e
						        3e
						
						        5e
						        6e
						mode: NOR
						
					`),
				},
				{
					input: "l",
					expected: heredoc(`
						1a1b1c1d1e
						    #c2d2e
						        3e
						
						        5e
						        6e
						mode: NOR
						
					`),
				},
				{
					input: "h",
					expected: heredoc(`
						1a1b1c1d1e
						#   2c2d2e
						        3e
						
						        5e
						        6e
						mode: NOR
						
					`),
				},
				{
					input: "j",
					expected: heredoc(`
						1a1b1c1d1e
						    2c2d2e
						#       3e
						
						        5e
						        6e
						mode: NOR
						
					`),
				},
				{
					input: "j",
					expected: heredoc(`
						1a1b1c1d1e
						    2c2d2e
						        3e
						#
						        5e
						        6e
						mode: NOR
						
					`),
				},
				{
					input: "h",
					expected: heredoc(`
						1a1b1c1d1e
						    2c2d2e
						        3e
						#
						        5e
						        6e
						mode: NOR
						
					`),
				},
				{
					input: "l",
					expected: heredoc(`
						1a1b1c1d1e
						    2c2d2e
						        3e
						#
						        5e
						        6e
						mode: NOR
						
					`),
				},
				{
					input: "j",
					expected: heredoc(`
						1a1b1c1d1e
						    2c2d2e
						        3e
						
						#       5e
						        6e
						mode: NOR
						
					`),
				},
				{
					input: "l",
					expected: heredoc(`
						1a1b1c1d1e
						    2c2d2e
						        3e
						
						    #   5e
						        6e
						mode: NOR
						
					`),
				},
				{
					input: "l",
					expected: heredoc(`
						1a1b1c1d1e
						    2c2d2e
						        3e
						
						        #e
						        6e
						mode: NOR
						
					`),
				},
				{
					input: "l",
					expected: heredoc(`
						1a1b1c1d1e
						    2c2d2e
						        3e
						
						        5#
						        6e
						mode: NOR
						
					`),
				},
				{
					input: "l",
					expected: heredoc(`
						a1b1c1d1e1
						   2c2d2e2
						       3e3
						
						       5e#
						       6e6
						mode: NOR
						
					`),
				},
				{
					input: "l",
					expected: heredoc(`
						1b1c1d1e1f
						  2c2d2e2f
						      3e3f
						
						      5e5#
						      6e6f
						mode: NOR
						
					`),
				},
				{
					input: "l",
					expected: heredoc(`
						1b1c1d1e1f
						  2c2d2e2f
						      3e3f
						
						      5e5#
						      6e6f
						mode: NOR
						
					`),
				},
				{
					input: "k",
					expected: heredoc(`
						1b1c1d1e1f
						  2c2d2e2f
						      3e3f
						#
						      5e5f
						      6e6f
						mode: NOR
						
					`),
				},
				{
					input: "k",
					expected: heredoc(`
						1b1c1d1e1f
						  2c2d2e2f
						      3e3#
						
						      5e5f
						      6e6f
						mode: NOR
						
					`),
				},
				{
					input: "hh",
					expected: heredoc(`
						1b1c1d1e1f
						  2c2d2e2f
						      3#3f
						
						      5e5f
						      6e6f
						mode: NOR
						
					`),
				},
				{
					input: "j",
					expected: heredoc(`
						1b1c1d1e1f
						  2c2d2e2f
						      3e3f
						#
						      5e5f
						      6e6f
						mode: NOR
						
					`),
				},
				{
					input: "j",
					expected: heredoc(`
						1b1c1d1e1f
						  2c2d2e2f
						      3e3f
						
						      5#5f
						      6e6f
						mode: NOR
						
					`),
				},
				{
					input: "h",
					expected: heredoc(`
						1b1c1d1e1f
						  2c2d2e2f
						      3e3f
						
						      #e5f
						      6e6f
						mode: NOR
						
					`),
				},
				{
					input: "h",
					expected: heredoc(`
						1b1c1d1e1f
						  2c2d2e2f
						      3e3f
						
						  #   5e5f
						      6e6f
						mode: NOR
						
					`),
				},
			}
			test(t, 8, 10, content, keystrokes)
		})
	})
}

/*
 * test utilities
 */

// virtual terminal on memory
type virtterm struct {
	lines      [][]byte
	row, col   int
	curX, curY int
}

func newvirtterm(row, col int) *virtterm {
	return &virtterm{row: row, col: col}
}

func (t *virtterm) init() (func(), error) {
	if t.row == 0 || t.col == 0 {
		panic("row, col must be set before init")
	}

	t.lines = make([][]byte, t.row)
	return func() {}, nil
}

func (t *virtterm) windowsize() (int, int, error) {
	return t.row, t.col, nil
}

func (t *virtterm) refresh() {
	t.lines = make([][]byte, t.row)
}

func (t *virtterm) clearline() {
	t.lines[t.curY] = []byte{}
}

func (t *virtterm) putcursor(x, y int) {
	t.curX = x
	t.curY = y
}

func (t *virtterm) Write(data []byte) (n int, err error) {
	t.lines[t.curY] = append(t.lines[t.curY][:t.curX], append(data, t.lines[t.curY][t.curX:]...)...)
	return len(data), nil
}

func (t *virtterm) String() string {
	s := ""
	for y := range t.lines {
		line := bytes.Runes(t.lines[y])

		if len(line) == 0 && y != len(t.lines)-1 {
			if y == t.curY && t.curX == 0 {
				s += "#"
			}

			s += "\n"
			continue
		}

		for x := range line {
			if x == t.curX && y == t.curY {
				s += "#" // render cursor as #
			} else {
				s += string(line[x])
			}

			// if last character on NOT last line, append \n
			if x == len(line)-1 && y != len(t.lines)-1 {
				s += "\n"
			}
		}
	}

	return s
}

// virtual stdin on memory
type virtstdin struct {
	r *io.PipeReader
	w *io.PipeWriter
}

func newvirtstdin() *virtstdin {
	r, w := io.Pipe()
	return &virtstdin{r: r, w: w}
}

func (i *virtstdin) Read(p []byte) (n int, err error) {
	return i.r.Read(p)
}

func (i *virtstdin) input(s string) (int, error) {
	return i.w.Write([]byte(s))
}

func heredoc(raw string) string {
	lines := strings.Split(raw, "\n")

	// skip first and last line
	lines = lines[1 : len(lines)-1]

	minIndent := math.MaxInt

	// find minimum indent
	for _, line := range lines {
		tabs := 0
		for _, c := range line {
			if c == '\t' {
				tabs++
			} else {
				break
			}
		}

		if tabs < minIndent {
			minIndent = tabs
		}
	}

	// remove indent from every line
	for i, line := range lines {
		lines[i] = line[minIndent:]
	}

	return strings.Join(lines, "\n")
}
