package mcp

import "context"

// MCPManager is the interface through which the rest of the application
// interacts with the MCP service. The concrete implementation is Manager;
// test code may substitute a no-op stub.
type MCPManager interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	IsHealthy(ctx context.Context) bool
	Logs() error
}
