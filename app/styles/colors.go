package styles

import (
	"fmt"
	"math"
	"math/rand"
	"time"

	"github.com/charmbracelet/lipgloss"
)

const (
	ColorLogo              = lipgloss.Color("93")
	ColorBorder            = lipgloss.Color("0")
	ColorBorderActive      = ColorLogo // tight to the logo color
	ColorStatusBarLogCount = lipgloss.Color("93")
	ColorStatusBarBeamInfo = lipgloss.Color("22")
	ColorErrorBackground   = lipgloss.Color("160")
)

func RandColor() (lipgloss.Color, lipgloss.Color) {
	// including extended ANSI Grayscale color reach from 0-255
	// see https://github.com/muesli/termenv which is used for lipgloss

	color := lipgloss.Color(fmt.Sprint(rand.Intn(256)))

	r, g, b, _ := color.RGBA()

	var foreground lipgloss.Color = lipgloss.Color("#000000")

	delta := readableForeground(r, g, b)
	if delta == 1 {
		foreground = lipgloss.Color("#ffffff")
	}
	return color, foreground
}

func readableForeground(r, g, b uint32) int {

	r = uint32(math.Pow(float64(r/255), 2.2))
	g = uint32(math.Pow(float64(g/255), 2.2))
	b = uint32(math.Pow(float64(b/255), 2.2))

	delta := float64(0.2126)*float64(r) + float64(0.07151)*float64(g) + float64(0.0721)*float64(b)

	return int(math.Ceil(delta))
}

func init() {
	rand.Seed(time.Now().Unix())
}
