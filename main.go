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
	"unsafe"
)

type mode int

const (
	normal mode = iota + 1
	insert
)

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

type line struct {
	buffer []*character
}

func newline() *line {
	return &line{buffer: []*character{}}
}

func (l *line) String() string {
	sb := strings.Builder{}
	for _, c := range l.buffer {
		sb.WriteString(c.String())
	}
	return sb.String()
}

func (l *line) width() int {
	return len(l.buffer)
}

func (l *line) copy() *line {
	return &line{buffer: slices.Clone(l.buffer)}
}

func (l *line) insert(r rune, at int) {
	l.buffer = slices.Insert(l.buffer, at, newCharacter(r))
}

func (l *line) deleteChar(at int) {
	if l.width()-1 < at {
		return
	}

	l.buffer = slices.Delete(l.buffer, at, at+1)
}

type cursorpos struct {
	x, y int // left, top is (0, 0)

	hist []*cursorpos // tail is the latest
}

func (c *cursorpos) save() {
	c.hist = append(c.hist, &cursorpos{x: c.x, y: c.y})
}

// restore the cursor from the latest hist
func (c *cursorpos) restore() {
	c.x = c.hist[len(c.hist)-1].x
	c.y = c.hist[len(c.hist)-1].y
}

type screen struct {
	maxRows int
	maxCols int
	mode    mode
	cursor  cursorpos
	lines   []*line
	file    *os.File
	// the index of lines which is shown on the top of current screen.
	topline int
}

func (s *screen) currentLineIndex() int {
	return s.cursor.y + s.topline
}

func (s *screen) syncAll() {
	s.syncLinesAfter(0)
	s.syncCursor()
}

func (s *screen) syncCurrentLine() {
	s.cursor.save()
	movecursor(0, s.cursor.y)
	clearline()
	fmt.Fprint(os.Stdout, fmt.Sprintf("%s", s.lines[s.currentLineIndex()].String()))
	s.cursor.restore()
	s.syncCursor()
}

func (s *screen) syncLinesAfter(row int) {
	s.cursor.save()
	for i := row; i < min(len(s.lines), s.maxRows); i++ {
		s.cursor.y = i
		s.moveCursorToLineHead()
		s.syncCursor()
		clearline()
		fmt.Fprint(os.Stdout, fmt.Sprintf("%s", s.lines[s.currentLineIndex()].String()))
	}
	s.cursor.restore()
	s.syncCursor()
}

func (s *screen) syncCursor() {
	line := s.lines[s.currentLineIndex()]
	x := 0
	for i := range s.cursor.x {
		x += line.buffer[i].dispWidth
	}
	movecursor(x, s.cursor.y)
}

type direction int

const (
	up direction = iota + 1
	down
	left
	right
)

func (s *screen) moveCursor(direction direction) bool {
	current := s.cursor

	switch direction {
	case up:
		// when cursor is on the top and the first line is shown at the top,
		// do nothing.
		if current.y == 0 && s.topline == 0 {
			return false
		}

		if current.y == 0 {
			// when cursor is on the top but still upper line exists,
			// scroll 1 line up but the cursor itself does not move.
			s.topline--
			s.syncLinesAfter(0)
		} else {
			// else, move the cursor itself
			s.cursor.y = current.y - 1 // move up
		}

		// if upper line is shorter, the x should be the end of the upper line
		curline := s.lines[s.currentLineIndex()]
		if curline.width() < current.x {
			s.cursor.x = curline.width()
		}

		return true

	case down:
		if s.currentLineIndex() == len(s.lines)-1 {
			return false
		}

		if s.cursor.y == s.maxRows-1 && s.currentLineIndex() < len(s.lines)-1 {
			s.topline++
			s.syncLinesAfter(0)
		} else {
			s.cursor.y = current.y + 1 // move down
		}

		// if next line is shorter, the x should be the end of the next line
		curline := s.lines[s.currentLineIndex()]
		if curline.width() < current.x {
			s.cursor.x = curline.width()
		}

		return true

	case left:
		if current.x == 0 {
			return false
		}

		s.cursor.x -= 1

		return true

	case right:
		if current.x == s.lines[s.currentLineIndex()].width() {
			return false
		}

		s.cursor.x += 1

		return true

	default:
		panic("invalid direction is passed")
	}
}

