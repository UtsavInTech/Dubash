// Copyright 2026 the Dubash contributors. Apache-2.0.

package web

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/UtsavInTech/dubash/internal/asr"
	"github.com/UtsavInTech/dubash/internal/bank"
)

// call sends an authorized request and decodes the JSON answer into out.
func call(t *testing.T, method, u string, body any, out any) int {
	t.Helper()
	var rd *bytes.Reader
	ct := ""
	switch b := body.(type) {
	case nil:
		rd = bytes.NewReader(nil)
	case url.Values:
		rd, ct = bytes.NewReader([]byte(b.Encode())), "application/x-www-form-urlencoded"
	default:
		data, _ := json.Marshal(b)
		rd, ct = bytes.NewReader(data), "application/json"
	}
	req, _ := http.NewRequest(method, u, rd)
	req.Header.Set("Authorization", "Bearer secret")
	if ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil && resp.StatusCode == http.StatusOK {
		json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode
}

// TestBankFlow walks the admin's path: add an entry, publish the event's
// version, edit the bank (the live event does not change), fix a line live,
// approve the suggestion, publish again, roll back.
func TestBankFlow(t *testing.T) {
	srv := newTestServer(t)
	if resp, _ := get(t, srv.URL+"/api/admin/bank/entries"); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bank without token: %d", resp.StatusCode)
	}
	var e bank.Entry
	if code := call(t, "POST", srv.URL+"/api/admin/bank/entries", bank.Entry{
		Scope: bank.ScopeEvent, ScopeID: "sala-a", Term: "Nerdearla", Keep: true, Status: bank.Approved,
		Translations: map[string]string{"es": "Nerdearla (conferencia)"},
	}, &e); code != 200 || e.ID == "" || e.Source != bank.SourceManual {
		t.Fatalf("create: %d %+v", code, e)
	}
	if code := call(t, "POST", srv.URL+"/api/admin/bank/entries", bank.Entry{Scope: bank.ScopeEvent}, nil); code != 400 {
		t.Fatalf("invalid entry: %d", code)
	}
	var view contextView
	call(t, "GET", srv.URL+"/api/admin/events/sala-a/context", nil, &view)
	if view.InForce != 0 || len(view.Pending.Added) != 1 {
		t.Fatalf("before publishing: %+v", view)
	}
	var v1 bank.Version
	if code := call(t, "POST", srv.URL+"/api/admin/events/sala-a/context", url.Values{"note": {"day 1"}}, &v1); code != 200 || v1.Number != 1 {
		t.Fatalf("publish: %d %+v", code, v1)
	}
	e.Aliases = []string{"Nerdearla 2026"}
	call(t, "PUT", srv.URL+"/api/admin/bank/entries/"+e.ID, e, &e)
	call(t, "GET", srv.URL+"/api/admin/events/sala-a/context", nil, &view)
	if view.InForce != 1 || len(view.Pending.Changed) != 1 {
		t.Fatalf("an edit must wait for publishing: %+v", view)
	}

	// A live line, then a fix of it.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/ingest/sala-a?token=secret", nil)
	if err != nil {
		t.Fatal(err)
	}
	for range 10 {
		ws.Write(ctx, websocket.MessageBinary, make([]byte, asr.BytesPerSecond/10))
	}
	ws.Close(websocket.StatusNormalClosure, "")
	for {
		if _, body := get(t, srv.URL+"/export/sala-a/txt?lang=en"); strings.Contains(body, "Nerdearla") {
			break
		}
		if ctx.Err() != nil {
			t.Fatal("no line")
		}
		time.Sleep(20 * time.Millisecond)
	}
	var fix correctResponse
	if code := call(t, "POST", srv.URL+"/api/admin/rooms/sala-a/segments/0/correct", url.Values{"text": {"Welcome to Dubash."}}, &fix); code != 200 || len(fix.Suggested) != 1 || fix.Warning != "" {
		t.Fatalf("correct: %d %+v", code, fix)
	}
	if code := call(t, "POST", srv.URL+"/api/admin/rooms/sala-a/segments/99/correct", url.Values{"text": {"x"}}, nil); code != 404 {
		t.Fatalf("unknown line: %d", code)
	}
	var sugs []bank.Entry
	call(t, "GET", srv.URL+"/api/admin/bank/entries?status=suggested", nil, &sugs)
	if len(sugs) != 1 || sugs[0].Term != "Dubash" || sugs[0].Misheard[0] != "Nerdearla" {
		t.Fatalf("suggestions %+v", sugs)
	}
	var appr bank.Entry
	if code := call(t, "POST", srv.URL+"/api/admin/bank/entries/"+sugs[0].ID+"/approve", nil, &appr); code != 200 || appr.Status != bank.Approved {
		t.Fatalf("approve: %d %+v", code, appr)
	}
	var cs []bank.Correction
	call(t, "GET", srv.URL+"/api/admin/bank/corrections?event=sala-a", nil, &cs)
	if len(cs) != 1 || cs[0].Corrected != "Welcome to Dubash." {
		t.Fatalf("corrections %+v", cs)
	}
	var v2 bank.Version
	call(t, "POST", srv.URL+"/api/admin/events/sala-a/context", nil, &v2)
	if v2.Number != 2 || len(v2.Entries) != 2 {
		t.Fatalf("version 2 %+v", v2)
	}
	var back bank.Version
	if code := call(t, "POST", srv.URL+"/api/admin/events/sala-a/context/"+itoa(v1.ID)+"/pin", nil, &back); code != 200 || back.Number != 1 {
		t.Fatalf("rollback %d %+v", code, back)
	}
	call(t, "GET", srv.URL+"/api/admin/events/sala-a/context", nil, &view)
	if view.InForce != 1 {
		t.Fatalf("after rollback %+v", view)
	}
	if code := call(t, "POST", srv.URL+"/api/admin/events/sala-b/context/"+itoa(v1.ID)+"/pin", nil, nil); code != 404 {
		t.Fatalf("pinned another event's version: %d", code)
	}
	var audit []bank.AuditEntry
	call(t, "GET", srv.URL+"/api/admin/bank/audit?limit=3", nil, &audit)
	if len(audit) != 3 || audit[0].Action != "pin" {
		t.Fatalf("audit %+v", audit)
	}
	if resp, body := get(t, srv.URL+"/bank"); resp.StatusCode != 200 || !strings.Contains(body, "Dictation Bank") {
		t.Fatalf("bank page %d", resp.StatusCode)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
