package main

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"time"
)

// ponytail: serialize small test datasets; use resource locks if gateway throughput grows.
var gatewayMu sync.Mutex
var gatewayResource = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
var errGatewayFull = errors.New("gateway dataset is full")
var errGatewaySize = errors.New("gateway record exceeds 64 KiB")

func gatewayOneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func initGatewaySchema(conn *sql.DB) error {
	_, err := conn.Exec(`CREATE TABLE IF NOT EXISTS gateway_records(id TEXT PRIMARY KEY,kind TEXT NOT NULL,resource TEXT NOT NULL,payload TEXT NOT NULL);
 CREATE INDEX IF NOT EXISTS gateway_resource ON gateway_records(kind,resource);
 CREATE TABLE IF NOT EXISTS gateway_replays(route TEXT NOT NULL,request_key TEXT NOT NULL,fingerprint TEXT NOT NULL,status INTEGER NOT NULL,response TEXT NOT NULL,PRIMARY KEY(route,request_key));`)
	return err
}

type gatewayInput struct {
	Operation     string         `json:"operation"`
	RecordType    string         `json:"record_type"`
	RecordID      string         `json:"record_id"`
	Fields        map[string]any `json:"fields"`
	Match         map[string]any `json:"match"`
	Note          string         `json:"note"`
	Resource      string         `json:"resource"`
	Filters       map[string]any `json:"filters"`
	Values        map[string]any `json:"values"`
	Limit         int            `json:"limit"`
	CalendarID    string         `json:"calendar_id"`
	Timezone      string         `json:"timezone"`
	StartAt       string         `json:"start_at"`
	EndAt         string         `json:"end_at"`
	EventID       string         `json:"event_id"`
	Title         string         `json:"title"`
	AttendeeEmail string         `json:"attendee_email"`
	AttendeeName  string         `json:"attendee_name"`
	Notes         string         `json:"notes"`
	Description   string         `json:"description"`
	Inputs        map[string]any `json:"inputs"`
	Analysis      string         `json:"analysis"`
	Transcript    string         `json:"transcript"`
	Language      string         `json:"language"`
	Instructions  string         `json:"instructions"`
}

func registerGateways(mux *http.ServeMux) {
	for _, kind := range []string{"crm", "calendar", "business", "database", "voice"} {
		mux.HandleFunc("POST /secure/gateways/"+kind, gatewayHandler(kind))
	}
}

