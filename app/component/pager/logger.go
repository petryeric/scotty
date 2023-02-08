package pager

import (
	"bytes"
	"strconv"
	"strings"

	"github.com/KonstantinGasser/scotty/app/styles"
	"github.com/KonstantinGasser/scotty/debug"
	plexer "github.com/KonstantinGasser/scotty/multiplexer"
	"github.com/KonstantinGasser/scotty/ring"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	bottomSectionHeight = 1
	inputSectionHeight  = 2

	// wow literally no idea why this number hence
	// the variable name - if you get why tell me and
	// pls open a PR..else pls don't change it
	magicNumber = 2

	awaitInput = iota + 1
	hasInput
)

var (
	pagerStyle = lipgloss.NewStyle()

	inputStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(styles.DefaultColor.Border)
)

type subscriber struct {
	label string
	color lipgloss.Color
}

// Logger implements the tea.Model interface.
// Furthermore, Logger allows to tail logs.
// Logger does not not store the logs its only
// porose is it to display them.
type Logger struct {
	buffer ring.Buffer
	writer bytes.Buffer

	beams map[string]lipgloss.Color
	// stores the length of the longest stream
	// label in order to align the start of the logs
	maxLabelLength int
	// underlying model which handles
	// scrolling and rendering of the logs
	view viewport.Model

	// available tty width and height
	// updates if changes
	width, height int

	// relativeIndex represents the requested index
	// on the current page. Whichever item == index
	// is highlighted and formatted. Note relativeIndex
	// is the relative index to the page. In order
	// to get the index of the element within the
	// buffer one must add the offsetStart to the
	// relativeIndex to get the absolute index.
	relativeIndex int
	// absoluteIndex refers to the actual index in the
	// buffer which is currently formatted
	absoluteIndex int
	// offsetStart if used when paging through the logs
	// and formatting log lines. It refers to the index
	// with which the pager starts (first log of the page)
	offsetStart int
	// pageSize refers to the number of items currently
	// visible in the view - line wraps are not included
	// an item which takes up two lines counts as one
	pageSize int
	// awaitInput indicated if ECS is pressed.
	// if awaitInput == false the input for commands
	// is focused else moved out of focus
	awaitInput bool
	// input is the input field to select
	// an index to format and input further
	// commands
	input textinput.Model

	footer tea.Model
}

func NewLogger(width, height int) *Logger {

	w, h := width, height-bottomSectionHeight-inputSectionHeight-magicNumber // -1 to margin top for testing

	view := viewport.New(w, h)
	view.Height = h
	view.MouseWheelEnabled = true

	input := textinput.New()
	input.Placeholder = "line number (use k/j to move and ESC to exit)"
	input.Prompt = ":"

	return &Logger{
		buffer: ring.New(uint32(12)),
		writer: bytes.Buffer{},

		beams:          map[string]lipgloss.Color{},
		maxLabelLength: 0,
		view:           view,
		width:          w,
		height:         h,
		awaitInput:     false,
		relativeIndex:  -1,
		footer:         newFooter(w, h),
		input:          input,
	}
}

func (pager *Logger) Init() tea.Cmd {
	return nil
}

