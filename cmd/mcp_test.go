package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/babarot/syno/internal/dsm"
)

const testHost = "https://192.168.1.10:5001"

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
	return &containerList{Host: testHost, Containers: []containerRow{{Name: "web-app-1", State: "running", Image: "example/app:latest", Status: "Up 2 days"}}}, f.err
}

func (f *fakeBackend) Packages(_ context.Context, outdated bool) (any, error) {
	f.args = []any{outdated}
	return &packageList{Host: testHost, Packages: []packageRow{{ID: "web", Version: "1.2.0-0100", Latest: "1.3.0-0110", InStore: true}}}, f.err
}

func (f *fakeBackend) Shares(_ context.Context, recycle bool) (any, error) {
	f.args = []any{recycle}
	return &shareList{Host: testHost, Shares: []shareRow{{Name: "media", Volume: "/volume1", UsedBytes: 1 << 40, RecycleBin: true}}}, f.err
}

func (f *fakeBackend) APIList(_ context.Context, filter string) (any, error) {
	f.args = []any{filter}
	return &apiList{Host: testHost, APIs: map[string]dsm.APIInfo{"SYNO.Core.Share": {Path: "entry.cgi", MinVersion: 1, MaxVersion: 1, RequestFormat: "JSON"}}}, f.err
}

func (f *fakeBackend) API(_ context.Context, api, method string, version int, params url.Values) (any, error) {
	f.args = []any{api, method, version, params}
	return &apiResult{Host: testHost, Data: json.RawMessage(`{"shares":[]}`)}, f.err
}

func connectMCP(t *testing.T, b backend, allowAPI bool) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	if _, err := newMCPServer(b, allowAPI).Connect(ctx, st, nil); err != nil {
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
	base := []string{"syno_api_list", "syno_containers", "syno_doctor", "syno_packages", "syno_shares", "syno_status"}
	for _, allowAPI := range []bool{false, true} {
		cs := connectMCP(t, &fakeBackend{}, allowAPI)
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
		want := base
		if allowAPI {
			want = append([]string{"syno_api"}, base...)
		}
		slices.Sort(names)
		if !slices.Equal(names, want) {
			t.Errorf("allowAPI %v: tools = %v, want %v", allowAPI, names, want)
		}

		// The instructions match the tools offered.
		instructions := cs.InitializeResult().Instructions
		if got := strings.Contains(instructions, "--allow-api"); got == allowAPI {
			t.Errorf("allowAPI %v: instructions mention --allow-api: %v", allowAPI, got)
		}
	}
}

func TestMCPCalls(t *testing.T) {
	b := &fakeBackend{}
	cs := connectMCP(t, b, true)
	tests := []struct {
		tool     string
		args     map[string]any
		wantArgs []any
		wantText string
	}{
		{"syno_status", nil, nil, `{"model":"DS923+"}`},
		{"syno_doctor", map[string]any{"only": []string{"volumes"}}, []any{[]string(nil), []string{"volumes"}}, `{"status":"ok"}`},
		{"syno_containers", map[string]any{"running": true, "project": "web"}, []any{true, "web"},
			`{"host":"` + testHost + `","containers":[{"name":"web-app-1","state":"running","image":"example/app:latest","status":"Up 2 days"}]}`},
		{"syno_packages", map[string]any{"outdated": true}, []any{true},
			`{"host":"` + testHost + `","packages":[{"id":"web","name":"","version":"1.2.0-0100","latest":"1.3.0-0110","security":false,"in_store":true,"status":""}]}`},
		{"syno_packages", nil, []any{false}, ""},
		{"syno_shares", map[string]any{"recycle": true}, []any{true}, ""},
		{"syno_shares", nil, []any{false},
			`{"host":"` + testHost + `","shares":[{"name":"media","volume":"/volume1","used_bytes":1099511627776,"hidden":false,"encrypted":false,"read_only":false,"usb":false,"recycle_bin":true}]}`},
		{"syno_api_list", map[string]any{"filter": "share"}, []any{"share"}, `{"host":"` + testHost + `","apis":{"SYNO.Core.Share":{"path":"entry.cgi","minVersion":1,"maxVersion":1,"requestFormat":"JSON"}}}`},
		{"syno_api", map[string]any{"api": "SYNO.Core.Share", "method": "list", "params": map[string]any{"additional": `["share_quota"]`}},
			[]any{"SYNO.Core.Share", "list", 0, url.Values{"additional": {`["share_quota"]`}}}, `{"host":"` + testHost + `","data":{"shares":[]}}`},
		{"syno_api", map[string]any{"api": "SYNO.Core.System", "method": "info", "version": 3},
			[]any{"SYNO.Core.System", "info", 3, url.Values{}}, ""},
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
	cs := connectMCP(t, &fakeBackend{err: errors.New("no DSM found")}, false)
	text, isErr := callTool(t, cs, "syno_status", nil)
	if !isErr || text != "no DSM found" {
		t.Errorf("got %q (error %v), want the backend error", text, isErr)
	}
	// Arguments of the wrong type are rejected before the backend runs.
	if _, isErr := callTool(t, cs, "syno_packages", map[string]any{"outdated": "yes"}); !isErr {
		t.Error("a string for outdated should be an error")
	}
}

func TestMCPAPIRefusesWrites(t *testing.T) {
	b := &fakeBackend{args: []any{"untouched"}}
	cs := connectMCP(t, b, true)
	for _, method := range []string{"set", "delete", "shutdown"} {
		text, isErr := callTool(t, cs, "syno_api", map[string]any{"api": "SYNO.Core.System", "method": method})
		if !isErr || !strings.Contains(text, "refused") {
			t.Errorf("%s: got %q (error %v), want a refusal", method, text, isErr)
		}
	}
	if len(b.args) != 1 || b.args[0] != "untouched" {
		t.Errorf("the backend was called with %v", b.args)
	}
	// api and method are required.
	if _, isErr := callTool(t, cs, "syno_api", map[string]any{"method": "list"}); !isErr {
		t.Error("a call without api should be an error")
	}
}

func TestCheckMCPStart(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := checkMCPStart(); err != nil {
		t.Errorf("without profiles: %v, want the server to start", err)
	}

	if err := os.MkdirAll(filepath.Join(dir, "syno"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "syno", "profiles.yaml"), []byte("current: [\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := checkMCPStart(); err == nil {
		t.Error("a broken profiles.yaml should fail at start")
	}
}
