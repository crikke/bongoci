package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

func main() {
	runServer()
}

// runServer runs the LSP server over stdin/stdout using the JSON-RPC 2.0
// framing defined by the Language Server Protocol (Content-Length headers).
func runServer() {
	handler := protocol.Handler{
		Initialize:            initialize,
		Initialized:           func(_ *glsp.Context, _ *protocol.InitializedParams) error { return nil },
		Shutdown:              func(_ *glsp.Context) error { return nil },
		Exit:                  func(_ *glsp.Context) error { os.Exit(0); return nil },
		TextDocumentDidOpen:   textDocumentDidOpen,
		TextDocumentDidChange: textDocumentDidChange,
		TextDocumentHover:     textDocumentHover,
	}

	log.SetOutput(os.Stderr)

	reader := bufio.NewReader(os.Stdin)
	writer := os.Stdout

	for {
		// Read headers
		headers := make(map[string]string)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if err == io.EOF {
					return
				}
				log.Printf("bongo-ls: read error: %v", err)
				return
			}
			line = strings.TrimRight(line, "\r\n")
			if line == "" {
				break
			}
			parts := strings.SplitN(line, ": ", 2)
			if len(parts) == 2 {
				headers[parts[0]] = parts[1]
			}
		}

		contentLengthStr, ok := headers["Content-Length"]
		if !ok {
			log.Printf("bongo-ls: missing Content-Length header")
			return
		}
		contentLength, err := strconv.Atoi(contentLengthStr)
		if err != nil {
			log.Printf("bongo-ls: bad Content-Length %q: %v", contentLengthStr, err)
			return
		}

		body := make([]byte, contentLength)
		if _, err := io.ReadFull(reader, body); err != nil {
			log.Printf("bongo-ls: body read error: %v", err)
			return
		}

		// Decode request
		var req struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id,omitempty"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			log.Printf("bongo-ls: json unmarshal error: %v", err)
			// Best-effort: try to extract an id from the raw body to send a proper error response.
			var idProbe struct{ ID json.RawMessage `json:"id"` }
			if json.Unmarshal(body, &idProbe) == nil && idProbe.ID != nil && string(idProbe.ID) != "null" {
				sendError(writer, idProbe.ID, -32700, "parse error")
			}
			continue
		}

		ctx := &glsp.Context{
			Method: req.Method,
			Params: req.Params,
			Notify: func(method string, params any) {
				sendNotification(writer, method, params)
			},
		}

		result, validMethod, validParams, handleErr := handler.Handle(ctx)

		// Only send a response if this was a request (has an ID)
		if req.ID != nil && string(req.ID) != "null" {
			switch {
			case handleErr != nil:
				sendError(writer, req.ID, -32603, handleErr.Error())
			case !validMethod:
				sendError(writer, req.ID, -32601, "method not found: "+req.Method)
			case !validParams:
				sendError(writer, req.ID, -32602, "invalid params")
			default:
				sendResult(writer, req.ID, result)
			}
		}
	}
}

func sendNotification(w io.Writer, method string, params any) {
	msg := struct {
		JSONRPC string `json:"jsonrpc"`
		Method  string `json:"method"`
		Params  any    `json:"params,omitempty"`
	}{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}
	writeJSON(w, msg)
}

func sendResult(w io.Writer, id json.RawMessage, result any) {
	msg := struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  any             `json:"result"`
	}{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
	writeJSON(w, msg)
}

func sendError(w io.Writer, id json.RawMessage, code int, message string) {
	msg := struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Error   struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}{
		JSONRPC: "2.0",
		ID:      id,
	}
	msg.Error.Code = code
	msg.Error.Message = message
	writeJSON(w, msg)
}

var writeMu sync.Mutex

func writeJSON(w io.Writer, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
	writeMu.Lock()
	defer writeMu.Unlock()
	fmt.Fprint(w, header)
	w.Write(data) //nolint:errcheck
}

func initialize(_ *glsp.Context, _ *protocol.InitializeParams) (any, error) {
	syncFull := protocol.TextDocumentSyncKindFull
	return protocol.InitializeResult{
		Capabilities: protocol.ServerCapabilities{
			TextDocumentSync: &protocol.TextDocumentSyncOptions{
				OpenClose: boolPtr(true),
				Change:    &syncFull,
			},
			HoverProvider: true,
		},
	}, nil
}

func boolPtr(b bool) *bool { return &b }

func textDocumentDidOpen(context *glsp.Context, params *protocol.DidOpenTextDocumentParams) error {
	docsMu.Lock()
	docs[params.TextDocument.URI] = params.TextDocument.Text
	docsMu.Unlock()
	publishDiagnostics(context, params.TextDocument.URI, params.TextDocument.Text)
	return nil
}

func textDocumentDidChange(context *glsp.Context, params *protocol.DidChangeTextDocumentParams) error {
	if len(params.ContentChanges) == 0 {
		return nil
	}
	// Full sync: last change contains complete document text.
	// ContentChanges elements are TextDocumentContentChangeEvent or
	// TextDocumentContentChangeEventWhole; both have a Text field.
	var text string
	last := params.ContentChanges[len(params.ContentChanges)-1]
	switch v := last.(type) {
	case protocol.TextDocumentContentChangeEvent:
		text = v.Text
	case protocol.TextDocumentContentChangeEventWhole:
		text = v.Text
	default:
		return nil
	}
	docsMu.Lock()
	docs[params.TextDocument.URI] = text
	docsMu.Unlock()
	publishDiagnostics(context, params.TextDocument.URI, text)
	return nil
}

func textDocumentHover(_ *glsp.Context, params *protocol.HoverParams) (*protocol.Hover, error) {
	docsMu.Lock()
	text, ok := docs[params.TextDocument.URI]
	docsMu.Unlock()
	if !ok {
		return nil, nil
	}
	word := wordAtPosition(text, int(params.Position.Line), int(params.Position.Character))
	desc, found := keywords[word]
	if !found {
		return nil, nil
	}
	return &protocol.Hover{
		Contents: protocol.MarkupContent{
			Kind:  protocol.MarkupKindMarkdown,
			Value: fmt.Sprintf("**%s**\n\n%s", word, desc),
		},
	}, nil
}
