package main

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"os/exec"
	"strconv"
	"strings"
)

type Completion struct {
	class,
	name,
	typ string
}

type CompletionResponse struct {
	partial     int
	completions []Completion
}

// readGocodeJson unmarshals the gocode json output into a CompletionResponse
func readGocodeJson(oj []interface{}) (*CompletionResponse, error) {
	var partial int
	if p, ok := oj[0].(float64); !ok {
		return nil, errors.New("bad response: unexpected partial")
	} else {
		partial = int(p)
	}
	var comps []interface{}
	if c, ok := oj[1].([]interface{}); !ok {
		return nil, errors.New("bad response: unexpected completion")
	} else {
		comps = c
	}
	completions := make([]Completion, len(comps))
	for i := range comps {
		comp := comps[i].(map[string]interface{})
		completions[i] = Completion{
			class: comp["class"].(string),
			name:  comp["name"].(string),
			typ:   comp["type"].(string),
		}
	}
	return &CompletionResponse{
		partial:     partial,
		completions: completions,
	}, nil
}

// gocode runs the github.com/nsf/gocode utility on a given input
func gocode(r io.Reader, pos int) (*CompletionResponse, error) {
	args := []string{
		"-f", "json", "autocomplete", strconv.Itoa(pos),
	}
	cmd := exec.Command("gocode", args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(stdin, r); err != nil {
		return nil, err
	}
	stdin.Close()

	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var oj []interface{}
	if err := json.Unmarshal(out, &oj); err != nil {
		return nil, err
	}
	if len(oj) > 0 {
		return readGocodeJson(oj)
	}
	return &CompletionResponse{}, nil
}

// handleCompleteRequest autocompletes code from a complete_request method,
// and sends the various reply messages.
func handleCompleteRequest(receipt msgReceipt) error {
	reqcontent := receipt.Msg.Content.(map[string]interface{})
	code, ok := reqcontent["code"].(string)
	if !ok {
		return errors.New("bad request: unexpected code")
	}
	cursorPos, ok := reqcontent["cursor_pos"].(float64)
	if !ok {
		return errors.New("bad request: unexpected cursor position")
	}

	// Tell the front-end that the kernel is working and when finished notify the
	// front-end that the kernel is idle again.
	if err := receipt.PublishKernelStatus(kernelBusy); err != nil {
		log.Printf("Error publishing kernel status 'busy': %v\n", err)
	}
	defer func() {
		if err := receipt.PublishKernelStatus(kernelIdle); err != nil {
			log.Printf("Error publishing kernel status 'idle': %v\n", err)
		}
	}()

	// Prepare the map that will hold the reply content.
	content := make(map[string]interface{})

	// Get completions
	in := strings.NewReader(code)
	if c, err := gocode(in, int(cursorPos)); err != nil {
		log.Fatal(err)
		content["status"] = "error"
	} else {
		matches := make([]string, len(c.completions))
		for i := range c.completions {
			matches[i] = c.completions[i].name
		}
		content["cursor_start"] = cursorPos
		content["cursor_end"] = cursorPos
		content["matches"] = matches
		content["status"] = "ok"
	}

	return receipt.Reply("complete_reply", content)
}
