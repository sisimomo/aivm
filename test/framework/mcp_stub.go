package framework

import "context"

// NoopMCP is a no-op implementation of mcp.MCPManager used in integration
// tests. It avoids any Docker / mcpjungle dependency.
type NoopMCP struct{}

func (n *NoopMCP) Start(_ context.Context) error    { return nil }
func (n *NoopMCP) Stop(_ context.Context) error     { return nil }
func (n *NoopMCP) IsHealthy(_ context.Context) bool { return true }
