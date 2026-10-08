package ticketfairy

import (
	"context"
	"encoding/json"
	"iter"
	"net/url"
	"strconv"
	"strings"
)

// EventsService reads public event listings and works with your own events.
type EventsService struct {
	client *Client
}

// ListParams filters the public event listing. Every field is optional.
type ListParams struct {
	// Search is words to match against event names and descriptions.
	Search string
	// Country is an ISO 3166-1 alpha-2 country code.
	Country string
	// State is a region, state or province.
	State string
	// From and To are dates as YYYY-MM-DD.
	From string
	To   string
	// SectionType is the listing section to read: upcoming, current, slider,
	// past or none.
	SectionType string
	// Timezone is an IANA timezone for the date window.
	Timezone string
	// Sort is created_at, updated_at or start_date.
	Sort string
	// Order is asc or desc.
	Order string
	// BrandID returns only events from this brand.
	BrandID int64
	// Include lists the kinds of event to include, such as paid_events,
	// free_events, activities and external_events.
	Include []string
	// Size is the number of events per page, up to 200.
	Size int
	// Page is the page number, starting at 1. To read page after page, use
	// Cursor or Events.All instead.
	Page int
	// Cursor is the previous page's Pagination.NextCursor.
	Cursor string
}

func (p ListParams) values() url.Values {
	values := url.Values{}
	set := func(name, value string) {
		if value != "" {
			values.Set(name, value)
		}
	}
	set("search", p.Search)
	set("country", p.Country)
	set("state", p.State)
	set("from", p.From)
	set("to", p.To)
	set("section_type", p.SectionType)
	set("timezone", p.Timezone)
	set("sort", p.Sort)
	set("order", p.Order)
	if p.BrandID != 0 {
		values.Set("brandId", strconv.FormatInt(p.BrandID, 10))
	}
	set("include", strings.Join(p.Include, ","))
	if p.Size != 0 {
		values.Set("size", strconv.Itoa(p.Size))
	}
	if p.Page != 0 {
		values.Set("page", strconv.Itoa(p.Page))
	}
	set("cursor", p.Cursor)
	return values
}

// EventPage is one page of the public event listing.
type EventPage struct {
	Events     []Event    `json:"events"`
	Pagination Pagination `json:"pagination"`
}

// Pagination says where a page sits in the result.
type Pagination struct {
	Page       int `json:"page"`
	Size       int `json:"size"`
	TotalCount int `json:"totalCount"`
	TotalPages int `json:"totalPages"`
	// NextCursor reads the next page when sent as ListParams.Cursor with the
	// same filters. It is empty on the last page.
	NextCursor string `json:"nextCursor"`
}

// Event is one live event, as the public listing sends it. Start and end
// dates are ISO 8601 in the event's own timezone, which is given alongside
// them. Raw holds the event exactly as it arrived, including any field this
// type does not name.
type Event struct {
	URL              string         `json:"url"`
	DisplayName      string         `json:"displayName"`
	Subtitle         string         `json:"subtitle"`
	Description      string         `json:"description"`
	ShortDescription string         `json:"shortDescription"`
	EventTypes       []string       `json:"eventTypes"`
	Tags             []string       `json:"tags"`
	MinimumAge       string         `json:"minimumAge"`
	Currency         *Currency      `json:"currency"`
	StartingPrice    *StartingPrice `json:"startingPrice"`
	StartDate        string         `json:"startDate"`
	EndDate          string         `json:"endDate"`
	Timezone         string         `json:"timezone"`
	CreatedAt        string         `json:"createdAt"`
	UpdatedAt        string         `json:"updatedAt"`
	ImageURL         string         `json:"imageURL"`
	ThumbnailURL     string         `json:"thumbnailURL"`
	BackgroundURL    string         `json:"backgroundURL"`
	Brand            *Brand         `json:"brand"`
	Venue            *Venue         `json:"venue"`

	Raw json.RawMessage `json:"-"`
}

// UnmarshalJSON keeps the event's raw JSON alongside the named fields.
func (e *Event) UnmarshalJSON(data []byte) error {
	type plain Event
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*e = Event(decoded)
	e.Raw = append(json.RawMessage(nil), data...)
	return nil
}

// Currency is the currency tickets are sold in.
type Currency struct {
	Currency string `json:"currency"`
	Symbol   string `json:"symbol"`
	// DecimalPlaces is the number of minor-unit digits. It is normally a
	// number; an event saved with a numeric string keeps the string.
	DecimalPlaces json.RawMessage `json:"decimal_places"`
}

