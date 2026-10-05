package usageapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// RPCRequest is one JSON-RPC 2.0 request.
type RPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// RPCResponse is one JSON-RPC 2.0 result or error.
type RPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError follows the JSON-RPC 2.0 error object shape.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// RPCServer maps JSON-RPC methods to a Service.
type RPCServer struct {
	Service *Service
}

// MethodDescription is returned by rpc.discover.
type MethodDescription struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Params      string `json:"params"`
}

// Handle executes one already-decoded request.
func (server RPCServer) Handle(ctx context.Context, request RPCRequest) RPCResponse {
	response := RPCResponse{JSONRPC: "2.0", ID: request.ID}
	if server.Service == nil {
		response.Error = applicationError(errors.New("RPC service is not configured"))
		return response
	}
	if request.JSONRPC != "2.0" || strings.TrimSpace(request.Method) == "" {
		response.Error = &RPCError{Code: -32600, Message: "Invalid Request"}
		return response
	}

	var result any
	var err error
	switch request.Method {
	case "rpc.discover":
		if err = requireEmptyParams(request.Params); err == nil {
			result = discoverResult()
		}
	case "accounts.list":
		if err = requireEmptyParams(request.Params); err == nil {
			result, err = server.Service.ListAccounts()
		}
	case "accounts.rename":
		var params struct {
			Account string `json:"account"`
			NewName string `json:"new_name"`
		}
		if err = decodeParams(request.Params, &params); err == nil {
			err = requireStrings(map[string]string{"account": params.Account, "new_name": params.NewName})
		}
		if err == nil {
			result, err = server.Service.RenameAccount(params.Account, params.NewName)
		}
	case "accounts.remove":
		var params struct {
			Account string `json:"account"`
		}
		if err = decodeParams(request.Params, &params); err == nil {
			err = requireStrings(map[string]string{"account": params.Account})
		}
		if err == nil {
			result, err = server.Service.RemoveAccount(params.Account)
		}
	case "auth.device.begin":
		var params struct {
			Provider string `json:"provider"`
		}
		if err = decodeParams(request.Params, &params); err == nil {
			err = requireStrings(map[string]string{"provider": params.Provider})
		}
		if err == nil {
			result, err = server.Service.BeginDeviceAuth(ctx, params.Provider)
		}
	case "auth.device.poll":
		var params DeviceAuthPoll
		if err = decodeParams(request.Params, &params); err == nil {
			err = requireStrings(map[string]string{"session_id": params.SessionID, "user_code": params.UserCode})
		}
		if err == nil {
			result, err = server.Service.PollDeviceAuth(ctx, params)
		}
	case "accounts.api_key.save":
		var params APIKeyAccount
		if err = decodeParams(request.Params, &params); err == nil {
			err = requireStrings(map[string]string{"provider": params.Provider, "api_key": params.APIKey})
		}
		if err == nil {
			result, err = server.Service.SaveAPIKeyAccount(params)
		}
	case "usage.get":
		var params struct {
			Account string `json:"account"`
			Refresh *bool  `json:"refresh"`
		}
		if err = decodeParams(request.Params, &params); err == nil {
			refresh := true
			if params.Refresh != nil {
				refresh = *params.Refresh
			}
			result, err = server.Service.Usage(ctx, params.Account, refresh)
		}
	case "resets.get":
		var params struct {
			Account            string `json:"account"`
			Refresh            *bool  `json:"refresh"`
			IncludeUnavailable bool   `json:"include_unavailable"`
		}
		if err = decodeParams(request.Params, &params); err == nil {
			err = requireStrings(map[string]string{"account": params.Account})
		}
		if err == nil {
			refresh := true
			if params.Refresh != nil {
				refresh = *params.Refresh
			}
			result, err = server.Service.ResetCredits(ctx, params.Account, refresh, params.IncludeUnavailable)
		}
	case "settings.get":
		if err = requireEmptyParams(request.Params); err == nil {
			result, err = server.Service.Settings()
		}
	case "settings.set":
		var params Settings
		if err = decodeParams(request.Params, &params); err == nil {
			result, err = server.Service.UpdateSettings(params)
		}
	default:
		response.Error = &RPCError{Code: -32601, Message: "Method not found", Data: map[string]string{"method": request.Method}}
		return response
	}
	if err != nil {
		var parameterError *paramsError
		if errors.As(err, &parameterError) {
			response.Error = &RPCError{Code: -32602, Message: "Invalid params", Data: parameterError.Error()}
		} else {
			response.Error = applicationError(err)
		}
		return response
	}
	response.Result = result
	return response
}