func gatewayHandler(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if !requireAPIKey(w, r) {
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
		if err != nil {
			if _, ok := err.(*http.MaxBytesError); ok {
				writeError(w, 413, "Gateway body exceeds 64 KiB.")
			} else {
				writeError(w, 400, "Could not read gateway body.")
			}
			return
		}
		body = []byte(strings.TrimSpace(string(body)))
		var input gatewayInput
		if len(body) == 0 || body[0] != '{' || json.Unmarshal(body, &input) != nil {
			writeError(w, 400, "Send a JSON object matching the gateway operation.")
			return
		}
		requestKey := r.Header.Get("Idempotency-Key")
		if len(requestKey) > 256 {
			writeError(w, 400, "Idempotency-Key must be at most 256 bytes.")
			return
		}
		canonical, _ := json.Marshal(input)
		fingerprint := fmt.Sprintf("%x", sha256.Sum256(canonical))
		gatewayMu.Lock()
		defer gatewayMu.Unlock()
		tx, err := db.BeginTx(r.Context(), nil)
		if err != nil {
			writeError(w, 500, "Gateway database unavailable.")
			return
		}
		defer tx.Rollback()
		if requestKey != "" {
			var previousHash, response string
			var status int
			err = tx.QueryRow("SELECT fingerprint,status,response FROM gateway_replays WHERE route=? AND request_key=?", kind, requestKey).Scan(&previousHash, &status, &response)
			if err == nil {
				if previousHash != fingerprint {
					writeError(w, 409, "This idempotency key was already used with a different request.")
					return
				}
				w.Header().Set("Idempotency-Replayed", "true")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = w.Write([]byte(response))
				return
			}
			if err != sql.ErrNoRows {
				writeError(w, 500, "Gateway database unavailable.")
				return
			}
		}
		status, result, err := gatewayOperation(tx, kind, input)
		if err != nil {
			if errors.Is(err, errGatewayFull) {
				writeError(w, 409, "This mock dataset has reached its 1000-record limit.")
				return
			}
			if errors.Is(err, errGatewaySize) {
				writeError(w, 413, "A stored gateway record cannot exceed 64 KiB.")
				return
			}
			writeError(w, 500, "Gateway database operation failed.")
			return
		}
		result["mock"] = true
		if status >= 400 {
			writeJSON(w, status, result)
			return
		}
		response, _ := json.Marshal(result)
		if requestKey != "" {
			_, err = tx.Exec("INSERT INTO gateway_replays VALUES (?,?,?,?,?)", kind, requestKey, fingerprint, status, string(response))
		}
		if err != nil || tx.Commit() != nil {
			writeError(w, 500, "Gateway database operation failed.")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(response)
	}
}

func gatewayBad(status int, detail string) (int, map[string]any, error) {
	return status, map[string]any{"detail": detail}, nil
}
func gatewayRecords(tx *sql.Tx, kind, resource string) ([]map[string]any, error) {
	rows, err := tx.Query("SELECT payload FROM gateway_records WHERE kind=? AND resource=? ORDER BY rowid LIMIT 1000", kind, resource)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := []map[string]any{}
	for rows.Next() {
		var raw string
		var record map[string]any
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &record); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}
func gatewayMatch(record, filters map[string]any) bool {
	for key, value := range filters {
		actual, found := record[key]
		if !found || !reflect.DeepEqual(actual, value) {
			return false
		}
	}
	return true
}
func gatewaySave(tx *sql.Tx, kind, resource string, record map[string]any) error {
	if record["id"] == nil {
		var count int
		if err := tx.QueryRow("SELECT COUNT(*) FROM gateway_records WHERE kind=? AND resource=?", kind, resource).Scan(&count); err != nil {
			return err
		}
		if count >= 1000 {
			return errGatewayFull
		}
		var id [16]byte
		_, _ = rand.Read(id[:])
		record["id"] = kind + "_" + hex.EncodeToString(id[:])
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if len(raw) > 64<<10 {
		return errGatewaySize
	}
	_, err = tx.Exec("INSERT INTO gateway_records(id,kind,resource,payload) VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload", record["id"], kind, resource, string(raw))
	return err
}

func gatewayOperation(tx *sql.Tx, kind string, in gatewayInput) (int, map[string]any, error) {
	if kind == "voice" {
		if in.Transcript == "" || in.Language == "" {
			return gatewayBad(400, "Transcript and language are required.")
		}
		if !gatewayOneOf(in.Analysis, "summary", "sentiment", "intent", "quality", "custom") {
			return gatewayBad(400, "Choose summary, sentiment, intent, quality, or custom analysis.")
		}
		if (in.Analysis == "quality" || in.Analysis == "custom") && in.Instructions == "" {
			return gatewayBad(400, "Instructions are required for quality and custom analysis.")
		}
		preview := []rune(in.Transcript)
		if len(preview) > 240 {
			preview = preview[:240]
		}
		return 200, map[string]any{"analysis": in.Analysis, "language": in.Language, "result": map[string]any{"summary": string(preview), "sentiment": "neutral", "intent": "test_intent", "message": "Simulated text analysis; no AI or audio provider was called."}}, nil
	}
	if kind == "business" {
		if !gatewayResource.MatchString(in.Operation) || in.Description == "" {
			return gatewayBad(400, "A valid operation name and description are required.")
		}
		record := map[string]any{"operation": in.Operation, "description": in.Description, "inputs": in.Inputs, "status": "accepted"}
		return 201, record, gatewaySave(tx, kind, in.Operation, record)
	}
	if kind == "calendar" {
		return gatewayCalendar(tx, in)
	}
	resource := in.Resource
	if kind == "crm" {
		resource = in.RecordType
		if !gatewayOneOf(resource, "contact", "company", "deal", "case", "custom") {
			return gatewayBad(400, "Choose contact, company, deal, case, or custom record_type.")
		}
	}
	if !gatewayResource.MatchString(resource) {
		return gatewayBad(400, "Resource must contain lowercase letters, digits, and underscores, starting with a letter.")
	}
	records, err := gatewayRecords(tx, kind, resource)
	if err != nil {
		return 0, nil, err
	}
	op := in.Operation
	fields, filters := in.Values, in.Filters
	if kind == "crm" {
		if !gatewayOneOf(op, "create_record", "find_record", "update_record", "add_note") {
			return gatewayBad(400, "Unsupported CRM operation.")
		}
		fields, filters = in.Fields, in.Match
		switch op {
		case "create_record":
			op = "create"
		case "find_record":
			op = "find"
		case "update_record":
			op = "update"
			filters = map[string]any{"id": in.RecordID}
		case "add_note":
			op = "note"
			filters = map[string]any{"id": in.RecordID}
		}
		if (op == "update" || op == "note") && in.RecordID == "" {
			return gatewayBad(400, "record_id is required.")
		}
		if op == "find" && len(filters) == 0 {
			return gatewayBad(400, "Matching fields are required.")
		}
	}
	if kind == "database" && !gatewayOneOf(op, "create", "read", "update", "delete") {
		return gatewayBad(400, "Unsupported database operation.")
	}
	if op == "create" {
		if len(fields) == 0 {
			return gatewayBad(400, "Record fields or values are required.")
		}
		delete(fields, "id")
		if kind == "crm" {
			fields["record_type"] = resource
		}
		return 201, fields, gatewaySave(tx, kind, resource, fields)
	}
	if (op == "update" || op == "delete") && len(filters) == 0 {
		return gatewayBad(400, "Filters are required for updates and deletes.")
	}
	if op == "update" && len(fields) == 0 {
		return gatewayBad(400, "Changed fields or values are required.")
	}
	if op == "note" && in.Note == "" {
		return gatewayBad(400, "Note text is required.")
	}
	if op != "read" && op != "find" && op != "update" && op != "delete" && op != "note" {
		return gatewayBad(400, "Unsupported operation.")
	}
	limit := in.Limit
	if limit == 0 {
		limit = 10
	}
	if limit < 1 || limit > 100 {
		return gatewayBad(400, "Limit must be from 1 to 100.")
	}
	matched := []map[string]any{}
	for _, record := range records {
		if !gatewayMatch(record, filters) {
			continue
		}
		if op == "find" {
			record["found"] = true
			return 200, record, nil
		}
		if op == "update" {
			for key, value := range fields {
				if key != "id" && key != "record_type" {
					record[key] = value
				}
			}
			if err = gatewaySave(tx, kind, resource, record); err != nil {
				return 0, nil, err
			}
		}
		if op == "note" {
			notes, _ := record["notes"].([]any)
			record["notes"] = append(notes, in.Note)
			return 200, record, gatewaySave(tx, kind, resource, record)
		}
		if op == "delete" {
			if _, err = tx.Exec("DELETE FROM gateway_records WHERE id=?", record["id"]); err != nil {
				return 0, nil, err
			}
		}
		matched = append(matched, record)
		if kind == "crm" {
			return 200, record, nil
		}
		if op == "read" && len(matched) >= limit {
			break
		}
	}
	if kind == "crm" {
		if op == "find" {
			return 200, map[string]any{"found": false, "record": nil}, nil
		}
		return gatewayBad(404, "CRM record not found.")
	}
	return 200, map[string]any{"operation": op, "resource": resource, "records": matched, "count": len(matched)}, nil
}

func gatewayCalendar(tx *sql.Tx, in gatewayInput) (int, map[string]any, error) {
	if in.CalendarID == "" || len(in.CalendarID) > 128 || in.Timezone == "" {
		return gatewayBad(400, "calendar_id and timezone are required.")
	}
	if _, err := time.LoadLocation(in.Timezone); err != nil {
		return gatewayBad(400, "Use a valid IANA timezone, such as UTC or Asia/Kolkata.")
	}
	if !gatewayOneOf(in.Operation, "check_availability", "create_booking", "reschedule_booking", "cancel_booking") {
		return gatewayBad(400, "Unsupported calendar operation.")
	}
	records, err := gatewayRecords(tx, "calendar", in.CalendarID)
	if err != nil {
		return 0, nil, err
	}
	var target map[string]any
	for _, record := range records {
		if record["id"] == in.EventID {
			target = record
		}
	}
	if in.Operation == "reschedule_booking" || in.Operation == "cancel_booking" {
		if target == nil {
			return gatewayBad(404, "Booking not found.")
		}
	}
	if in.Operation == "cancel_booking" {
		_, err = tx.Exec("DELETE FROM gateway_records WHERE id=?", in.EventID)
		return 200, map[string]any{"event_id": in.EventID, "status": "cancelled"}, err
	}
	start, err1 := time.Parse(time.RFC3339, in.StartAt)
	end, err2 := time.Parse(time.RFC3339, in.EndAt)
	if err1 != nil || err2 != nil || !end.After(start) {
		return gatewayBad(400, "Use RFC3339 start_at/end_at with end after start.")
	}
	available := true
	for _, record := range records {
		if in.Operation == "reschedule_booking" && record["id"] == in.EventID {
			continue
		}
		a, _ := time.Parse(time.RFC3339, fmt.Sprint(record["start_at"]))
		b, _ := time.Parse(time.RFC3339, fmt.Sprint(record["end_at"]))
		if start.Before(b) && end.After(a) {
			available = false
		}
	}
	if in.Operation == "check_availability" {
		return 200, map[string]any{"available": available, "calendar_id": in.CalendarID, "start_at": in.StartAt, "end_at": in.EndAt}, nil
	}
	if !available {
		return gatewayBad(409, "The requested time overlaps an existing test booking.")
	}
	status := 200
	if in.Operation == "create_booking" {
		if in.Title == "" {
			return gatewayBad(400, "Title is required.")
		}
		if _, err = mail.ParseAddress(in.AttendeeEmail); err != nil {
			return gatewayBad(400, "A valid attendee_email is required.")
		}
		target = map[string]any{"title": in.Title, "attendee_email": in.AttendeeEmail, "attendee_name": in.AttendeeName}
		status = 201
	}
	target["calendar_id"] = in.CalendarID
	target["timezone"] = in.Timezone
	target["start_at"] = in.StartAt
	target["end_at"] = in.EndAt
	target["notes"] = in.Notes
	if err = gatewaySave(tx, "calendar", in.CalendarID, target); err != nil {
		return 0, nil, err
	}
	target["event_id"] = target["id"]
	target["status"] = "confirmed"
	return status, target, nil
}
