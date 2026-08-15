package terminal

// TextExtractor removes terminal control sequences while preserving visible text
// for the Connector event stream. It retains escape state across read chunks.
type TextExtractor struct {
	state      escapeState
	lastCR     bool
	csi        []byte
	screenMode bool
}

type escapeState uint8

const (
	escapeNormal escapeState = iota
	escapeEscape
	escapeCSI
	escapeOSC
	escapeOSCEscape
)

func (e *TextExtractor) Write(input []byte) string {
	output := make([]byte, 0, len(input))
	for _, value := range input {
		switch e.state {
		case escapeEscape:
			switch value {
			case '[':
				e.state = escapeCSI
				e.csi = e.csi[:0]
			case ']':
				e.state = escapeOSC
			case 0x1b:
				e.state = escapeEscape
			default:
				e.state = escapeNormal
			}
			continue
		case escapeCSI:
			if len(e.csi) < 64 {
				e.csi = append(e.csi, value)
			}
			if value >= 0x40 && value <= 0x7e {
				e.recordCSI(value)
				e.state = escapeNormal
			}
			continue
		case escapeOSC:
			if value == 0x07 {
				e.state = escapeNormal
			} else if value == 0x1b {
				e.state = escapeOSCEscape
			}
			continue
		case escapeOSCEscape:
			if value == '\\' || value == 0x07 {
				e.state = escapeNormal
			} else if value != 0x1b {
				e.state = escapeOSC
			}
			continue
		}

		if value == 0x1b {
			e.state = escapeEscape
			e.lastCR = false
			continue
		}
		if e.lastCR {
			if value == '\n' {
				output = append(output, '\n')
				e.lastCR = false
				continue
			}
			// A bare carriage return is normally a terminal redraw. Do not
			// turn it into a cloud event boundary.
			e.lastCR = false
		}
		switch value {
		case '\r':
			e.lastCR = true
		case '\n':
			output = append(output, value)
			e.lastCR = false
		case '\t':
			output = append(output, ' ')
			e.lastCR = false
		default:
			if value >= 0x20 && value != 0x7f {
				output = append(output, value)
			}
			e.lastCR = false
		}
	}
	return string(output)
}

// ScreenMode reports whether the stream has entered a cursor-addressed screen.
// Full-screen TUIs use this mode, so callers can avoid uploading each redraw.
func (e *TextExtractor) ScreenMode() bool { return e.screenMode }

func (e *TextExtractor) recordCSI(final byte) {
	parameters := string(e.csi[:len(e.csi)-1])
	switch final {
	case 'H', 'f', 'J':
		e.screenMode = true
	case 'h':
		if parameters == "?47" || parameters == "?1047" || parameters == "?1049" {
			e.screenMode = true
		}
	case 'l':
		if parameters == "?47" || parameters == "?1047" || parameters == "?1049" {
			e.screenMode = false
		}
	}
}
