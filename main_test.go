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
	content := heredoc(`
		package main
		
		import (
			"bufio"
			"fmt"
			"os"
			"strings"
		)
		
		func main() {
			scanner := bufio.NewScanner(strings.NewReader("gopher"))
			for scanner.Scan() {
				fmt.Println(len(scanner.Bytes()) == 6)
			}
			if err := scanner.Err(); err != nil {
				fmt.Fprintln(os.Stderr, "shouldn't see an error scanning a string")
			}
		}
	`)

	tests := []struct {
		name     string
		inputs   func(in *virtstdin)
		content  string
		expected string
		row, col int
	}{
		{
			name:    "at first",
			inputs:  func(in *virtstdin) {},
			content: content,
			expected: heredoc(`
					#ackage ma
					
					import (
					    "bufio
					    "fmt"
					    "os"
					mode: NORMAL
					
				`),
			row: 8,
			col: 10,
		},
		{
			name: "move and scroll",
			inputs: func(in *virtstdin) {
				in.input("j")
				in.input("j")
				in.input("j")
			},
			content: content,
			expected: heredoc(`
					package ma
					
					import (
					#   "bufio
					    "fmt"
					    "os"
					mode: NORMAL
					
				`),
			row: 8,
			col: 10,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			term := newvirtterm(tc.row, tc.col)
			in := newvirtstdin()
			go editor(term, strings.NewReader(tc.content), in)
			tc.inputs(in)
			time.Sleep(30 * time.Millisecond) // wait for terminal in another goroutine finishes its work
			assert(t, term, tc.expected)
		})
	}
}

/*
 * test utilities
 */

// assertion
func assert(t *testing.T, term *virtterm, expected string) {
	t.Helper()
	got := term.String()
	if got != expected {
		t.Fatalf("========== expected ==========\n%v\n==========\n========== got ==========\n%v\n==========", expected, got)
	}
}

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
