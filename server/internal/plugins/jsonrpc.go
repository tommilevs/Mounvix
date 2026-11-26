package plugins

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync/atomic"
	"unicode/utf8"
)

const maxRPCFrameBytes = 1 << 20

type rpcMessage struct {
	JSONRPC       string          `json:"jsonrpc"`
	ID            json.RawMessage `json:"id,omitempty"`
	Method        string          `json:"method,omitempty"`
	Params        json.RawMessage `json:"params,omitempty"`
	Result        json.RawMessage `json:"result,omitempty"`
	Error         *rpcError       `json:"error,omitempty"`
	resultPresent bool            `json:"-"`
	errorPresent  bool            `json:"-"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcConn struct {
	conn     net.Conn
	reader   *bufio.Reader
	nextID   atomic.Uint64
	pluginID string
	settings func(string) (json.RawMessage, error)
}

func decodeRPCMessage(frame []byte) (rpcMessage, error) {
	var message rpcMessage
	if len(frame) == 0 || len(frame) > maxRPCFrameBytes {
		return message, errors.New("JSON-RPC frame size is invalid")
	}
	if !utf8.Valid(frame) {
		return message, errors.New("JSON-RPC frame must be valid UTF-8")
	}
	if !json.Valid(frame) {
		return message, errors.New("malformed JSON-RPC frame")
	}
	if err := json.Unmarshal(frame, &message); err != nil {
		return message, fmt.Errorf("decode JSON-RPC frame: %w", err)
	}
	var members map[string]json.RawMessage
	if err := json.Unmarshal(frame, &members); err != nil {
		return message, fmt.Errorf("inspect JSON-RPC frame: %w", err)
	}
	_, message.resultPresent = members["result"]
	errorObject, hasError := members["error"]
	message.errorPresent = hasError
	if hasError {
		if message.Error == nil {
			return message, errors.New("JSON-RPC error member must be an object")
		}
		var errorFields map[string]json.RawMessage
		if err := json.Unmarshal(errorObject, &errorFields); err != nil || errorFields == nil {
			return message, errors.New("JSON-RPC error member must be an object")
		}
		code, ok := errorFields["code"]
		if !ok || json.Unmarshal(code, new(int)) != nil {
			return message, errors.New("JSON-RPC error code must be an integer")
		}
		text, ok := errorFields["message"]
		if !ok || json.Unmarshal(text, new(string)) != nil {
			return message, errors.New("JSON-RPC error message must be a string")
		}
	}
	if message.JSONRPC != "2.0" {
		return message, errors.New("JSON-RPC version must be 2.0")
	}
	if len(message.ID) != 0 && string(message.ID) != "null" && !json.Valid(message.ID) {
		return message, errors.New("invalid JSON-RPC request id")
	}
	if len(message.ID) == 0 || string(message.ID) == "null" {
		return message, errors.New("JSON-RPC request id is required")
	}
	if message.ID[0] != '"' && (message.ID[0] < '0' || message.ID[0] > '9') && message.ID[0] != '-' {
		return message, errors.New("JSON-RPC request id must be a string or number")
	}
	if message.Method == "" && message.resultPresent == message.errorPresent {
		return message, errors.New("JSON-RPC response must contain exactly one of result or error")
	}
	if message.Method != "" && (message.resultPresent || message.errorPresent) {
		return message, errors.New("JSON-RPC message cannot be both request and response")
	}
	return message, nil
}

func rpcResponse(id json.RawMessage, result json.RawMessage, rpcErr *rpcError) rpcMessage {
	if rpcErr != nil {
		result = nil
	} else if len(result) == 0 {
		result = json.RawMessage("null")
	}
	return rpcMessage{JSONRPC: "2.0", ID: append(json.RawMessage(nil), id...), Result: result, Error: rpcErr}
}

func (c *rpcConn) call(method string, params json.RawMessage) (json.RawMessage, error) {
	id, err := json.Marshal(c.nextID.Add(1))
	if err != nil {
		return nil, fmt.Errorf("encode JSON-RPC request id: %w", err)
	}
	request := rpcMessage{JSONRPC: "2.0", ID: id, Method: method, Params: params}
	if err := writeRPCFrame(c.conn, request); err != nil {
		return nil, err
	}
	for {
		frame, err := readRPCFrame(c.reader)
		if err != nil {
			return nil, err
		}
		message, err := decodeRPCMessage(frame)
		if err != nil {
			return nil, err
		}
		if message.Method != "" {
			if err := c.handleReverseRequest(message); err != nil {
				return nil, err
			}
			continue
		}
		if string(message.ID) != string(id) {
			return nil, errors.New("JSON-RPC response id mismatch")
		}
		if message.Error != nil {
			return nil, fmt.Errorf("plugin JSON-RPC error %d: %s", message.Error.Code, message.Error.Message)
		}
		return message.Result, nil
	}
}

func (c *rpcConn) handleReverseRequest(request rpcMessage) error {
	var result json.RawMessage
	var rpcErr *rpcError
	switch request.Method {
	case "host.log":
		// Plugin log messages are intentionally accepted without reflecting content to callers.
		result = json.RawMessage(`{"accepted":true}`)
	case "host.settings.get":
		var args struct {
			Key string `json:"key"`
		}
		if err := json.Unmarshal(request.Params, &args); err != nil || args.Key == "" || c.settings == nil {
			rpcErr = &rpcError{Code: -32602, Message: "settings unavailable"}
		} else if value, err := c.settings(args.Key); err != nil || !json.Valid(value) {
			rpcErr = &rpcError{Code: -32602, Message: "setting unavailable"}
		} else {
			result = value
		}
	default:
		rpcErr = &rpcError{Code: -32601, Message: "method not found"}
	}
	return writeRPCFrame(c.conn, rpcResponse(request.ID, result, rpcErr))
}

func readRPCFrame(reader *bufio.Reader) ([]byte, error) {
	frame := make([]byte, 0, 4096)
	for {
		part, err := reader.ReadSlice('\n')
		if len(frame)+len(part) > maxRPCFrameBytes+1 {
			return nil, errors.New("JSON-RPC frame exceeds 1 MiB")
		}
		frame = append(frame, part...)
		if err == nil {
			return frame[:len(frame)-1], nil
		}
		if !errors.Is(err, bufio.ErrBufferFull) {
			if errors.Is(err, io.EOF) && len(frame) != 0 {
				return nil, errors.New("JSON-RPC frame is missing newline terminator")
			}
			return nil, err
		}
	}
}

func writeRPCFrame(writer io.Writer, message rpcMessage) error {
	frame, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("encode JSON-RPC message: %w", err)
	}
	if len(frame) > maxRPCFrameBytes {
		return errors.New("JSON-RPC frame exceeds 1 MiB")
	}
	frame = append(frame, '\n')
	written, err := writer.Write(frame)
	if err != nil {
		return err
	}
	if written != len(frame) {
		return io.ErrShortWrite
	}
	return nil
}