// StartingPrice is the lowest price a buyer can pay now for a ticket sold
// online to anyone. It is a guide, not a quote: checkout confirms the price.
type StartingPrice struct {
	// Amount is in major units with the currency's decimal places, for
	// example "25.00".
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

// Brand is the event's organiser.
type Brand struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// Venue is where the event takes place. When Hidden is true, Name is empty
// and the address fields are absent; the city, state and country stay.
type Venue struct {
	Name          string `json:"name"`
	Country       string `json:"country"`
	State         string `json:"state"`
	City          string `json:"city"`
	Hidden        bool   `json:"hidden"`
	GooglePlaceID string `json:"googlePlaceId"`
	Latitude      string `json:"latitude"`
	Longitude     string `json:"longitude"`
	PostalCode    string `json:"postalCode"`
	Street        string `json:"street"`
	StreetNumber  string `json:"streetNumber"`
}

// List returns one page of public events that are on sale or upcoming. It
// needs no token.
func (s *EventsService) List(ctx context.Context, params ListParams) (*EventPage, error) {
	res, err := s.client.public(ctx, request{method: "GET", path: "/api/v1/events/listing", query: params.values()})
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Data *EventPage `json:"data"`
	}
	if err := json.Unmarshal(res.raw, &envelope); err != nil || envelope.Data == nil {
		return nil, unexpected("the event listing", res.status, res.body)
	}
	return envelope.Data, nil
}

// All returns every public event that matches params, page after page,
// following Pagination.NextCursor. It stops after limit events when limit is
// more than 0. params.Page and params.Cursor are ignored. Stop ranging at any
// time to stop fetching.
//
//	for event, err := range client.Events.All(ctx, ticketfairy.ListParams{Search: "jazz"}, 200) {
//		if err != nil {
//			return err
//		}
//		fmt.Println(event.DisplayName)
//	}
func (s *EventsService) All(ctx context.Context, params ListParams, limit int) iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		params.Page = 0
		params.Cursor = ""
		yielded := 0
		for {
			page, err := s.List(ctx, params)
			if err != nil {
				yield(Event{}, err)
				return
			}
			for _, event := range page.Events {
				if limit > 0 && yielded >= limit {
					return
				}
				if !yield(event, nil) {
					return
				}
				yielded++
			}
			if page.Pagination.NextCursor == "" {
				return
			}
			params.Cursor = page.Pagination.NextCursor
		}
	}
}

// CreateParams describes a new event.
type CreateParams struct {
	// BrandID is the brand the event belongs to. You need the admin or owner
	// role on it.
	BrandID int64
	// Attributes are the event's fields. A draft needs displayName, slug
	// (letters, digits, hyphens and underscores) and "flagDraft": true. A
	// complete event also needs startDate, endDate, salesEndDate, currency
	// and a venue.
	Attributes map[string]any
	// IdempotencyKey makes the create safe to retry. A new random key is used
	// when it is empty, so a retry after a network error never creates a
	// second event.
	IdempotencyKey string
}

// Create creates an event for a brand you manage, and returns it as one map:
// "id", "type" and the event's attributes.
func (s *EventsService) Create(ctx context.Context, params CreateParams) (map[string]any, error) {
	key := params.IdempotencyKey
	if key == "" {
		key = newUUID()
	}
	attributes := params.Attributes
	if attributes == nil {
		attributes = map[string]any{}
	}
	body := map[string]any{
		"data": map[string]any{
			"type":       "event",
			"attributes": attributes,
			"relationships": map[string]any{
				"owner": map[string]any{"data": map[string]any{"type": "brand", "id": strconv.FormatInt(params.BrandID, 10)}},
			},
		},
	}
	res, err := s.client.organiser(ctx, request{
		method:  "POST",
		path:    "/api/events",
		body:    body,
		headers: map[string]string{"Content-Type": jsonAPI, "Idempotency-Key": key},
	})
	if err != nil {
		return nil, err
	}
	fields, ok := res.body.(map[string]any)
	if !ok {
		return nil, unexpected("the new event", res.status, res.body)
	}
	return flatten(fields["data"], res.status)
}

// SalesSections are the report sections Sales returns by default.
var SalesSections = []string{"sales", "release_breakdown"}

// Sales returns tickets sold and revenue for an event, by day and by ticket
// type and release. A promoter sees only the sales they made. Leave out
// sections to get SalesSections.
func (s *EventsService) Sales(ctx context.Context, eventID int64, sections ...string) (map[string]any, error) {
	if len(sections) == 0 {
		sections = SalesSections
	}
	res, err := s.client.organiser(ctx, request{
		method: "GET",
		path:   "/api/events/" + strconv.FormatInt(eventID, 10) + "/relationships/performance",
		query:  url.Values{"section": {strings.Join(sections, "|")}},
	})
	if err != nil {
		return nil, err
	}
	fields, ok := res.body.(map[string]any)
	if !ok {
		return nil, unexpected("the event's sales", res.status, res.body)
	}
	sales, ok := fields["data"].(map[string]any)
	if !ok {
		return nil, unexpected("the event's sales", res.status, res.body)
	}
	return sales, nil
}

// flatten turns a JSON:API resource into one map: "id", "type" and its
// attributes.
func flatten(resource any, status int) (map[string]any, error) {
	fields, ok := resource.(map[string]any)
	if !ok {
		return nil, unexpected("the resource", status, resource)
	}
	flat := map[string]any{"id": fields["id"], "type": fields["type"]}
	if attributes, ok := fields["attributes"].(map[string]any); ok {
		for name, value := range attributes {
			flat[name] = value
		}
	}
	return flat, nil
}
