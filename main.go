package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"syscall"
	"unicode"
	"unicode/utf8"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

var _debug bool

type mode int

const (
	normal mode = iota + 1
	insert
)

func (m mode) String() string {
	switch m {
	case normal:
		return "NORMAL"
	case insert:
		return "INSERT"
	default:
		panic("unknown mode")
	}
}

/*
 * character
 */

// character represents a single character, such as "a", "1", "#", space, tab, etc.
type character struct {
	r rune
	// true if the character represents Tab.
	tab       bool
	dispWidth int
	str       string
}

func newCharacter(r rune) *character {
	if r == '\t' {
		// Raw Tab changes its size dynamically and it's hard to properly display, so
		// Tab is treated as 4 spaces.
		return &character{tab: true, dispWidth: 4, str: "    "}
	}

	if fullwidth(r) {
		return &character{r: r, dispWidth: 2, str: string(r)}
	}

	return &character{r: r, dispWidth: 1, str: string(r)}
}

func (c *character) String() string {
	return c.str
}

/*
 * line
 */

type line struct {
	buffer []*character
}

func emptyline() *line {
	return &line{buffer: []*character{}}
}

func (l *line) String() string {
	// todo: can be cached
	sb := strings.Builder{}
	for _, c := range l.buffer {
		sb.WriteString(c.String())
	}
	return sb.String()
}

func (l *line) indexByCursor(cursor, offset int) int {
	x := -offset
	for i, c := range l.buffer {
		x += c.dispWidth
		if x >= cursor+1 {
			return i
		}
	}

	panic("should not come here")
}

func (l *line) length() int {
	// todo: can be cached
	return len(l.buffer)
}

func (l *line) width() int {
	// todo: can be cached
	x := 0
	for i := range l.length() {
		x += l.buffer[i].dispWidth
	}
	return x
}

func (l *line) insertChar(c *character, at int) {
	l.buffer = slices.Insert(l.buffer, at, c)
}

func (l *line) deleteChar(at int) {
	l.buffer = slices.Delete(l.buffer, at, at+1)
}

func (l *line) cut(from, limit int) string {
	s := l.String()
	length := len(s)

	if length < from {
		return ""
	}

	to := from + limit

	if length < to {
		to = length
	}

	return l.String()[from:to]
}

/*
 * screen
 */

type cursorpos struct {
	x, y int
}

type screen struct {
	term terminal

	// terminal screen height and width
	maxRows int
	maxCols int

	// current mode
	mode mode

	// whole text contents in the current buffer.
	lines []*line
	// file  *os.File

	// cursor position based on the character
	cursor *cursorpos

	// the index indicating the upper left corner of the rectangle that is
	// cut from the text and actually displayed in the terminal.
	dispFromX int
	dispFromY int

	// state dirtiness. if it's dirty, it means the actual terminal screen and
	// the state of screen on memory are not synchronized. It's done in synchronize().
	modechanged     bool
	dispzoneChanged bool
	changedlines    []int
}

func (s *screen) changeMode(mode mode) {
	s.mode = mode
	s.modechanged = true
}

func (s *screen) currentChar() *character {
	l := s.currentLine()
	return l.buffer[l.indexByCursor(s.cursor.x, s.dispFromX)]
}

func (s *screen) currentLine() *line {
	return s.lines[s.cursor.y]
}

