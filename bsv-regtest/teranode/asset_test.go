package teranode

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The envelope as teranode v0.16.0 returns it for /settings?category=none.
func TestBuildInfoReadsSettingsEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, p, ok := r.BasicAuth(); !ok || u != "bitcoin" || p != "bitcoin" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"Authentication required","success":false}`))
			return
		}
		if r.URL.Path != "/settings" || r.URL.Query().Get("category") != "none" {
			t.Errorf("unexpected request %s", r.URL)
		}
		_, _ = w.Write([]byte(`{"settings":[],"total":660,"filtered":0,"version":"v0.16.0","commit":"4edb60a40"}`))
	}))
	defer srv.Close()

	c := NewAssetClient(srv.URL)
	if _, _, err := c.BuildInfo(context.Background()); err == nil {
		t.Fatal("without credentials the 401 must be an error")
	}
	c.User, c.Pass = "bitcoin", "bitcoin"
	v, commit, err := c.BuildInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if v != "v0.16.0" || commit != "4edb60a40" {
		t.Fatalf("got %q %q", v, commit)
	}
}

func TestBuildInfoWithoutVersionIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"settings":[],"total":660,"filtered":0}`))
	}))
	defer srv.Close()

	if _, _, err := NewAssetClient(srv.URL).BuildInfo(context.Background()); err == nil {
		t.Fatal("an envelope without a version must be an error, not an empty version")
	}
}

// The freeze RPC needs the output's UTXO hash; it comes from the node's /utxos rows.
func TestUTXOHashPicksTheOutput(t *testing.T) {
	const txid = "4a5e1e4baab89f3a32518a88c31bc87f618f76673e2cc77ab2127b7afdeda33b"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/utxos/"+txid+"/json" {
			t.Errorf("unexpected request %s", r.URL)
		}
		_, _ = w.Write([]byte(`[
			{"txid":"` + txid + `","vout":0,"utxoHash":"aa00000000000000000000000000000000000000000000000000000000000000","status":"OK"},
			{"txid":"` + txid + `","vout":1,"utxoHash":"bb00000000000000000000000000000000000000000000000000000000000000","status":"OK"},
			{"txid":"` + txid + `","vout":2,"status":"OK"}]`))
	}))
	defer srv.Close()

	c := NewAssetClient(srv.URL)
	h, err := c.UTXOHash(context.Background(), txid, 1)
	if err != nil {
		t.Fatal(err)
	}
	if h != "bb00000000000000000000000000000000000000000000000000000000000000" {
		t.Fatalf("got %q", h)
	}
	if _, err := c.UTXOHash(context.Background(), txid, 2); err == nil {
		t.Fatal("a row without a utxoHash must be an error")
	}
	if _, err := c.UTXOHash(context.Background(), txid, 7); err == nil {
		t.Fatal("a missing output must be an error")
	}
}
