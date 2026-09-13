package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHubSearchPreservesImportedRoots(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true,"data":{"results":[{"path":"/conversations/demo/conversation.md"},{"path":"/custom/note.md"},{"path":"/projects/demo/context.md"}]}}`))
	}))
	defer server.Close()
	result, err := hubSearch(context.Background(), &hubTarget{APIBase: server.URL}, "probe", "all")
	if err != nil {
		t.Fatal(err)
	}
	expected := []string{"/conversations/demo/conversation.md", "/custom/note.md", "project/demo/context.md"}
	if len(result.Results) != len(expected) {
		t.Fatalf("got %+v", result.Results)
	}
	for i, path := range expected {
		if result.Results[i].Path != path {
			t.Errorf("got %s, want %s", result.Results[i].Path, path)
		}
	}
}

func TestSearchScopeAcceptsImportedRoots(t *testing.T) {
	for raw, want := range map[string]string{"all": "all", "/": "all", "conversations": "/conversations", "/conversations/demo": "/conversations/demo", "/custom/CaseSensitive": "/custom/CaseSensitive", "project/demo": "/projects/demo"} {
		got, err := externalPathToSearchScope(raw)
		if err != nil || got != want {
			t.Errorf("%q: got %q, %v; want %q", raw, got, err, want)
		}
	}
}
