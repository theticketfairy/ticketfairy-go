package ticketfairy

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func placeResponse(venue map[string]any, search bool, count int) string {
	row := map[string]any{"id": "place-95", "attributes": map[string]any{"venue": venue, "place_id": "place-95", "name": "Te Whaea", "formatted_address": "Wellington NZ"}}
	var data any = row
	if search {
		rows := []any{}
		for i := 0; i < count; i++ {
			rows = append(rows, row)
		}
		data = rows
	}
	raw, _ := json.Marshal(map[string]any{"data": data})
	return string(raw)
}
func resolvedTestVenue() map[string]any {
	return map[string]any{"googlePlaceId": "place-95", "displayName": "Te Whaea", "country": "NZ", "latitude": -41.3, "longitude": 174.7}
}

const createdTestEvent = `{"data":{"id":"54815","type":"event","attributes":{}}}`

func TestVenueSearchResolvesBeforeCreateWithoutMutatingInput(t *testing.T) {
	r := newRecorder(t, jsonReply(200, placeResponse(resolvedTestVenue(), true, 1)), jsonReply(200, createdTestEvent))
	input := map[string]any{"displayName": "Te Whaea", "city": "Wellington", "latitude": 0, "longitude": 0, "flagDisabled": true, "custom": "keep", "largeInteger": int64(9007199254740993)}
	_, err := testClient(r, &noSleep{}, WithToken("token")).Events.Create(context.Background(), CreateParams{BrandID: 95, Attributes: map[string]any{"flagDraft": true, "venue": input}})
	if err != nil {
		t.Fatal(err)
	}
	if r.requests[0].URL.Path != "/api/places/search" || r.requests[0].URL.Query().Get("search") != "Te Whaea Wellington" || r.requests[0].URL.Query().Has("q") {
		t.Fatal(r.requests[0].URL)
	}
	var body map[string]any
	_ = json.Unmarshal([]byte(r.bodies[1]), &body)
	saved := body["data"].(map[string]any)["attributes"].(map[string]any)["venue"].(map[string]any)
	if saved["latitude"] != -41.3 || saved["googlePlaceId"] != "place-95" || saved["flagDisabled"] != true || saved["custom"] != "keep" {
		t.Fatal(saved)
	}
	if !strings.Contains(r.bodies[1], `"largeInteger":9007199254740993`) {
		t.Fatal("venue integer precision lost", r.bodies[1])
	}
	if input["latitude"] != 0 || input["googlePlaceId"] != nil {
		t.Fatal("input mutated", input)
	}
}

func TestVenueChosenIDAcceptsEquator(t *testing.T) {
	venue := resolvedTestVenue()
	venue["latitude"] = 0
	venue["longitude"] = 30
	r := newRecorder(t, jsonReply(200, placeResponse(venue, false, 1)), jsonReply(200, createdTestEvent))
	_, err := testClient(r, &noSleep{}, WithToken("token")).Events.Create(context.Background(), CreateParams{BrandID: 95, Attributes: map[string]any{"venue": map[string]any{"googlePlaceId": "place-95"}}, IdempotencyKey: "same-key"})
	if err != nil {
		t.Fatal(err)
	}
	if r.requests[0].URL.Path != "/api/places/place-95" || r.requests[1].Header.Get("Idempotency-Key") != "same-key" {
		t.Fatal("lookup or key changed")
	}
}

func TestVenueAmbiguousSearchReturnsChoicesWithoutSaving(t *testing.T) {
	r := newRecorder(t, jsonReply(200, placeResponse(resolvedTestVenue(), true, 2)))
	_, err := testClient(r, &noSleep{}, WithToken("token")).Events.Create(context.Background(), CreateParams{Attributes: map[string]any{"venue": map[string]any{"displayName": "Te Whaea"}}})
	var detail *Error
	if !errors.Is(err, ErrValidation) || !errors.As(err, &detail) || detail.Code != "VENUE_AMBIGUOUS" || r.count() != 1 {
		t.Fatal(err, r.count())
	}
	if len(detail.Body.(map[string]any)["candidates"].([]map[string]any)) != 2 {
		t.Fatal(detail.Body)
	}
}

func TestInvalidVenueResponsesNeverSave(t *testing.T) {
	for _, changes := range []map[string]any{{"latitude": 0, "longitude": 0}, {"latitude": nil}, {"latitude": true}, {"latitude": 91}, {"country": ""}, {"googlePlaceId": "different"}} {
		venue := resolvedTestVenue()
		for k, v := range changes {
			venue[k] = v
		}
		r := newRecorder(t, jsonReply(200, placeResponse(venue, false, 1)))
		_, err := testClient(r, &noSleep{}, WithToken("token")).Events.Create(context.Background(), CreateParams{Attributes: map[string]any{"flagDraft": true, "venue": map[string]any{"googlePlaceId": "place-95"}}})
		if !errors.Is(err, ErrValidation) || r.count() != 1 {
			t.Fatal(changes, err, r.count())
		}
	}
}

func TestVenueFailureHasNoManualFallback(t *testing.T) {
	r := newRecorder(t, jsonReply(http.StatusForbidden, `{"message":"Places access denied"}`))
	_, err := testClient(r, &noSleep{}, WithToken("token")).Events.Create(context.Background(), CreateParams{Attributes: map[string]any{"venue": map[string]any{"displayName": "Te Whaea", "latitude": -41.3, "longitude": 174.7}}})
	if !errors.Is(err, ErrPermissionDenied) || r.count() != 1 {
		t.Fatal(err, r.count())
	}
}

func TestDraftsOnlineAndVenueSettingsNeedNoLookup(t *testing.T) {
	for _, attrs := range []map[string]any{{"flagDraft": true}, {"flagDraft": true, "venue": map[string]any(nil)}, {"isOnlineEvent": true, "venue": map[string]any{"displayName": "Zoom"}}, {"venue": map[string]any{"flagDisabled": true}}} {
		r := newRecorder(t, jsonReply(200, createdTestEvent))
		_, err := testClient(r, &noSleep{}, WithToken("token")).Events.Create(context.Background(), CreateParams{Attributes: attrs})
		if err != nil || r.count() != 1 || r.requests[0].Method != "POST" {
			t.Fatal(err, r.count())
		}
	}
}

func TestInvalidVenueInputSendsNoRequests(t *testing.T) {
	for _, venue := range []any{"Te Whaea", map[string]any{"googlePlaceId": 123}, map[string]any{"latitude": 0}, map[string]any{"googlePlaceId": ""}} {
		r := newRecorder(t)
		_, err := testClient(r, &noSleep{}, WithToken("token")).Events.Create(context.Background(), CreateParams{Attributes: map[string]any{"venue": venue}})
		if !errors.Is(err, ErrValidation) || r.count() != 0 {
			t.Fatal(venue, err, r.count())
		}
	}
}
