package ticketfairy

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"net/url"
	"strings"
)

var venueLocationFields = []string{"displayName", "googlePlaceId", "streetNumber", "street", "city", "state", "country", "postalCode", "latitude", "longitude"}

func (s *EventsService) resolveVenue(ctx context.Context, attributes map[string]any) (map[string]any, error) {
	result := make(map[string]any, len(attributes))
	for key, value := range attributes {
		result[key] = value
	}
	if online, _ := attributes["isOnlineEvent"].(bool); online || attributes["venue"] == nil {
		return result, nil
	}
	raw, err := json.Marshal(attributes["venue"])
	if err != nil {
		return nil, unresolvedVenue()
	}
	if string(raw) == "null" {
		return result, nil
	}
	var venue map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&venue) != nil || venue == nil {
		return nil, unresolvedVenue()
	}
	location := false
	for _, field := range venueLocationFields {
		if _, exists := venue[field]; exists {
			location = true
		}
	}
	if !location {
		return result, nil
	}
	placeID, ok := venue["googlePlaceId"].(string)
	if venue["googlePlaceId"] != nil && !ok {
		return nil, unresolvedVenue()
	}
	placeID = strings.TrimSpace(placeID)
	parts := []string{}
	for _, field := range venueLocationFields {
		if field == "googlePlaceId" || field == "latitude" || field == "longitude" {
			continue
		}
		if value, ok := venue[field].(string); ok && strings.TrimSpace(value) != "" {
			parts = append(parts, strings.TrimSpace(value))
		}
	}
	query := strings.Join(parts, " ")
	if placeID == "" && query == "" {
		return nil, unresolvedVenue()
	}
	r := request{method: "GET", path: "/api/places/" + url.PathEscape(placeID)}
	if placeID == "" {
		r.path = "/api/places/search"
		r.query = url.Values{"search": {query}}
	}
	res, err := s.client.organiser(ctx, r)
	if err != nil {
		return nil, err
	}
	envelope, ok := res.body.(map[string]any)
	if !ok {
		return nil, unresolvedVenue()
	}
	data := envelope["data"]
	if placeID == "" {
		rows, ok := data.([]any)
		if !ok || len(rows) == 0 {
			return nil, unresolvedVenue()
		}
		if len(rows) > 1 {
			candidates := []map[string]any{}
			for i, row := range rows {
				if i >= 10 {
					break
				}
				resource, ok := row.(map[string]any)
				if !ok {
					continue
				}
				attrs, _ := resource["attributes"].(map[string]any)
				id := attrs["place_id"]
				if id == nil {
					id = resource["id"]
				}
				candidates = append(candidates, map[string]any{"place_id": id, "name": attrs["name"], "formatted_address": attrs["formatted_address"]})
			}
			return nil, &Error{Kind: ErrValidation, Code: "VENUE_AMBIGUOUS", Message: "Several venues matched. Set venue.googlePlaceId to the chosen Place ID and retry.", Body: map[string]any{"query": query, "candidates": candidates}}
		}
		data = rows[0]
	}
	resource, _ := data.(map[string]any)
	fields, _ := resource["attributes"].(map[string]any)
	resolved, _ := fields["venue"].(map[string]any)
	for _, field := range []string{"googlePlaceId", "displayName", "country"} {
		value, ok := resolved[field].(string)
		if !ok || strings.TrimSpace(value) == "" {
			return nil, unresolvedVenue()
		}
	}
	latNumber, latOK := resolved["latitude"].(json.Number)
	lngNumber, lngOK := resolved["longitude"].(json.Number)
	lat, latErr := latNumber.Float64()
	lng, lngErr := lngNumber.Float64()
	if !latOK || !lngOK || latErr != nil || lngErr != nil || math.IsNaN(lat) || math.IsNaN(lng) || math.IsInf(lat, 0) || math.IsInf(lng, 0) || math.Abs(lat) > 90 || math.Abs(lng) > 180 || (lat == 0 && lng == 0) || (placeID != "" && resolved["googlePlaceId"] != placeID) {
		return nil, unresolvedVenue()
	}
	for _, field := range venueLocationFields {
		venue[field] = resolved[field]
	}
	result["venue"] = venue
	return result, nil
}

func unresolvedVenue() *Error {
	return &Error{Kind: ErrValidation, Code: "VENUE_UNRESOLVED", Message: "The venue could not be resolved with valid map coordinates. Refine the name and address or supply another Google Place ID. No event was saved."}
}