func (pager *Logger) Update(msg tea.Msg) (tea.Model, tea.Cmd) {

	var (
		cmds []tea.Cmd
		cmd  tea.Cmd
	)

	switch msg := msg.(type) {
	case tea.MouseMsg:
		pager.view, cmd = pager.view.Update(msg)
		cmds = append(cmds, cmd)
	case tea.KeyMsg:
		switch msg.String() {
		// triggers the parsing mode of logs. Has no
		// effect while in parsing mode (awaitInput == true)
		case ":":
			if pager.awaitInput {
				break
			}
			pager.awaitInput = true

			// we need to kick of and continue to render
			// incoming logs. If we don't kick of the
			// rerendering the current logs are not wrapped
			// by the new width only once a new log is received
			pager.renderWindow(
				pager.relativeIndex,
				true,
				ring.WithLineWrap(pager.width),
			)

			pager.input.Focus()

		case "enter":
			if !pager.awaitInput {
				break
			}

			value := pager.input.Value()
			index, err := strconv.Atoi(value)
			if err != nil {
				debug.Print("input %q is not numeric. Type the index of the line you want to parse", value)
				break
			}

			pager.offsetStart = index
			pager.absoluteIndex = pager.offsetStart
			pager.relativeIndex = 0

			pager.pageSize = pager.renderOffset(
				pager.offsetStart,
				ring.WithInlineFormatting(pager.width, pager.absoluteIndex),
				ring.WithLineWrap(pager.width),
			)

		// exits the parsing mode. Has no effect
		// while not in parsing mode (awaitInput == false)
		case "esc":
			if !pager.awaitInput {
				break
			}

			width, height, err := styles.WindowSize()
			if err != nil {
				debug.Print("[tea.KeyMsg(esc)] unable to get tty width and height: %w\n", err)
			}

			pager.setDimensions(
				width,
				height-bottomSectionHeight-magicNumber,
			)

			pager.awaitInput = false
			pager.offsetStart = -1
			pager.relativeIndex = -1
			pager.absoluteIndex = -1

			pager.input.Reset()
			pager.input.Blur()
			// again the width of the log view changes on
			// exit as such we need to force a rerender
			// in order to fix the line wraps of each log
			pager.renderWindow(
				pager.height,
				true,
			)

		// selects the previous log line to be parsed
		// and displayed. Input ignores when relativeIndex <= 0
		case "k":

			if pager.absoluteIndex == 0 {
				break
			}

			pager.relativeIndex--
			pager.absoluteIndex--

			// requested index to format is outside (above)
			// the current view as such we need to shift the
			// content of the view up. For scrolling up we
			// only show the next upper element not an entire
			// new page like we do when scrolling down.
			if pager.relativeIndex < 0 {
				pager.relativeIndex = 0
				pager.offsetStart--
				if pager.offsetStart < 0 {
					pager.offsetStart = 0
				}

				pager.pageSize = pager.renderOffset(
					pager.offsetStart,
					ring.WithInlineFormatting(pager.width, pager.absoluteIndex),
					ring.WithLineWrap(pager.width),
				)
				break
			}

			pager.pageSize = pager.renderOffset(
				pager.offsetStart,
				ring.WithInlineFormatting(pager.width, pager.absoluteIndex),
				ring.WithLineWrap(pager.width),
			)
			// for this key stroke we don't need the msg any other where
			// and we putted to the input model the stork is registered
			// which we don't want
			return pager, tea.Batch(cmds...)

		// selects the next log line to be parsed and
		// displayed. Input ignored when relativeIndex >= buffer.cap
		case "j":
			pager.relativeIndex++ // index of the within the current page
			pager.absoluteIndex++ // overall index of the selected item in the buffer

			// nil items in the buffer indicated that the buffer is not full
			// and the requested index exists but has not been written to yet.
			// Just means user wanted a log that has not been beamed yet.
			if pager.buffer.Nil(pager.absoluteIndex) {
				// well showing nothing is not cool
				// compensate to last working index
				pager.absoluteIndex--
				pager.relativeIndex--

				pager.pageSize = pager.renderOffset(
					pager.offsetStart,
					ring.WithInlineFormatting(pager.width, pager.absoluteIndex),
					ring.WithLineWrap(pager.width),
				)

				break
			}

			// check if the requested log line is out of
			// the view (not included in the previous render)
			// if so we need to adjust the page/go to the next
			// page an rerender the view again
			if pager.relativeIndex >= pager.pageSize {
				pager.offsetStart += pager.relativeIndex
				// reset relativeIndex since its relative to
				// the current page. When the page changes
				// the relative index is 0
				pager.relativeIndex = 0

				pager.pageSize = pager.renderOffset(
					pager.offsetStart,
					ring.WithInlineFormatting(pager.width, pager.absoluteIndex),
					ring.WithLineWrap(pager.width),
				)
				break
			}

			// render logs starting from the offset
			// till offset+height. The returned
			// lines indicate how many items are included
			// in the render (hard to tell solely based on the string
			// from the pager.writer)
			pager.pageSize = pager.renderOffset(
				pager.offsetStart,
				ring.WithInlineFormatting(pager.width, pager.absoluteIndex),
				ring.WithLineWrap(pager.width),
			)

			// for this key stroke we don't need the msg any other where
			// and we putted to the input model the stork is registered
			// which we don't want
			return pager, tea.Batch(cmds...)
		}

	// event dispatched from bubbletea when the screen size changes.
	// We need to update the pager and pager.view width and height.
	// However, if the parsing mode is on the width is only 2/3
	// of the available screen size.
	case tea.WindowSizeMsg:

		width := msg.Width
		height := msg.Height - bottomSectionHeight - inputSectionHeight - magicNumber

		pager.setDimensions(
			width,
			height,
		)

		if pager.awaitInput && pager.relativeIndex >= 0 {
			pager.pageSize = pager.renderOffset(
				pager.relativeIndex,
				ring.WithLineWrap(pager.width),
				ring.WithInlineFormatting(pager.width, pager.absoluteIndex),
			)
			break
		}

		pager.renderWindow(
			pager.height,
			true,
			ring.WithLineWrap(pager.width),
		)

	// event dispatched each time a beam disconnects from scotty.
	// The message itself is the label of the stream which
	// disconnected. On a disconnect we need to recompute the
	// length of the longest stream label in order to maintain
	// pretty indention for logging the logs with the label prefix
	case plexer.Unsubscribe:
		// we only need to reassign the max value
		// if the current max is disconnecting
		if len(msg) >= pager.maxLabelLength {
			max := 0
			for label := range pager.beams {
				if len(label) > max && label != string(msg) {
					max = len(label)
				}
			}
			pager.maxLabelLength = max
		}

	// event dispatched each time a new stream connects to
	// the multiplexer. on-event we need to update the footer
	// model with the new stream information as well as update
	// the loggers state. The logger keeps track of connected beams
	// however only cares about the color to use when rendering the logs.
	// Logger will ensure that the color for the printed logs of a stream
	// are matching the color information in the footer
	case plexer.Subscriber:

		// update max label length for indenting
		// while displaying logs
		if len(msg) > pager.maxLabelLength {
			pager.maxLabelLength = len(msg)
		}

		label := string(msg)

		if _, ok := pager.beams[label]; !ok {
			color, _ := styles.RandColor()
			pager.beams[label] = color

			pager.footer, _ = pager.footer.Update(subscriber{
				label: label,
				color: color,
			})
		}

		pager.footer, _ = pager.footer.Update(subscriber{
			label: label,
			color: pager.beams[label],
		})

		return pager, tea.Batch(cmds...)

	// event dispatched by the multiplexer each time a client/stream
	// sends a log linen.
	// The logger needs to add the ansi color code stored for the stream
	// to the dispatched message before adding the data to the ring buffer.
	// Once added to the ring buffer the logger queries for the latest N
	// records (where N is equal to the height of the current viewport.Model)
	// and pass the string to the viewport.Model for rendering
	case plexer.Message:
		color := pager.beams[msg.Label]

		space := pager.maxLabelLength - len(msg.Label)
		if space < 0 {
			space = 0
		}

		prefix := lipgloss.NewStyle().
			Foreground(color).
			Render(
				msg.Label+strings.Repeat(" ", space),
			) + " | "

		pager.buffer.Append(append([]byte(prefix), msg.Data...))

		// while browsing through the logs do don't want to
		// keep moving down the new logs
		if pager.awaitInput && pager.relativeIndex >= 0 {
			break
		}

		pager.renderWindow(
			pager.height,
			true,
			ring.WithLineWrap(pager.width),
		)
	}

	// propagate events to child models.
	// in certain cases there will be an early return
	// in any of the cases above either because the event
	// is not relevant for any downstream model or because
	// ??? there was some other reason...??
	pager.view, cmd = pager.view.Update(msg)
	cmds = append(cmds, cmd)

	pager.footer, cmd = pager.footer.Update(msg)
	cmds = append(cmds, cmd)

	pager.input, cmd = pager.input.Update(msg)
	cmds = append(cmds, cmd)

	return pager, tea.Batch(cmds...)
}