func (s *screen) addline(direction direction) {
	switch direction {
	case up:
		s.lines = slices.Insert(s.lines, s.currentLineIndex(), newline())
	case down:
		s.lines = slices.Insert(s.lines, s.currentLineIndex()+1, newline())
	default:
		panic("invalid direction is passed to addline")
	}
}

func (s *screen) deleteCurrentChar() {
	line := s.lines[s.currentLineIndex()]
	line.deleteChar(s.cursor.x)
}

func (s *screen) moveCursorToLineHead() bool {
	if s.cursor.x == 0 {
		return false
	}

	s.cursor.x = 0
	return true
}

func (s *screen) debug() {
	debug("cursor: {x: %v, y: %v}\n", s.cursor.x, s.cursor.y)
}

func main() {
	/*
	 * Prepare terminal
	 */

	var orig syscall.Termios
	if err := ioctl_getTermios(&orig); err != nil {
		panic(err)
	}

	// restore termios original setting at exit
	defer func() {
		if err := ioctl_setTermios(&orig); err != nil {
			panic(err)
		}
	}()

	conf := orig

	// see https://github.com/antirez/kilo/blob/master/kilo.c#L226-L239
	conf.Iflag &= ^(uint32(syscall.BRKINT) | uint32(syscall.ICRNL) | uint32(syscall.INPCK) | uint32(syscall.ISTRIP) | uint32(syscall.IXON))
	conf.Oflag &= ^(uint32(syscall.OPOST))
	conf.Cflag |= (uint32(syscall.CS8))
	conf.Lflag &= ^(uint32(syscall.ECHO) | uint32(syscall.ICANON) | uint32(syscall.IEXTEN) | uint32(syscall.ISIG))
	conf.Cc[syscall.VMIN] = 0
	conf.Cc[syscall.VTIME] = 1
	if err := ioctl_setTermios(&conf); err != nil {
		panic(err)
	}

	/*
	 * Prepare editor state
	 */

	row, col, _, _, err := ioctl_getWindowSize()
	if err != nil {
		panic(err)
	}

	s := &screen{
		maxRows: row,
		maxCols: col,
		mode:    normal, // normal mode on startup
	}

	refresh()

	cursorx, cursory := resetcursor()
	s.cursor.x = cursorx
	s.cursor.y = cursory

	/*
	 * file handling
	 */

	defer refresh()

	flag.Parse()
	args := flag.Args()
	switch len(args) {
	case 0:
		s.lines = []*line{newline()} // initialize first line

	case 1:
		filename := args[0]
		f, err := os.OpenFile(filename, os.O_RDWR|os.O_CREATE, 0644)
		if err != nil {
			panic(err)
		}

		s.file = f

		defer s.file.Close()

		s.lines = []*line{newline()} // initialize first line
		currentline := 0

		reader := bufio.NewReader(s.file)
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
				s.lines = append(s.lines, newline())
				currentline++

			default:
				s.lines[currentline].buffer = append(s.lines[currentline].buffer, newCharacter(r))
			}
		}

		s.syncAll()

	default:
		panic("more than 2 args are passed")
	}

	/*
	 * start editor main routine
	 */

	reader := bufio.NewReader(os.Stdin)
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
		var (
			cursormoved bool
		)

		switch s.mode {
		case normal:
			switch {
			case r == ctrl('q'):
				goto finish

			case r == 'i':
				s.mode = insert

			case r == 'd':
				s.deleteCurrentChar()
				s.syncCurrentLine()

			case r == 'o':
				s.addline(down)
				s.moveCursor(down)
				s.moveCursorToLineHead()
				s.syncLinesAfter(s.cursor.y)
				cursormoved = true
				s.mode = insert

			case r == 'O':
				s.addline(up)
				s.moveCursor(up)
				s.moveCursorToLineHead()
				s.syncLinesAfter(s.cursor.y)
				cursormoved = true
				s.mode = insert

			case r == 'h', isArrowKey && dir == left:
				cursormoved = s.moveCursor(left)

			case r == 'j', isArrowKey && dir == down:
				cursormoved = s.moveCursor(down)

			case r == 'k', isArrowKey && dir == up:
				cursormoved = s.moveCursor(up)

			case r == 'l', isArrowKey && dir == right:
				cursormoved = s.moveCursor(right)
			}

		case insert:
			switch {
			case r == 27: // Esc
				s.mode = normal

			case isArrowKey && dir == left:
				cursormoved = s.moveCursor(left)

			case isArrowKey && dir == down:
				cursormoved = s.moveCursor(down)

			case isArrowKey && dir == up:
				cursormoved = s.moveCursor(up)

			case isArrowKey && dir == right:
				cursormoved = s.moveCursor(right)

			case unicode.IsControl(r):
				debug("control key is pressed: %v\n", r)

			default:
				// edit the line on memory
				line := s.lines[s.currentLineIndex()].copy()
				line.insert(r, s.cursor.x)
				s.lines[s.currentLineIndex()] = line

				debug("%v, %v, %v, %v\n", len(s.lines), s.lines, s.cursor.x, s.cursor.y)

				// save current cursor
				curcursor := s.cursor
				curcursor.x += 1

				// render
				clearline()
				s.moveCursorToLineHead()
				s.syncCursor() // this must be needed as moveCursorToLineHead changes the cursor position only logically.
				fmt.Fprint(os.Stdout, fmt.Sprintf("%s", line.String()))

				// restore cursor position
				s.cursor.x = curcursor.x
				s.cursor.y = curcursor.y
				cursormoved = true
			}

		default:
			panic("unknown mode")
		}

		if cursormoved {
			s.syncCursor()
		}

		s.debug()

	}

