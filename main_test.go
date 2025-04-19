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

func Test_editor_j_k(t *testing.T) {
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
		input    string
		expected string
	}{
		{
			input: "",
			expected: heredoc(`
				#ackage ma
				
				import (
				    "bufio
				    "fmt"
				    "os"
				mode: NOR
				
			`),
		},
		{
			input: "k",
			expected: heredoc(`
				#ackage ma
				
				import (
				    "bufio
				    "fmt"
				    "os"
				mode: NOR
				
			`),
		},
		{
			input: "j",
			expected: heredoc(`
				package ma
				#
				import (
				    "bufio
				    "fmt"
				    "os"
				mode: NOR
				
			`),
		},
		{
			input: "j",
			expected: heredoc(`
				package ma
				
				#mport (
				    "bufio
				    "fmt"
				    "os"
				mode: NOR
				
			`),
		},
		{
			input: "j",
			expected: heredoc(`
				package ma
				
				import (
				#   "bufio
				    "fmt"
				    "os"
				mode: NOR
				
			`),
		},
		{
			input: "j",
			expected: heredoc(`
				package ma
				
				import (
				    "bufio
				#   "fmt"
				    "os"
				mode: NOR
				
			`),
		},
		{
			input: "j",
			expected: heredoc(`
				package ma
				
				import (
				    "bufio
				    "fmt"
				#   "os"
				mode: NOR
				
			`),
		},
		// down scroll
		{
			input: "j",
			expected: heredoc(`
				
				import (
				    "bufio
				    "fmt"
				    "os"
				#   "strin
				mode: NOR
				
			`),
		},
		{
			input: "j",
			expected: heredoc(`
				import (
				    "bufio
				    "fmt"
				    "os"
				    "strin
				#
				mode: NOR
				
			`),
		},
		{
			input: "j",
			expected: heredoc(`
				    "bufio
				    "fmt"
				    "os"
				    "strin
				)
				#
				mode: NOR
				
			`),
		},
		{
			input: "j",
			expected: heredoc(`
				    "fmt"
				    "os"
				    "strin
				)
				
				#unc main(
				mode: NOR
				
			`),
		},
		{
			input: "j",
			expected: heredoc(`
				    "os"
				    "strin
				)
				
				func main(
				#   scanne
				mode: NOR
				
			`),
		},
		{
			input: "j",
			expected: heredoc(`
				    "strin
				)
				
				func main(
				    scanne
				#   for sc
				mode: NOR
				
			`),
		},
		{
			input: "j",
			expected: heredoc(`
				)
				
				func main(
				    scanne
				    for sc
				#       fm
				mode: NOR
				
			`),
		},
		{
			input: "j",
			expected: heredoc(`
				
				func main(
				    scanne
				    for sc
				        fm
				#   }
				mode: NOR
				
			`),
		},
		{
			input: "j",
			expected: heredoc(`
				func main(
				    scanne
				    for sc
				        fm
				    }
				#   if err
				mode: NOR
				
			`),
		},
		{
			input: "j",
			expected: heredoc(`
				    scanne
				    for sc
				        fm
				    }
				    if err
				#       fm
				mode: NOR
				
			`),
		},
		{
			input: "j",
			expected: heredoc(`
				    for sc
				        fm
				    }
				    if err
				        fm
				#   }
				mode: NOR
				
			`),
		},
		{
			input: "j",
			expected: heredoc(`
				        fm
				    }
				    if err
				        fm
				    }
				#
				mode: NOR
				
			`),
		},
		// stop at the bottom line
		{
			input: "j",
			expected: heredoc(`
				        fm
				    }
				    if err
				        fm
				    }
				#
				mode: NOR
				
			`),
		},
		{
			input: "j",
			expected: heredoc(`
				        fm
				    }
				    if err
				        fm
				    }
				#
				mode: NOR
				
			`),
		},
		// go up
		{
			input: "k",
			expected: heredoc(`
				        fm
				    }
				    if err
				        fm
				#   }
				}
				mode: NOR
				
			`),
		},
		{
			input: "k",
			expected: heredoc(`
				        fm
				    }
				    if err
				#       fm
				    }
				}
				mode: NOR
				
			`),
		},
		{
			input: "k",
			expected: heredoc(`
				        fm
				    }
				#   if err
				        fm
				    }
				}
				mode: NOR
				
			`),
		},
		{
			input: "k",
			expected: heredoc(`
				        fm
				#   }
				    if err
				        fm
				    }
				}
				mode: NOR
				
			`),
		},
		{
			input: "k",
			expected: heredoc(`
				#       fm
				    }
				    if err
				        fm
				    }
				}
				mode: NOR
				
			`),
		},
		// up scroll
		{
			input: "k",
			expected: heredoc(`
				#   for sc
				        fm
				    }
				    if err
				        fm
				    }
				mode: NOR
				
			`),
		},
		{
			input: "k",
			expected: heredoc(`
				#   scanne
				    for sc
				        fm
				    }
				    if err
				        fm
				mode: NOR
				
			`),
		},
		{
			input: "k",
			expected: heredoc(`
				#unc main(
				    scanne
				    for sc
				        fm
				    }
				    if err
				mode: NOR
				
			`),
		},
		{
			input: "k",
			expected: heredoc(`
				#
				func main(
				    scanne
				    for sc
				        fm
				    }
				mode: NOR
				
			`),
		},
		{
			input: "k",
			expected: heredoc(`
				#
				
				func main(
				    scanne
				    for sc
				        fm
				mode: NOR
				
			`),
		},
		{
			input: "k",
			expected: heredoc(`
				#   "strin
				)
				
				func main(
				    scanne
				    for sc
				mode: NOR
				
			`),
		},
		{
			input: "k",
			expected: heredoc(`
				#   "os"
				    "strin
				)
				
				func main(
				    scanne
				mode: NOR
				
			`),
		},
		{
			input: "k",
			expected: heredoc(`
				#   "fmt"
				    "os"
				    "strin
				)
				
				func main(
				mode: NOR
				
			`),
		},
		{
			input: "k",
			expected: heredoc(`
				#   "bufio
				    "fmt"
				    "os"
				    "strin
				)
				
				mode: NOR
				
			`),
		},
		{
			input: "k",
			expected: heredoc(`
				#mport (
				    "bufio
				    "fmt"
				    "os"
				    "strin
				)
				mode: NOR
				
			`),
		},
		{
			input: "k",
			expected: heredoc(`
				#
				import (
				    "bufio
				    "fmt"
				    "os"
				    "strin
				mode: NOR
				
			`),
		},
		{
			input: "k",
			expected: heredoc(`
				#ackage ma
				
				import (
				    "bufio
				    "fmt"
				    "os"
				mode: NOR
				
			`),
		},
		// stops at the top line
		{
			input: "k",
			expected: heredoc(`
				#ackage ma
				
				import (
				    "bufio
				    "fmt"
				    "os"
				mode: NOR
				
			`),
		},
		{
			input: "k",
			expected: heredoc(`
				#ackage ma
				
				import (
				    "bufio
				    "fmt"
				    "os"
				mode: NOR
				
			`),
		},
	}

	term := newvirtterm(8, 10)
	in := newvirtstdin()
	go editor(term, strings.NewReader(content), in)

	for i, tc := range tests {
		in.input(tc.input)
		time.Sleep(1 * time.Millisecond) // wait for terminal in another goroutine finishes its work
		assert(t, i, term, tc.expected)
	}
}

/*
 * test utilities
 */

// assertion
func assert(t *testing.T, i int, term *virtterm, expected string) {
	t.Helper()
	got := term.String()
	if got != expected {
		t.Fatalf("(test %v)\n========== expected ==========\n%v\n==========\n========== got ==========\n%v\n==========", i, expected, got)
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
