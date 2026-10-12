package main

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClaireGateways(t *testing.T) {
	h := testServer(t)
	headers := map[string]string{"X-API-Key": apiKey, "Idempotency-Key": "crm-create"}
	call := func(kind string, input any, status int) (*httptest.ResponseRecorder, map[string]any) {
		t.Helper()
		body, _ := json.Marshal(input)
		w := request(h, "POST", "/secure/gateways/"+kind, string(body), headers)
		if w.Code != status {
			t.Fatalf("%s: status %d want %d: %s", kind, w.Code, status, w.Body.String())
		}
		var result map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return w, result
	}
	create := map[string]any{"operation": "create_record", "record_type": "contact", "fields": map[string]any{"name": "Demo", "email": "demo@example.com"}}
	first, record := call("crm", create, 201)
	replay, _ := call("crm", create, 201)
	if replay.Body.String() != first.Body.String() || replay.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatal("retry not replayed")
	}
	create["fields"] = map[string]any{"name": "Changed"}
	call("crm", create, 409)
	// Verify persistence, including replay metadata, across reopening SQLite.
	db.Close()
	var err error
	db, err = openDB()
	if err != nil {
		t.Fatal(err)
	}
	create["fields"] = map[string]any{"name": "Demo", "email": "demo@example.com"}
	replay, _ = call("crm", create, 201)
	if replay.Body.String() != first.Body.String() {
		t.Fatal("replay lost on restart")
	}
	delete(headers, "Idempotency-Key")
	_, found := call("crm", map[string]any{"operation": "find_record", "record_type": "contact", "match": map[string]any{"email": "demo@example.com"}}, 200)
	if found["id"] != record["id"] || found["found"] != true {
		t.Fatal("CRM readback failed")
	}
	headers["Idempotency-Key"] = "note"
	note := map[string]any{"operation": "add_note", "record_type": "contact", "record_id": record["id"], "note": "Called"}
	call("crm", note, 200)
	call("crm", note, 200)
	delete(headers, "Idempotency-Key")
	_, found = call("crm", map[string]any{"operation": "find_record", "record_type": "contact", "match": map[string]any{"id": record["id"]}}, 200)
	if len(found["notes"].([]any)) != 1 {
		t.Fatal("retry duplicated note")
	}
	call("database", map[string]any{"operation": "create", "resource": "orders", "values": map[string]any{"state": "new"}}, 201)
	call("database", map[string]any{"operation": "update", "resource": "orders", "values": map[string]any{"state": "done"}}, 400)
	_, result := call("database", map[string]any{"operation": "update", "resource": "orders", "filters": map[string]any{"state": "new"}, "values": map[string]any{"state": "done"}}, 200)
	if result["count"] != float64(1) {
		t.Fatal("database update failed")
	}
	_, result = call("database", map[string]any{"operation": "read", "resource": "orders", "filters": map[string]any{"state": "done"}}, 200)
	if result["count"] != float64(1) {
		t.Fatal("database readback failed")
	}
	call("database", map[string]any{"operation": "delete", "resource": "orders"}, 400)
	call("database", map[string]any{"operation": "delete", "resource": "orders", "filters": map[string]any{"state": "done"}}, 200)
	_, result = call("database", map[string]any{"operation": "read", "resource": "orders"}, 200)
	if result["count"] != float64(0) {
		t.Fatal("delete failed")
	}
	booking := map[string]any{"operation": "create_booking", "calendar_id": "demo", "timezone": "UTC", "start_at": "2030-01-10T10:00:00Z", "end_at": "2030-01-10T10:30:00Z", "title": "Demo", "attendee_email": "demo@example.com"}
	_, event := call("calendar", booking, 201)
	call("calendar", booking, 409)
	booking["operation"] = "check_availability"
	_, result = call("calendar", booking, 200)
	if result["available"] != false {
		t.Fatal("overlap not detected")
	}
	booking["operation"] = "reschedule_booking"
	booking["event_id"] = event["event_id"]
	booking["start_at"] = "2030-01-10T11:00:00Z"
	booking["end_at"] = "2030-01-10T11:30:00Z"
	call("calendar", booking, 200)
	call("calendar", map[string]any{"operation": "cancel_booking", "calendar_id": "demo", "timezone": "UTC", "event_id": event["event_id"]}, 200)
	_, result = call("business", map[string]any{"operation": "create_support_ticket", "description": "Test", "inputs": map[string]any{"subject": "Demo"}}, 201)
	if result["mock"] != true || result["status"] != "accepted" {
		t.Fatal("business mock missing")
	}
	call("voice", map[string]any{"analysis": "summary", "transcript": "Demo transcript", "language": "en"}, 200)
	call("voice", map[string]any{"analysis": "quality", "transcript": "Demo", "language": "en"}, 400)
	for _, kind := range []string{"crm", "calendar", "business", "database", "voice"} {
		if request(h, "POST", "/secure/gateways/"+kind, "{}", nil).Code != 401 {
			t.Fatal("missing authentication", kind)
		}
		if request(h, "POST", "/secure/gateways/"+kind, "[]", headers).Code != 400 {
			t.Fatal("nonobject accepted", kind)
		}
	}
	call("database", map[string]any{"operation": "read|delete", "resource": "orders"}, 400)
	if request(h, "POST", "/secure/gateways/voice", strings.Repeat("x", 65537), headers).Code != 413 {
		t.Fatal("body size limit missing")
	}
	preflight := request(h, "OPTIONS", "/secure/gateways/crm", "", map[string]string{"Origin": "https://my-app.example", "Access-Control-Request-Method": "POST", "Access-Control-Request-Headers": "content-type,x-api-key,idempotency-key"})
	if preflight.Code != 204 || preflight.Header().Get("Access-Control-Allow-Origin") != "*" || !strings.Contains(preflight.Header().Get("Access-Control-Allow-Headers"), "Idempotency-Key") {
		t.Fatal("gateway CORS failed")
	}
	if request(h, "GET", "/postman_collection.json", "", nil).Code != 200 {
		t.Fatal("Postman unavailable")
	}
}
