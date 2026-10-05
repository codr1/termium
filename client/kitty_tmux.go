package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const (
	tmuxPassthroughPrefix = "\033Ptmux;"
	tmuxPassthroughSuffix = "\033\\"
	kittyPlaceholder      = '\U0010EEEE'
)

type kittySessionState struct {
	tmux             bool
	imageID          uint32
	placeholderWidth int
	placeholderRows  int
}

var kittySession = kittySessionState{imageID: 1}

func setupKittySession() error {
	kittySession = kittySessionState{imageID: 1}
	if os.Getenv("TMUX") == "" {
		return nil
	}
	if err := enableTmuxPassthrough(); err != nil {
		return err
	}
	id, err := randomKittyImageID(rand.Reader)
	if err != nil {
		return fmt.Errorf("create Kitty image ID: %w", err)
	}
	kittySession.tmux, kittySession.imageID = true, id
	return nil
}

func tmuxCommand(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "tmux", args...)
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return "", fmt.Errorf("tmux command timed out")
	}
	if err != nil {
		return "", fmt.Errorf("tmux %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

func enableTmuxPassthrough() error {
	value, err := tmuxCommand("show", "-Ap", "allow-passthrough")
	if err != nil {
		return fmt.Errorf("Kitty graphics in tmux require tmux 3.3 or newer: %w", err)
	}
	fields := strings.Fields(value)
	if len(fields) > 0 && (fields[len(fields)-1] == "on" || fields[len(fields)-1] == "all") {
		return nil
	}
	if _, err := tmuxCommand("set", "-p", "allow-passthrough", "on"); err != nil {
		return fmt.Errorf("enable Kitty graphics passthrough for this tmux pane: %w", err)
	}
	return nil
}

func tmuxCellSize() (CharSize, bool) {
	if os.Getenv("TMUX") == "" {
		return CharSize{}, false
	}
	value, err := tmuxCommand("display-message", "-p", "#{window_cell_width} #{window_cell_height}")
	if err != nil {
		return CharSize{}, false
	}
	fields := strings.Fields(value)
	if len(fields) != 2 {
		return CharSize{}, false
	}
	width, widthErr := strconv.Atoi(fields[0])
	height, heightErr := strconv.Atoi(fields[1])
	if widthErr != nil || heightErr != nil || width < 1 || height < 1 {
		return CharSize{}, false
	}
	return CharSize{Width: width, Height: height}, true
}

func randomKittyImageID(source io.Reader) (uint32, error) {
	var raw [4]byte
	for {
		if _, err := io.ReadFull(source, raw[:3]); err != nil {
			return 0, err
		}
		id := uint32(raw[0])<<16 | uint32(raw[1])<<8 | uint32(raw[2])
		if id != 0 {
			return id, nil
		}
	}
}

func wrapTmuxPassthrough(data []byte) []byte {
	output := make([]byte, 0, len(data)+len(tmuxPassthroughPrefix)+len(tmuxPassthroughSuffix)+2)
	output = append(output, tmuxPassthroughPrefix...)
	for _, b := range data {
		output = append(output, b)
		if b == '\033' {
			output = append(output, b)
		}
	}
	return append(output, tmuxPassthroughSuffix...)
}

func kittyDeleteSequence() []byte {
	id := kittySession.imageID
	if id == 0 {
		id = 1
	}
	command := []byte(fmt.Sprintf("\033_Ga=d,d=I,i=%d,q=2\033\\", id))
	if kittySession.tmux {
		return wrapTmuxPassthrough(command)
	}
	return command
}

func writeKittyFrame(w io.Writer, data []byte, row, column, width, height int) error {
	if !kittySession.tmux {
		return writeGraphicsFrame(w, data, row, column)
	}
	columns, rows := kittyCells(width, height, charSize)
	if rows > len(kittyRowDiacritics) {
		return fmt.Errorf("Kitty tmux image is %d rows tall; reduce the pane below %d rows", rows, len(kittyRowDiacritics)+1)
	}
	write := func(data []byte) error {
		n, err := w.Write(data)
		if err == nil && n != len(data) {
			err = io.ErrShortWrite
		}
		return err
	}
	if err := write([]byte("\033[s")); err != nil {
		return err
	}
	var firstErr error
	if err := write(data); err != nil {
		firstErr = err
	}
	if firstErr == nil && (kittySession.placeholderWidth != columns || kittySession.placeholderRows != rows) {
		id := kittySession.imageID
		color := []byte(fmt.Sprintf("\033[38;2;%d;%d;%dm", id>>16, id>>8&255, id&255))
		for y := 0; y < rows; y++ {
			line := strings.Builder{}
			line.Grow(columns*4 + 32)
			fmt.Fprintf(&line, "\033[%d;%dH%s%c%c", row+y, column, color, kittyPlaceholder, kittyRowDiacritics[y])
			for x := 1; x < columns; x++ {
				line.WriteRune(kittyPlaceholder)
			}
			if err := write([]byte(line.String())); err != nil {
				firstErr = err
				break
			}
		}
		if firstErr == nil {
			kittySession.placeholderWidth, kittySession.placeholderRows = columns, rows
		}
	}
	if err := write([]byte("\033[39m\033[u")); firstErr == nil {
		firstErr = err
	}
	return firstErr
}

func kittyCells(width, height int, cell CharSize) (int, int) {
	if cell.Width < 1 {
		cell.Width = 8
	}
	if cell.Height < 1 {
		cell.Height = 16
	}
	return max(1, (width+cell.Width-1)/cell.Width), max(1, (height+cell.Height-1)/cell.Height)
}

// Normative Kitty graphics protocol row/column diacritics, derived from
// Unicode 6.0. Only the row mark is needed because columns inherit left-to-right.
var kittyRowDiacritics = [...]rune{
	0x0305, 0x030D, 0x030E, 0x0310, 0x0312, 0x033D, 0x033E, 0x033F, 0x0346, 0x034A, 0x034B, 0x034C,
	0x0350, 0x0351, 0x0352, 0x0357, 0x035B, 0x0363, 0x0364, 0x0365, 0x0366, 0x0367, 0x0368, 0x0369,
	0x036A, 0x036B, 0x036C, 0x036D, 0x036E, 0x036F, 0x0483, 0x0484, 0x0485, 0x0486, 0x0487, 0x0592,
	0x0593, 0x0594, 0x0595, 0x0597, 0x0598, 0x0599, 0x059C, 0x059D, 0x059E, 0x059F, 0x05A0, 0x05A1,
	0x05A8, 0x05A9, 0x05AB, 0x05AC, 0x05AF, 0x05C4, 0x0610, 0x0611, 0x0612, 0x0613, 0x0614, 0x0615,
	0x0616, 0x0617, 0x0657, 0x0658, 0x0659, 0x065A, 0x065B, 0x065D, 0x065E, 0x06D6, 0x06D7, 0x06D8,
	0x06D9, 0x06DA, 0x06DB, 0x06DC, 0x06DF, 0x06E0, 0x06E1, 0x06E2, 0x06E4, 0x06E7, 0x06E8, 0x06EB,
	0x06EC, 0x0730, 0x0732, 0x0733, 0x0735, 0x0736, 0x073A, 0x073D, 0x073F, 0x0740, 0x0741, 0x0743,
	0x0745, 0x0747, 0x0749, 0x074A, 0x07EB, 0x07EC, 0x07ED, 0x07EE, 0x07EF, 0x07F0, 0x07F1, 0x07F3,
	0x0816, 0x0817, 0x0818, 0x0819, 0x081B, 0x081C, 0x081D, 0x081E, 0x081F, 0x0820, 0x0821, 0x0822,
	0x0823, 0x0825, 0x0826, 0x0827, 0x0829, 0x082A, 0x082B, 0x082C, 0x082D, 0x0951, 0x0953, 0x0954,
	0x0F82, 0x0F83, 0x0F86, 0x0F87, 0x135D, 0x135E, 0x135F, 0x17DD, 0x193A, 0x1A17, 0x1A75, 0x1A76,
	0x1A77, 0x1A78, 0x1A79, 0x1A7A, 0x1A7B, 0x1A7C, 0x1B6B, 0x1B6D, 0x1B6E, 0x1B6F, 0x1B70, 0x1B71,
	0x1B72, 0x1B73, 0x1CD0, 0x1CD1, 0x1CD2, 0x1CDA, 0x1CDB, 0x1CE0, 0x1DC0, 0x1DC1, 0x1DC3, 0x1DC4,
	0x1DC5, 0x1DC6, 0x1DC7, 0x1DC8, 0x1DC9, 0x1DCB, 0x1DCC, 0x1DD1, 0x1DD2, 0x1DD3, 0x1DD4, 0x1DD5,
	0x1DD6, 0x1DD7, 0x1DD8, 0x1DD9, 0x1DDA, 0x1DDB, 0x1DDC, 0x1DDD, 0x1DDE, 0x1DDF, 0x1DE0, 0x1DE1,
	0x1DE2, 0x1DE3, 0x1DE4, 0x1DE5, 0x1DE6, 0x1DFE, 0x20D0, 0x20D1, 0x20D4, 0x20D5, 0x20D6, 0x20D7,
	0x20DB, 0x20DC, 0x20E1, 0x20E7, 0x20E9, 0x20F0, 0x2CEF, 0x2CF0, 0x2CF1, 0x2DE0, 0x2DE1, 0x2DE2,
	0x2DE3, 0x2DE4, 0x2DE5, 0x2DE6, 0x2DE7, 0x2DE8, 0x2DE9, 0x2DEA, 0x2DEB, 0x2DEC, 0x2DED, 0x2DEE,
	0x2DEF, 0x2DF0, 0x2DF1, 0x2DF2, 0x2DF3, 0x2DF4, 0x2DF5, 0x2DF6, 0x2DF7, 0x2DF8, 0x2DF9, 0x2DFA,
	0x2DFB, 0x2DFC, 0x2DFD, 0x2DFE, 0x2DFF, 0xA66F, 0xA67C, 0xA67D, 0xA6F0, 0xA6F1, 0xA8E0, 0xA8E1,
	0xA8E2, 0xA8E3, 0xA8E4, 0xA8E5, 0xA8E6, 0xA8E7, 0xA8E8, 0xA8E9, 0xA8EA, 0xA8EB, 0xA8EC, 0xA8ED,
	0xA8EE, 0xA8EF, 0xA8F0, 0xA8F1, 0xAAB0, 0xAAB2, 0xAAB3, 0xAAB7, 0xAAB8, 0xAABE, 0xAABF, 0xAAC1,
	0xFE20, 0xFE21, 0xFE22, 0xFE23, 0xFE24, 0xFE25, 0xFE26, 0x10A0F, 0x10A38, 0x1D185, 0x1D186, 0x1D187,
	0x1D188, 0x1D189, 0x1D1AA, 0x1D1AB, 0x1D1AC, 0x1D1AD, 0x1D242, 0x1D243, 0x1D244,
}
