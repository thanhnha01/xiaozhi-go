package main

// Device-side MCP JSON-RPC responses. Cloud MCP tools registered in xiaozhi.me
// execute on the Xiaozhi backend, not inside this PC device simulator.
import (
    "encoding/json"
    "fmt"
    "log"
)

type deviceMCPRequest struct {
    JSONRPC string          `json:"jsonrpc"`
    ID      json.RawMessage `json:"id"`
    Method  string          `json:"method"`
}

func deviceMCPResponse(payload []byte) (string, error) {
    var request deviceMCPRequest
    if err := json.Unmarshal(payload, &request); err != nil {
        return "", fmt.Errorf("invalid device MCP JSON: %w", err)
    }
    if request.Method == "" {
        return "", fmt.Errorf("MCP request has no method")
    }
    // JSON-RPC notifications do not have an id and MUST NOT be replied to.
    if len(request.ID) == 0 || string(request.ID) == "null" {
        return "", nil
    }
    response := map[string]any{"jsonrpc": "2.0", "id": request.ID}
    switch request.Method {
    case "initialize":
        response["result"] = map[string]any{
            "protocolVersion": "2024-11-05",
            "capabilities": map[string]any{"tools": map[string]any{}},
            "serverInfo": map[string]any{"name": appName, "version": appVersion},
        }
    case "tools/list":
        // Do not advertise cloud MCP tools as local IoT tools.
        response["result"] = map[string]any{"tools": []any{}}
    case "ping":
        response["result"] = map[string]any{}
    default:
        response["error"] = map[string]any{
            "code": -32601,
            "message": "Local device MCP method not available: " + request.Method,
        }
    }
    data, err := json.Marshal(response)
    if err != nil {
        return "", err
    }
    return string(data), nil
}

func (app *App) handleMcp(payload []byte) {
    response, err := deviceMCPResponse(payload)
    if err != nil {
        log.Printf("Không thể xử lý device MCP: %v", err)
        return
    }
    if response == "" {
        return
    }
    if err := app.proto.SendMcpMessage(response); err != nil {
        log.Printf("Không thể gửi phản hồi device MCP: %v", err)
    }
}
