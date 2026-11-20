package plugins

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"testing"
)

func TestJSONRPCFrameValidation(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		wantErr bool
	}{
		{name: "valid request", line: `{"jsonrpc":"2.0","id":1,"method":"plugin.invoke","params":{}}`},
		{name: "invalid version", line: `{"jsonrpc":"1.0","id":1,"method":"plugin.invoke"}`, wantErr: true},
		{name: "missing id", line: `{"jsonrpc":"2.0","method":"plugin.invoke"}`, wantErr: true},
		{name: "null id", line: `{"jsonrpc":"2.0","id":null,"method":"plugin.invoke"}`, wantErr: true},
		{name: "object id", line: `{"jsonrpc":"2.0","id":{},"method":"plugin.invoke"}`, wantErr: true},
		{name: "boolean id", line: `{"jsonrpc":"2.0","id":true,"method":"plugin.invoke"}`, wantErr: true},
		{name: "invalid json", line: `{"jsonrpc":"2.0",`, wantErr: true},
		{name: "oversized frame", line: strings.Repeat("x", maxRPCFrameBytes+1), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := decodeRPCMessage([]byte(tt.line))
			if (err != nil) != tt.wantErr {
				t.Fatalf("decodeRPCMessage() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestJSONRPCRejectsInvalidUTF8(t *testing.T) {
	frame := []byte(`{"jsonrpc":"2.0","id":1,"method":"plugin.invoke","params":{"value":"`)
	frame = append(frame, 0xff)
	frame = append(frame, []byte(`"}}`)...)
	if _, err := decodeRPCMessage(frame); err == nil {
		t.Fatal("JSON-RPC frame with invalid UTF-8 was accepted")
	}
}

func TestJSONRPCRejectsMalformedErrorResponses(t *testing.T) {
	frames := []string{
		`{"jsonrpc":"2.0","id":1,"result":null,"error":null}`,
		`{"jsonrpc":"2.0","id":1,"error":{}}`,
		`{"jsonrpc":"2.0","id":1,"error":{"code":-32602}}`,
		`{"jsonrpc":"2.0","id":1,"error":{"message":"bad"}}`,
		`{"jsonrpc":"2.0","id":1,"error":{"code":"bad","message":"bad"}}`,
	}
	for _, frame := range frames {
		if _, err := decodeRPCMessage([]byte(frame)); err == nil {
			t.Errorf("malformed JSON-RPC error response was accepted: %s", frame)
		}
	}
}

func TestJSONRPCReverseRequestCorrelation(t *testing.T) {
	client, server := net.Pipe()
	defer func() {
		if err := client.Close(); err != nil {
			t.Errorf("close JSON-RPC client: %v", err)
		}
	}()
	defer func() {
		if err := server.Close(); err != nil {
			t.Errorf("close JSON-RPC server: %v", err)
		}
	}()
	transport := &rpcConn{conn: client, reader: bufio.NewReader(client), settings: func(key string) (json.RawMessage, error) {
		if key != "theme" {
			return nil, errors.New("unexpected setting key")
		}
		return json.RawMessage(`"dark"`), nil
	}}
	serverDone := make(chan error, 1)
	go func() {
		reader := bufio.NewReader(server)
		if _, err := readRPCFrame(reader); err != nil {
			serverDone <- err
			return
		}
		if err := writeRPCFrame(server, rpcMessage{JSONRPC: "2.0", ID: json.RawMessage(`"reverse-9"`), Method: "host.log", Params: json.RawMessage(`{"message":"ok"}`)}); err != nil {
			serverDone <- err
			return
		}
		response, err := readRPCFrame(reader)
		if err != nil {
			serverDone <- err
			return
		}
		decoded, err := decodeRPCMessage(response)
		if err == nil && (string(decoded.ID) != `"reverse-9"` || decoded.Error != nil || string(decoded.Result) != `{"accepted":true}`) {
			err = errors.New("host.log response was not safe or did not preserve its request id")
		}
		if err != nil {
			serverDone <- err
			return
		}
		if err := writeRPCFrame(server, rpcMessage{JSONRPC: "2.0", ID: json.RawMessage(`"reverse-settings"`), Method: "host.settings.get", Params: json.RawMessage(`{"key":"theme"}`)}); err != nil {
			serverDone <- err
			return
		}
		settingsFrame, err := readRPCFrame(reader)
		if err != nil {
			serverDone <- err
			return
		}
		settingsResponse, err := decodeRPCMessage(settingsFrame)
		if err == nil && (string(settingsResponse.ID) != `"reverse-settings"` || string(settingsResponse.Result) != `"dark"`) {
			err = errors.New("settings response did not preserve safe value and request id")
		}
		if err != nil {
			serverDone <- err
			return
		}
		serverDone <- writeRPCFrame(server, rpcResponse(json.RawMessage("1"), json.RawMessage(`{"ok":true}`), nil))
	}()
	result, err := transport.call("plugin.invoke", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(result) != `{"ok":true}` {
		t.Fatalf("result = %s", result)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
}

func TestJSONRPCErrorResponseContainsOnlyErrorAndIsValid(t *testing.T) {
	response := rpcResponse(json.RawMessage(`"reverse-error"`), nil, &rpcError{Code: -32602, Message: "settings unavailable"})
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeRPCMessage(encoded)
	if err != nil {
		t.Fatalf("decode error response: %v; json=%s", err, encoded)
	}
	if decoded.Error == nil || len(decoded.Result) != 0 {
		t.Fatalf("error response contains result or no error: %+v", decoded)
	}
	both := []byte(`{"jsonrpc":"2.0","id":1,"result":null,"error":{"code":-32602,"message":"bad"}}`)
	if _, err := decodeRPCMessage(both); err == nil {
		t.Fatal("JSON-RPC response with both result and error was accepted")
	}
}

func TestJSONRPCResponsePreservesRequestID(t *testing.T) {
	request := rpcMessage{JSONRPC: "2.0", ID: json.RawMessage(`"request-7"`), Method: "host.log", Params: json.RawMessage(`{"level":"info","message":"ready"}`)}
	response := rpcResponse(request.ID, json.RawMessage(`null`), nil)
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeRPCMessage(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if string(decoded.ID) != string(request.ID) || decoded.Error != nil {
		t.Fatalf("response did not preserve request id: %+v", decoded)
	}
}