func (s *screen) synchronize() {
	// update status line
	if s.modechanged {
		s.term.putcursor(0, s.maxRows)
		fmt.Fprint(s.term, fmt.Sprintf("mode: %v", s.mode))
	}

	// update lines
	if s.dispzoneChanged {
		// when topline is changed, all shown lines must be updated
		for i := 0; i < min(len(s.lines), s.maxRows); i++ {
			s.term.putcursor(0, i)
			s.term.clearline()
			line := s.lines[s.dispFromY+i]
			fmt.Fprint(s.term, line.cut(s.dispFromX, s.maxCols))
		}
	} else if len(s.changedlines) != 0 {
		// when topline is not changed but some lines are changed, change only them
		slices.Sort(s.changedlines)
		for _, l := range slices.Compact(s.changedlines) {
			// if changed line is above topline, no need to re-render.
			if l < s.dispFromY {
				continue
			}

			// if changed line is below screen bottom, no need to re-render.
			if s.maxRows < l-s.dispFromY {
				continue
			}

			s.term.putcursor(0, l-s.dispFromY)
			s.term.clearline()
			line := s.lines[l-s.dispFromY]
			fmt.Fprint(s.term, line.cut(s.dispFromX, s.maxCols))
		}
	}

	// update cursor position
	x := s.cursor.x
	lim := s.currentLine().width() - s.dispFromX
	if lim <= 0 {
		x = 0
	} else if lim-1 < x {
		x = lim - 1
	}

	s.term.putcursor(x, s.cursor.y-s.dispFromY)

	s.modechanged = false
	s.dispzoneChanged = false
	s.changedlines = []int{}
}

type direction int

const (
	up direction = iota + 1
	down
	left
	right
)

func (s *screen) moveCursor(direction direction) {
	switch direction {
	case up:
		switch {
		case s.cursor.y == 0 && s.dispFromY == 0:
			// case s.currentLineIndex() == 0:
			// when cursor is on the top and the first line is shown at the top,
			// do nothing.
			return

		case s.cursor.y-s.dispFromY == 0 && 0 < s.dispFromY:
			// when cursor is on the top but still upper line exists,
			// scroll 1 line up but the cursor itself does not move.
			s.cursor.y--
			s.dispFromY--
			s.dispzoneChanged = true

		default:
			// just move the cursor itself
			s.cursor.y--
		}

	case down:
		switch {
		case s.cursor.y == len(s.lines)-1:
			// when there are no more lines, do nothing.
			return

		case s.cursor.y-s.dispFromY == s.maxRows-1 && s.cursor.y < len(s.lines)-1:
			// when cursor is at the bottom but lines still exist, scroll down.
			s.cursor.y++
			s.dispFromY++
			s.dispzoneChanged = true

		default:
			// just move the cursor itself
			s.cursor.y++
		}

	case left:
		lim := s.currentLine().width()
		if lim == 0 {
			s.cursor.x = 0
		} else if lim <= s.dispFromX {
			s.cursor.x = 0
		} else if lim <= s.cursor.x {
			s.cursor.x = lim - s.dispFromX - 1
		}

		switch {
		case s.cursor.x == 0 && s.dispFromX == 0:
			return

		case s.cursor.x+s.dispFromX >= s.currentLine().width():
			width := s.currentLine().width()
			if width == 0 {
				s.dispFromX = 0
			} else {
				s.dispFromX = width - 1
			}
			s.dispzoneChanged = true

		case s.cursor.x == 0 && 0 < s.dispFromX:
			// scroll to left
			curidx := s.currentLine().indexByCursor(s.cursor.x, s.dispFromX)
			dispwidth := s.currentLine().buffer[curidx-1].dispWidth
			s.dispFromX -= dispwidth
			s.dispzoneChanged = true

		case s.cursor.x != 0 && s.currentLine().indexByCursor(s.cursor.x, s.dispFromX) == 0:
			return

		default:
			curidx := s.currentLine().indexByCursor(s.cursor.x, s.dispFromX)
			dispwidth := s.currentLine().buffer[curidx-1].dispWidth

			l := s.currentLine()

			alignedX := 0
			for i := range curidx {
				alignedX += l.buffer[i].dispWidth

			}

			s.cursor.x -= dispwidth + (s.cursor.x + s.dispFromX - alignedX)
		}

	case right:
		lim := s.currentLine().width()
		if lim == 0 {
			s.cursor.x = 0
		} else if lim <= s.dispFromX {
			s.cursor.x = lim - 1
		} else if lim <= s.cursor.x {
			s.cursor.x = lim - s.dispFromX - 1
		}

		curline := s.currentLine()

		switch {
		case s.cursor.x+s.dispFromX > curline.width():
			return

		case s.cursor.x+s.dispFromX == curline.width()-1:
			return

		case s.cursor.x == s.maxCols-1 && s.cursor.x+s.dispFromX < curline.width()-1:
			dispwidth := s.currentChar().dispWidth
			s.dispFromX += dispwidth
			s.dispzoneChanged = true

		default:
			l := s.currentLine()
			if l.length() == 0 {
				return
			}

			dispwidth := s.currentChar().dispWidth

			idx := l.indexByCursor(s.cursor.x, s.dispFromX)
			alignedX := 0
			for i := range idx {
				alignedX += l.buffer[i].dispWidth

			}

			s.cursor.x += dispwidth - (s.cursor.x + s.dispFromX - alignedX)
		}

	default:
		panic("invalid direction is passed")
	}
}

