package tui

import (
	"encoding/base64"
	"fmt"
	"io"
)

type Clipboard interface {
	Copy(text string) error
}

type OSC52Clipboard struct {
	output io.Writer
}

func NewOSC52Clipboard(output io.Writer) OSC52Clipboard {
	return OSC52Clipboard{output: output}
}

func (clipboard OSC52Clipboard) Copy(text string) error {
	if clipboard.output == nil {
		return nil
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(text))
	_, err := fmt.Fprintf(clipboard.output, "\x1b]52;c;%s\a", encoded)
	return err
}
