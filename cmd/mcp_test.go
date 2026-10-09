package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type fakeBackend struct {
	err  error
	args []any // the arguments of the last call
}

func (f *fakeBackend) Status(context.Context) (any, error) {
	f.args = nil
	return map[string]any{"model": "DS923+"}, f.err
}

func (f *fakeBackend) Doctor(_ context.Context, skip, only []string) (any, error) {
	f.args = []any{skip, only}
	return map[string]any{"status": "ok"}, f.err
}

func (f *fakeBackend) Containers(_ context.Context, running bool, project string) (any, error) {
	f.args = []any{running, project}
	return []containerRow{{Name: "web-app-1", State: "running", Image: "example/app:latest", Status: "Up 2 days"}}, f.err
}

func (f *fakeBackend) Packages(_ context.Context, outdated bool) (any, error) {
	f.args = []any{outdated}
	return []packageRow{{ID: "web", Version: "1.2.0-0100", Latest: "1.3.0-0110", InStore: true}}, f.err
}

func connectMCP(t *testing.T, b backend) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	if _, err := newMCPServer(b).Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func callTool(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) (string, bool) {
	t.Helper()
	r, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if len(r.Content) != 1 {
		t.Fatalf("%s: got %d contents, want 1", name, len(r.Content))
	}
	text, ok := r.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("%s: content is %T, want text", name, r.Content[0])
	}
	return text.Text, r.IsError
}

func TestMCPTools(t *testing.T) {
	cs := connectMCP(t, &fakeBackend{})
	r, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range r.Tools {
		names = append(names, tool.Name)
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Errorf("%s is not marked read-only", tool.Name)
		}
	}
	want := []string{"syno_containers", "syno_doctor", "syno_packages", "syno_status"}
	slices.Sort(names)
	if !slices.Equal(names, want) {
		t.Errorf("tools = %v, want %v", names, want)
	}
}

func TestMCPCalls(t *testing.T) {
	b := &fakeBackend{}
	cs := connectMCP(t, b)
	tests := []struct {
		tool     string
		args     map[string]any
		wantArgs []any
		wantText string
	}{
		{"syno_status", nil, nil, `{"model":"DS923+"}`},
		{"syno_doctor", map[string]any{"only": []string{"volumes"}}, []any{[]string(nil), []string{"volumes"}}, `{"status":"ok"}`},
		{"syno_containers", map[string]any{"running": true, "project": "web"}, []any{true, "web"},
			`[{"name":"web-app-1","state":"running","image":"example/app:latest","status":"Up 2 days"}]`},
		{"syno_packages", map[string]any{"outdated": true}, []any{true},
			`[{"id":"web","name":"","version":"1.2.0-0100","latest":"1.3.0-0110","security":false,"in_store":true,"status":""}]`},
		{"syno_packages", nil, []any{false}, ""},
	}
	for _, tt := range tests {
		text, isErr := callTool(t, cs, tt.tool, tt.args)
		if isErr {
			t.Errorf("%s: error %s", tt.tool, text)
			continue
		}
		if tt.wantText != "" && text != tt.wantText {
			t.Errorf("%s: got %s, want %s", tt.tool, text, tt.wantText)
		}
		got, _ := json.Marshal(b.args)
		want, _ := json.Marshal(tt.wantArgs)
		if string(got) != string(want) {
			t.Errorf("%s: backend got %s, want %s", tt.tool, got, want)
		}
	}
}

func TestMCPErrors(t *testing.T) {
	cs := connectMCP(t, &fakeBackend{err: errors.New("no DSM found")})
	text, isErr := callTool(t, cs, "syno_status", nil)
	if !isErr || text != "no DSM found" {
		t.Errorf("got %q (error %v), want the backend error", text, isErr)
	}
	// Arguments of the wrong type are rejected before the backend runs.
	if _, isErr := callTool(t, cs, "syno_packages", map[string]any{"outdated": "yes"}); !isErr {
		t.Error("a string for outdated should be an error")
	}
}
