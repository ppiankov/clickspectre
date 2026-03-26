// Package mcp implements a minimal MCP (Model Context Protocol) server
// for exposing clickspectre commands as tools over stdio.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
)

// Tool describes an MCP tool.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// Handler processes a tool call and returns the result as text.
type Handler func(ctx context.Context, args map[string]interface{}) (string, error)

// Server is a minimal MCP server over stdio.
type Server struct {
	name     string
	version  string
	tools    []Tool
	handlers map[string]Handler
	mu       sync.Mutex
}

// NewServer creates a new MCP server.
func NewServer(name, version string) *Server {
	return &Server{
		name:     name,
		version:  version,
		handlers: make(map[string]Handler),
	}
}

// RegisterTool adds a tool to the server.
func (s *Server) RegisterTool(name, description string, schema json.RawMessage, handler Handler) {
	s.tools = append(s.tools, Tool{
		Name:        name,
		Description: description,
		InputSchema: schema,
	})
	s.handlers[name] = handler
}

// Run starts the server on stdio, reading JSON-RPC requests and writing responses.
func (s *Server) Run(ctx context.Context) error {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<20) // 1MB buffer

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req jsonRPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			s.writeError(req.ID, -32700, "parse error")
			continue
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		s.handleRequest(ctx, &req)
	}

	return scanner.Err()
}

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"`
	Result  interface{} `json:"result,omitempty"`
	Error   *rpcError   `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (s *Server) handleRequest(ctx context.Context, req *jsonRPCRequest) {
	switch req.Method {
	case "initialize":
		s.writeResult(req.ID, map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]interface{}{
				"tools": map[string]interface{}{},
			},
			"serverInfo": map[string]interface{}{
				"name":    s.name,
				"version": s.version,
			},
		})

	case "notifications/initialized":
		// No response needed for notifications

	case "tools/list":
		s.writeResult(req.ID, map[string]interface{}{
			"tools": s.tools,
		})

	case "tools/call":
		s.handleToolCall(ctx, req)

	default:
		if req.ID != nil {
			s.writeError(req.ID, -32601, fmt.Sprintf("method not found: %s", req.Method))
		}
	}
}

func (s *Server) handleToolCall(ctx context.Context, req *jsonRPCRequest) {
	var params struct {
		Name      string                 `json:"name"`
		Arguments map[string]interface{} `json:"arguments"`
	}

	if err := json.Unmarshal(req.Params, &params); err != nil {
		s.writeError(req.ID, -32602, "invalid params")
		return
	}

	handler, ok := s.handlers[params.Name]
	if !ok {
		s.writeError(req.ID, -32602, fmt.Sprintf("unknown tool: %s", params.Name))
		return
	}

	result, err := handler(ctx, params.Arguments)
	if err != nil {
		s.writeResult(req.ID, map[string]interface{}{
			"content": []map[string]interface{}{
				{"type": "text", "text": fmt.Sprintf("error: %s", err.Error())},
			},
			"isError": true,
		})
		return
	}

	s.writeResult(req.ID, map[string]interface{}{
		"content": []map[string]interface{}{
			{"type": "text", "text": result},
		},
	})
}

func (s *Server) writeResult(id interface{}, result interface{}) {
	s.writeResponse(&jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	})
}

func (s *Server) writeError(id interface{}, code int, message string) {
	s.writeResponse(&jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &rpcError{Code: code, Message: message},
	})
}

func (s *Server) writeResponse(resp *jsonRPCResponse) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.Marshal(resp)
	if err != nil {
		slog.Error("failed to marshal response", slog.String("error", err.Error()))
		return
	}
	data = append(data, '\n')
	if _, err := os.Stdout.Write(data); err != nil {
		slog.Error("failed to write response", slog.String("error", err.Error()))
	}
}