finish:

	refresh()
	fmt.Fprintf(os.Stdout, "\n")
	resetcursor()
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

func refresh() {
	fmt.Fprint(os.Stdout, "\x1b[2J")
}

func clearline() {
	fmt.Fprint(os.Stdout, "\x1b[K")
}

func resetcursor() (int, int) {
	return movecursor(0, 0)
}

func movecursor(x, y int) (int, int) {
	fmt.Fprint(os.Stdout, fmt.Sprintf("\x1b[%v;%vH", y+1, x+1))
	return x, y
}

func ctrl(input byte) rune {
	return rune(input & 0x1f)
}

/*
 * ioctl stuff
 */

func ioctl_getWindowSize() (int, int, int, int, error) {
	type Winsize struct {
		Row    uint16
		Col    uint16
		Xpixel uint16
		Ypixel uint16
	}
	var w Winsize
	err := ioctl(syscall.Stdout, syscall.TIOCGWINSZ, unsafe.Pointer(&w))
	if err != nil {
		return 0, 0, 0, 0, err
	}

	return int(w.Row), int(w.Col), int(w.Xpixel), int(w.Ypixel), nil
}

func ioctl_getTermios(termios *syscall.Termios) error {
	if err := ioctl(syscall.Stdin, syscall.TCGETS, unsafe.Pointer(termios)); err != nil {
		return err
	}
	return nil
}

func ioctl_setTermios(termios *syscall.Termios) error {
	var TCSETSF uint = 0x5404 // not sure why but not defined in syscall package

	if err := ioctl(syscall.Stdin, TCSETSF, unsafe.Pointer(termios)); err != nil {
		return err
	}
	return nil
}

func ioctl(fd int, op uint, arg unsafe.Pointer) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(op), uintptr(arg))
	if errno != 0 {
		var err error
		// https://www.man7.org/linux/man-pages/man2/ioctl.2.html#ERRORS
		switch errno {
		case syscall.EBADF:
			err = fmt.Errorf("fd is not a valid")
		case syscall.EFAULT:
			err = fmt.Errorf("arg fault")
		case syscall.EINVAL:
			err = fmt.Errorf("invalid op or arg")
		case syscall.ENOTTY:
			err = fmt.Errorf("fd is not a character special dev")
		}
		return err
	}
	return nil
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