func (s *screen) insertline(direction direction) {
	switch direction {
	case up:
		s.lines = slices.Insert(s.lines, s.cursor.y, emptyline())
	case down:
		s.lines = slices.Insert(s.lines, s.cursor.y+1, emptyline())
	default:
		panic("invalid direction is passed to addline")
	}

	for i := s.cursor.y; i < s.maxRows; i++ {
		s.changedlines = append(s.changedlines, i)
	}
}

func (s *screen) insertChar(c *character) {
	s.currentLine().insertChar(c, s.cursor.x+s.dispFromX)
	s.changedlines = append(s.changedlines, s.cursor.y)
}

func (s *screen) deleteCurrentChar() {
	s.currentLine().deleteChar(s.cursor.x + s.dispFromX)
	s.changedlines = append(s.changedlines, s.cursor.y)
}

func (s *screen) debug() {
	debug("maxRows: %v, maxCols: %v, mode: %v, cursor: {x: %v, y: %v}, lines_count: %v, curlinelength: %v, curlinewidth: %v, dispFromX: %v, dispFromY: %v\n", s.maxRows, s.maxCols, s.mode, s.cursor.x, s.cursor.y, len(s.lines), s.currentLine().length(), s.currentLine().width(), s.dispFromX, s.dispFromY)
}

func main() {
	dbg := os.Getenv("TURTLE_DEBUG")
	if dbg != "" {
		_debug = true
	}

	flag.Parse()
	args := flag.Args()

	var r io.Reader

	switch len(args) {
	case 0:
		r = strings.NewReader("")

	case 1:
		filename := args[0]
		f, err := os.OpenFile(filename, os.O_RDWR|os.O_CREATE, 0644)
		if err != nil {
			panic(err)
		}
		defer f.Close()

		r = f

	default:
		fmt.Println("more than 2 args are passed")
		return
	}

	term := &unixVT100term{out: os.Stdout}
	editor(term, r, os.Stdin)
}