// Serve reads one JSON-RPC request per line and writes one response per line.
// Requests without an id are notifications and do not produce a response.
func (server RPCServer) Serve(ctx context.Context, input io.Reader, output io.Writer) error {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		if !json.Valid(line) {
			if err := encoder.Encode(RPCResponse{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &RPCError{Code: -32700, Message: "Parse error"}}); err != nil {
				return err
			}
			continue
		}
		var request RPCRequest
		if err := json.Unmarshal(line, &request); err != nil {
			if err := encoder.Encode(RPCResponse{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &RPCError{Code: -32600, Message: "Invalid Request"}}); err != nil {
				return err
			}
			continue
		}
		response := server.Handle(ctx, request)
		if len(request.ID) == 0 {
			continue
		}
		if err := encoder.Encode(response); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func discoverResult() any {
	return struct {
		ProtocolVersion string              `json:"protocol_version"`
		Framing         string              `json:"framing"`
		Methods         []MethodDescription `json:"methods"`
	}{
		ProtocolVersion: ProtocolVersion,
		Framing:         "newline-delimited JSON-RPC 2.0",
		Methods: []MethodDescription{
			{"rpc.discover", "Describe the protocol and available methods.", "{}"},
			{"accounts.list", "List accounts without credentials.", "{}"},
			{"accounts.rename", "Rename an account.", `{"account":"id|name|email","new_name":"name"}`},
			{"accounts.remove", "Remove an account and its cache.", `{"account":"id|name|email"}`},
			{"accounts.api_key.save", "Create or update an API-key account.", `{"account":"optional id|name","provider":"opencode-go|deepseek","api_key":"...","name":"optional"}`},
			{"auth.device.begin", "Begin device authorization for a provider.", `{"provider":"codex"}`},
			{"auth.device.poll", "Poll and persist a device authorization.", `{"provider":"codex","session_id":"...","user_code":"...","name":"optional"}`},
			{"usage.get", "Get usage for one or all accounts; refresh defaults to true.", `{"account":"optional","refresh":true}`},
			{"resets.get", "Get reset credits for one account; refresh defaults to true.", `{"account":"...","refresh":true,"include_unavailable":false}`},
			{"settings.get", "Get application settings.", "{}"},
			{"settings.set", "Replace application settings; invalid values are normalized.", `Settings object`},
		},
	}
}

type paramsError struct{ err error }

func (error *paramsError) Error() string { return error.err.Error() }

func decodeParams(raw json.RawMessage, target any) error {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		raw = json.RawMessage("{}")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return &paramsError{err}
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("params contain multiple JSON values")
		}
		return &paramsError{err}
	}
	return nil
}

func requireEmptyParams(raw json.RawMessage) error {
	var params map[string]any
	if err := decodeParams(raw, &params); err != nil {
		return err
	}
	if len(params) != 0 {
		return &paramsError{fmt.Errorf("method does not accept params")}
	}
	return nil
}

func requireStrings(values map[string]string) error {
	for name, value := range values {
		if strings.TrimSpace(value) == "" {
			return &paramsError{fmt.Errorf("%s is required", name)}
		}
	}
	return nil
}

func applicationError(err error) *RPCError {
	return &RPCError{Code: -32000, Message: "Application error", Data: err.Error()}
}
