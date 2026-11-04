package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// bridgeRequestTimeout bounds one request to the server, so a hung server cannot hold the client forever.
const bridgeRequestTimeout = 30 * time.Second

// readBufferBytes sizes the reader of the client's input. A longer line is read in pieces.
const readBufferBytes = 64 << 10

// bridgeLine is one line of the client's input, or the error that ended the input.
type bridgeLine struct {
	data    []byte
	tooLong bool
	err     error
}

// Bridge relays JSON-RPC lines from In to the server and the responses to Out, until In ends or ctx is cancelled.
// A request the server cannot answer gets a JSON-RPC error with its own id, so the client does not hang. A
// notification gets no output. Logs go to Logger only.
//
// A cancelled context returns at once, but a read that is already blocked on In returns only when In ends. The
// caller closes In on shutdown.
func Bridge(ctx context.Context, cfg BridgeConfig) error {
	if err := checkBridgeConfig(cfg); err != nil {
		return err
	}
	b := &bridge{cfg: cfg}
	lines := make(chan bridgeLine)
	go readLines(ctx, cfg.In, lines)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case line, ok := <-lines:
			if !ok {
				return nil
			}
			if line.err != nil {
				return fmt.Errorf("read client input: %w", line.err)
			}
			if err := b.handle(ctx, line); err != nil {
				return err
			}
		}
	}
}

// checkBridgeConfig refuses a bridge that cannot run.
func checkBridgeConfig(cfg BridgeConfig) error {
	switch {
	case cfg.URL == "":
		return errors.New("bridge server URL is required")
	case cfg.Token == "":
		return errors.New("bridge agent token is required")
	case cfg.In == nil:
		return errors.New("bridge input is required")
	case cfg.Out == nil:
		return errors.New("bridge output is required")
	case cfg.Logger == nil:
		return errors.New("bridge logger is required")
	}
	return nil
}

// bridge holds the state of one relay.
type bridge struct {
	cfg BridgeConfig
}

// handle relays one line. It returns an error only when the output cannot be written, because nothing else can be
// answered after that.
func (b *bridge) handle(ctx context.Context, line bridgeLine) error {
	if line.tooLong {
		return b.write(ctx, errorReply(nullID(), codeInvalidRequest, "the request is too large"))
	}
	if trimmed := bytes.TrimLeft(line.data, " \t"); len(trimmed) > 0 && trimmed[0] == '[' {
		return b.write(ctx, errorReply(nullID(), codeInvalidRequest, "batch requests are not supported"))
	}
	var msg rpcMessage
	if err := json.Unmarshal(line.data, &msg); err != nil {
		return b.write(ctx, errorReply(nullID(), codeParseError, "parse error"))
	}
	return b.forward(ctx, msg.ID, line.data)
}

// forward posts one message to the server and writes its answer. A notification (no id) is posted and gets no
// output, even when the post fails, because there is no request to answer.
func (b *bridge) forward(ctx context.Context, id json.RawMessage, body []byte) error {
	reqCtx, cancel := context.WithTimeout(ctx, bridgeRequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, b.cfg.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build bridge request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+b.cfg.Token)
	client := b.cfg.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		b.cfg.Logger.WarnContext(ctx, "mcp bridge request failed", "cause", err)
		return b.answerError(ctx, id, codeUnreachable, "the server could not be reached")
	}
	defer func() { _ = resp.Body.Close() }() // the body is read in full below; a close error has no use here
	return b.answer(ctx, id, resp)
}

// answer writes the server's answer for id, or a JSON-RPC error when the answer is not usable.
func (b *bridge) answer(ctx context.Context, id json.RawMessage, resp *http.Response) error {
	switch resp.StatusCode {
	case http.StatusAccepted:
		return nil
	case http.StatusUnauthorized:
		b.cfg.Logger.WarnContext(ctx, "mcp bridge token refused")
		return b.answerError(ctx, id, codeUnauthorized, "the server refused the agent token")
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		b.cfg.Logger.WarnContext(ctx, "mcp bridge answer unreadable", "cause", err)
		return b.answerError(ctx, id, codeUnreachable, "the server answer could not be read")
	}
	if len(raw) > maxBodyBytes || !json.Valid(raw) {
		b.cfg.Logger.WarnContext(ctx, "mcp bridge answer not usable", "status", resp.StatusCode)
		return b.answerError(ctx, id, codeUnreachable, "the server gave an answer that is not JSON-RPC")
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return fmt.Errorf("compact server answer: %w", err)
	}
	return b.writeLine(compact.Bytes())
}

// answerError writes a JSON-RPC error for id. A notification has no id, so it is only logged.
func (b *bridge) answerError(ctx context.Context, id json.RawMessage, code int, message string) error {
	if id == nil {
		return nil
	}
	return b.write(ctx, errorReply(id, code, message))
}

// write encodes one reply as one output line.
func (b *bridge) write(ctx context.Context, rep rpcReply) error {
	raw, err := json.Marshal(rep)
	if err != nil {
		b.cfg.Logger.ErrorContext(ctx, "mcp bridge reply encode failed", "cause", err)
		return nil
	}
	return b.writeLine(raw)
}

// writeLine writes raw and a newline to Out.
func (b *bridge) writeLine(raw []byte) error {
	if _, err := b.cfg.Out.Write(append(raw, '\n')); err != nil {
		return fmt.Errorf("write bridge output: %w", err)
	}
	return nil
}

// readLines reads the client's input line by line and sends each line on out. It stops at the end of the input, at
// the first read error, or when ctx is cancelled. It closes out when it stops.
func readLines(ctx context.Context, in io.Reader, out chan<- bridgeLine) {
	defer close(out)
	br := bufio.NewReaderSize(in, readBufferBytes)
	for {
		data, tooLong, err := readLine(br)
		if len(data) > 0 || tooLong {
			if !sendLine(ctx, out, bridgeLine{data: data, tooLong: tooLong}) {
				return
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return
			}
			sendLine(ctx, out, bridgeLine{err: err})
			return
		}
	}
}

// sendLine sends one line unless ctx ends first. It reports whether the line was sent.
func sendLine(ctx context.Context, out chan<- bridgeLine, line bridgeLine) bool {
	select {
	case out <- line:
		return true
	case <-ctx.Done():
		return false
	}
}

// readLine reads one line without its end of line. A line over maxBodyBytes is read to its end and reported as too
// long, so the lines after it still arrive.
func readLine(br *bufio.Reader) ([]byte, bool, error) {
	var data []byte
	tooLong := false
	for {
		chunk, isPrefix, err := br.ReadLine()
		if !tooLong {
			if len(data)+len(chunk) > maxBodyBytes {
				tooLong = true
				data = nil
			} else {
				data = append(data, chunk...)
			}
		}
		if err != nil || !isPrefix {
			return data, tooLong, err
		}
	}
}