func editor(term terminal, text io.Reader, input io.Reader) {
	fin, err := term.init()
	if err != nil {
		fin()
		panic(fin)
	}

	defer fin()
	defer term.refresh()

	term.refresh()

	/*
	 * Prepare editor state
	 */

	row, col, err := term.windowsize()
	if err != nil {
		panic(err)
	}

	s := &screen{
		term:            term,
		maxRows:         row - 2,
		maxCols:         col,
		mode:            normal,
		cursor:          &cursorpos{x: 0, y: 0},
		dispFromX:       0,
		dispFromY:       0,
		modechanged:     true,
		dispzoneChanged: true,
	}

	{
		s.lines = []*line{emptyline()} // initialize first line
		currentline := 0
		reader := bufio.NewReader(text)
		for {
			r, _, err := reader.ReadRune()
			if err == io.EOF {
				break
			}

			if err != nil {
				panic(fmt.Sprintf("read a file: %v", err))
			}

			switch r {
			case '\n':
				s.lines = append(s.lines, emptyline())
				currentline++

			default:
				s.lines[currentline].buffer = append(s.lines[currentline].buffer, newCharacter(r))
			}
		}
	}

	s.synchronize()

	/*
	 * start editor main routine
	 */

	reader := bufio.NewReader(input)
	for {
		// a unicode character takes 4 bytes at most
		b := make([]byte, 4)
		n, err := reader.Read(b)
		if err == io.EOF {
			continue
		}

		if n == -1 {
			panic("cannot read")
		}

		if err != nil {
			panic(err)
		}

		r, _ := utf8.DecodeRune(b)

		isArrowKey, dir := isarrowkey(b)

		switch s.mode {
		case normal:
			switch {
			case r == ctrl('q'):
				goto finish

			case r == 'i':
				s.changeMode(insert)

			case r == 'd':
				s.deleteCurrentChar()

			case r == 'o':
				s.insertline(down)
				s.moveCursor(down)
				s.changeMode(insert)

			case r == 'O':
				s.insertline(up)
				s.moveCursor(up)
				s.changeMode(insert)

			case r == 'h', isArrowKey && dir == left:
				s.moveCursor(left)

			case r == 'j', isArrowKey && dir == down:
				s.moveCursor(down)

			case r == 'k', isArrowKey && dir == up:
				s.moveCursor(up)

			case r == 'l', isArrowKey && dir == right:
				s.moveCursor(right)
			}

		case insert:
			switch {
			case r == 27: // Esc
				s.changeMode(normal)

			case isArrowKey && dir == left:
				s.moveCursor(left)

			case isArrowKey && dir == down:
				s.moveCursor(down)

			case isArrowKey && dir == up:
				s.moveCursor(up)

			case isArrowKey && dir == right:
				s.moveCursor(right)

			case unicode.IsControl(r):
				if _debug {
					debug("control key is pressed: %v\n", r)
				}

			default:
				s.insertChar(newCharacter(r))
				s.cursor.x++
			}

		default:
			panic("unknown mode")
		}

		s.synchronize()

		if _debug {
			s.debug()
		}
	}

finish:
	term.refresh()
	fmt.Fprintf(os.Stdout, "\n")
	term.putcursor(0, 0)
}

func isarrowkey(bs []byte) (bool, direction) {
	if bs[0] != '\x1b' || bs[1] != '[' {
		return false, 0
	}

	switch bs[2] {
	case 'A':
		return true, up
	case 'B':
		return true, down
	case 'C':
		return true, right
	case 'D':
		return true, left

		// todo: handle other keys
	}

	panic("unknown key came as arrow key")
}

func debug(format string, a ...any) (int, error) {
	return fmt.Fprintf(os.Stderr, format, a...)
}

func ctrl(input byte) rune {
	return rune(input & 0x1f)
}

type terminal interface {
	io.Writer

	init() (func(), error)
	windowsize() (int, int, error)
	refresh()
	clearline()
	putcursor(x, y int)
}

type unixVT100term struct {
	out io.Writer
}

func (t *unixVT100term) init() (func(), error) {
	oldstate, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return func() {}, err
	}
	return func() { term.Restore(int(os.Stdin.Fd()), oldstate) }, nil
}

func (t *unixVT100term) windowsize() (int, int, error) {
	winsize, err := unix.IoctlGetWinsize(syscall.Stdout, syscall.TIOCGWINSZ)
	if err != nil {
		return 0, 0, err
	}

	return int(winsize.Row), int(winsize.Col), nil
}

func (t *unixVT100term) refresh() {
	fmt.Fprint(t, "\x1b[2J")
}

func (t *unixVT100term) clearline() {
	fmt.Fprint(t, "\x1b[K")
}

func (t *unixVT100term) putcursor(x, y int) {
	fmt.Fprint(t, fmt.Sprintf("\x1b[%v;%vH", y+1, x+1))
}

func (t *unixVT100term) Write(p []byte) (int, error) {
	return t.out.Write(p)
}

/*
 * unicode utilities
 */