func (pager *Logger) View() string {
	return lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.JoinVertical(lipgloss.Left,
			pagerStyle.
				Padding(1).
				Render(
					pager.view.View(),
				),
			pager.footer.View(),
		),
		inputStyle.
			Render(
				pager.input.View(),
			),
	)
}

func (pager *Logger) renderWindow(rows int, toBottom bool, opts ...func(int, []byte) []byte) int {
	itemCount, err := pager.buffer.Window(
		&pager.writer,
		pager.height,
		opts...,
	)
	if err != nil {
		debug.Debug(err.Error())
	}

	pager.view.SetContent(pager.writer.String())
	pager.writer.Reset()

	if !toBottom {
		return itemCount
	}

	pager.view.GotoBottom()
	return itemCount
}

func (pager *Logger) renderOffset(offset int, opts ...func(int, []byte) []byte) int {
	lines, err := pager.buffer.Offset(
		&pager.writer,
		offset,
		pager.height,
		opts...,
	)
	if err != nil {
		debug.Debug(err.Error())
	}

	pager.view.SetContent(pager.writer.String())
	pager.writer.Reset()

	return lines
}

func (pager *Logger) setDimensions(width, height int) {
	pager.width, pager.height = width, height
	pager.view.Width, pager.view.Height = width, height
}
