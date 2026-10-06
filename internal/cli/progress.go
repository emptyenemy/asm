package cli

import (
	"fmt"
	"math"
	"strings"
	"sync/atomic"
	"time"
)

type transfer struct {
	received, total atomic.Int64
	base            atomic.Int64 // bytes already on disk when the download began
	started         time.Time
}

func (p *transfer) Write(data []byte) (int, error) {
	p.received.Add(int64(len(data)))
	return len(data), nil
}

func formatBytes(bytes float64) string {
	units := []string{"B", "KB", "MB", "GB"}
	unit := 0
	for bytes >= 1000 && unit < 3 {
		bytes /= 1000
		unit++
	}
	bytes = math.Round(bytes*100) / 100
	if bytes == math.Trunc(bytes) {
		return fmt.Sprintf("%.0f %s", bytes, units[unit])
	}
	return fmt.Sprintf("%.2f %s", bytes, units[unit])
}

func (t *terminal) activity(label string, progress *transfer) func() {
	if !t.interactive {
		t.print(t.err, label+"...")
		return func() {}
	}
	t.open()
	done, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		started, frame, oldWidth := time.Now(), 0, 0
		draw := func() {
			width := max(1, t.width()-1)
			text := fmt.Sprintf("  %c  %s  %ds", "|/-\\"[frame%4], label, int(time.Since(started).Seconds()))
			frame++
			if progress != nil {
				received, total := progress.received.Load(), progress.total.Load()
				stats := formatBytes(float64(received))
				if total > 0 {
					stats += " / " + formatBytes(float64(total))
				}
				seconds := time.Since(progress.started).Seconds()
				if seconds > 0 {
					stats += "  " + formatBytes(float64(max(0, received-progress.base.Load()))/seconds) + "/s"
				}
				percent := "  --"
				if total > 0 {
					percent = fmt.Sprintf("%3d%%", min(100, int(float64(received)*100/float64(total))))
				}
				if width >= 59 {
					barWidth := max(8, min(28, width-len(stats)-13))
					bar := ""
					if total > 0 {
						filled := min(barWidth, int(float64(received)/float64(total)*float64(barWidth)))
						bar = strings.Repeat("=", filled)
						if filled < barWidth {
							bar += ">"
							filled++
						}
						bar += strings.Repeat(".", barWidth-filled)
					} else {
						position := frame % barWidth
						bar = strings.Repeat(".", position) + ">" + strings.Repeat(".", barWidth-position-1)
					}
					text = "  [" + bar + "] " + percent + "  " + stats
				} else {
					text = fmt.Sprintf("  %c  %s  %s", "|/-\\"[frame%4], percent, formatBytes(float64(received)))
				}
			}
			runes := []rune(text)
			if len(runes) > width {
				runes = runes[:width]
			}
			text = string(runes)
			padding := strings.Repeat(" ", max(0, min(oldWidth, width)-len(runes)))
			fmt.Fprint(t.err, "\r"+t.style(text, "accent")+padding)
			oldWidth = len(runes)
		}
		draw()
		for {
			select {
			case <-ticker.C:
				draw()
			case <-done:
				fmt.Fprint(t.err, "\r"+strings.Repeat(" ", min(oldWidth, max(1, t.width()-1)))+"\r")
				return
			}
		}
	}()
	return func() { close(done); <-stopped }
}
