package main

import (
	"io"
	"strings"
	"testing"
	"time"
)

/*
 * test
 */

func Test_editor(t *testing.T) {
	content := `package main

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
}`

	testdone := make(chan struct{})
	term := newvirtterm(8, 10)
	in := newvirtstdin()
	go editor(term, strings.NewReader(content), in)
	go func() {
		in.input("j")
		in.input("j")
		in.input("j")
		time.Sleep(100 * time.Millisecond) // wait for terminal in another goroutine finishes its work

		if term.curX != 0 {
			t.Errorf("cursor position x not expected: %v", term.curX)
		}

		if term.curY != 3 {
			t.Errorf("cursor position y not expected: %v", term.curY)
		}

		testdone <- struct{}{}
	}()

	select {
	case <-testdone:
	}
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

	for range t.row {
		t.lines = append(t.lines, []byte{})
	}

	return func() {}, nil
}

func (t *virtterm) windowsize() (int, int, error) {
	return t.row, t.col, nil
}

func (t *virtterm) refresh() {
	for range t.row {
		t.lines = append(t.lines, []byte{})
	}
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