func fullwidth(r rune) bool {
	fullwidth := &unicode.RangeTable{
		R16: []unicode.Range16{
			{0x3000, 0x3000, 1},
			{0xff01, 0xff60, 1},
			{0xffe0, 0xffe6, 1},
		},
	}

	wide := &unicode.RangeTable{
		R16: []unicode.Range16{
			{0x1100, 0x115F, 1},
			{0x2329, 0x232A, 1},
			{0x2E80, 0x2FFB, 1},
			{0x3001, 0x303E, 1},
			{0x3041, 0x33FF, 1},
			{0x3400, 0x4DB5, 1},
			{0x4E00, 0x9FBB, 1},
			{0xA000, 0xA4C6, 1},
			{0xAC00, 0xD7A3, 1},
			{0xF900, 0xFAD9, 1},
			{0xFE10, 0xFE19, 1},
			{0xFE30, 0xFE6B, 1},
		},
		R32: []unicode.Range32{
			{0x20000, 0x2A6D6, 1},
			{0x2A6D7, 0x2F7FF, 1},
			{0x2F800, 0x2FA1D, 1},
			{0x2FA1E, 0x2FFFD, 1},
			{0x30000, 0x3FFFD, 1},
		},
	}

	ambiguous := &unicode.RangeTable{
		R16: []unicode.Range16{
			{0x00A1, 0x00A1, 1},
			{0x00A4, 0x00A4, 1},
			{0x00A7, 0x00A8, 1},
			{0x00AA, 0x00AA, 1},
			{0x00AD, 0x00AE, 1},
			{0x00B0, 0x00B4, 1},
			{0x00B6, 0x00BA, 1},
			{0x00BC, 0x00BF, 1},
			{0x00C6, 0x00C6, 1},
			{0x00D0, 0x00D0, 1},
			{0x00D7, 0x00D8, 1},
			{0x00DE, 0x00E1, 1},
			{0x00E6, 0x00E6, 1},
			{0x00E8, 0x00EA, 1},
			{0x00EC, 0x00ED, 1},
			{0x00F0, 0x00F0, 1},
			{0x00F2, 0x00F3, 1},
			{0x00F7, 0x00FA, 1},
			{0x00FC, 0x00FC, 1},
			{0x00FE, 0x00FE, 1},
			{0x0101, 0x0101, 1},
			{0x0111, 0x0111, 1},
			{0x0113, 0x0113, 1},
			{0x011B, 0x011B, 1},
			{0x0126, 0x0127, 1},
			{0x012B, 0x012B, 1},
			{0x0131, 0x0133, 1},
			{0x0138, 0x0138, 1},
			{0x013F, 0x0142, 1},
			{0x0144, 0x0144, 1},
			{0x0148, 0x014B, 1},
			{0x014D, 0x014D, 1},
			{0x0152, 0x0153, 1},
			{0x0166, 0x0167, 1},
			{0x016B, 0x016B, 1},
			{0x01CE, 0x01CE, 1},
			{0x01D0, 0x01D0, 1},
			{0x01D2, 0x01D2, 1},
			{0x01D4, 0x01D4, 1},
			{0x01D6, 0x01D6, 1},
			{0x01D8, 0x01D8, 1},
			{0x01DA, 0x01DA, 1},
			{0x01DC, 0x01DC, 1},
			{0x0251, 0x0251, 1},
			{0x0261, 0x0261, 1},
			{0x02C4, 0x02C4, 1},
			{0x02C7, 0x02C7, 1},
			{0x02C9, 0x02CB, 1},
			{0x02CD, 0x02CD, 1},
			{0x02D0, 0x02D0, 1},
			{0x02D8, 0x02DB, 1},
			{0x02DD, 0x02DD, 1},
			{0x02DF, 0x02DF, 1},
			{0x0300, 0x036F, 1},
			{0x0391, 0x03A9, 1},
			{0x03B1, 0x03C1, 1},
			{0x03C3, 0x03C9, 1},
			{0x0401, 0x0401, 1},
			{0x0410, 0x044F, 1},
			{0x0451, 0x0451, 1},
			{0x2010, 0x2010, 1},
			{0x2013, 0x2016, 1},
			{0x2018, 0x2019, 1},
			{0x201C, 0x201D, 1},
			{0x2020, 0x2022, 1},
			{0x2024, 0x2027, 1},
			{0x2030, 0x2030, 1},
			{0x2032, 0x2033, 1},
			{0x2035, 0x2035, 1},
			{0x203B, 0x203B, 1},
			{0x203E, 0x203E, 1},
			{0x2074, 0x2074, 1},
			{0x207F, 0x207F, 1},
			{0x2081, 0x2084, 1},
			{0x20AC, 0x20AC, 1},
			{0x2103, 0x2103, 1},
			{0x2105, 0x2105, 1},
			{0x2109, 0x2109, 1},
			{0x2113, 0x2113, 1},
			{0x2116, 0x2116, 1},
			{0x2121, 0x2122, 1},
			{0x2126, 0x2126, 1},
			{0x212B, 0x212B, 1},
			{0x2153, 0x2154, 1},
			{0x215B, 0x215E, 1},
			{0x2160, 0x216B, 1},
			{0x2170, 0x2179, 1},
			{0x2190, 0x2199, 1},
			{0x21B8, 0x21B9, 1},
			{0x21D2, 0x21D2, 1},
			{0x21D4, 0x21D4, 1},
			{0x21E7, 0x21E7, 1},
			{0x2200, 0x2200, 1},
			{0x2202, 0x2203, 1},
			{0x2207, 0x2208, 1},
			{0x220B, 0x220B, 1},
			{0x220F, 0x220F, 1},
			{0x2211, 0x2211, 1},
			{0x2215, 0x2215, 1},
			{0x221A, 0x221A, 1},
			{0x221D, 0x2220, 1},
			{0x2223, 0x2223, 1},
			{0x2225, 0x2225, 1},
			{0x2227, 0x222C, 1},
			{0x222E, 0x222E, 1},
			{0x2234, 0x2237, 1},
			{0x223C, 0x223D, 1},
			{0x2248, 0x2248, 1},
			{0x224C, 0x224C, 1},
			{0x2252, 0x2252, 1},
			{0x2260, 0x2261, 1},
			{0x2264, 0x2267, 1},
			{0x226A, 0x226B, 1},
			{0x226E, 0x226F, 1},
			{0x2282, 0x2283, 1},
			{0x2286, 0x2287, 1},
			{0x2295, 0x2295, 1},
			{0x2299, 0x2299, 1},
			{0x22A5, 0x22A5, 1},
			{0x22BF, 0x22BF, 1},
			{0x2312, 0x2312, 1},
			{0x2460, 0x24E9, 1},
			{0x24EB, 0x254B, 1},
			{0x2550, 0x2573, 1},
			{0x2580, 0x258F, 1},
			{0x2592, 0x2595, 1},
			{0x25A0, 0x25A1, 1},
			{0x25A3, 0x25A9, 1},
			{0x25B2, 0x25B3, 1},
			{0x25B6, 0x25B7, 1},
			{0x25BC, 0x25BD, 1},
			{0x25C0, 0x25C1, 1},
			{0x25C6, 0x25C8, 1},
			{0x25CB, 0x25CB, 1},
			{0x25CE, 0x25D1, 1},
			{0x25E2, 0x25E5, 1},
			{0x25EF, 0x25EF, 1},
			{0x2605, 0x2606, 1},
			{0x2609, 0x2609, 1},
			{0x260E, 0x260F, 1},
			{0x2614, 0x2615, 1},
			{0x261C, 0x261C, 1},
			{0x261E, 0x261E, 1},
			{0x2640, 0x2640, 1},
			{0x2642, 0x2642, 1},
			{0x2660, 0x2661, 1},
			{0x2663, 0x2665, 1},
			{0x2667, 0x266A, 1},
			{0x266C, 0x266D, 1},
			{0x266F, 0x266F, 1},
			{0x273D, 0x273D, 1},
			{0x2776, 0x277F, 1},
			{0xE000, 0xF8FF, 1},
			{0xFE00, 0xFE0F, 1},
			{0xFFFD, 0xFFFD, 1},
		},
		R32: []unicode.Range32{
			{0xE0100, 0xE01EF, 1},
			{0xF0000, 0xFFFFD, 1},
			{0x100000, 0x10FFFD, 1},
		},
	}

	return unicode.Is(fullwidth, r) || unicode.Is(wide, r) || unicode.Is(ambiguous, r)
}
