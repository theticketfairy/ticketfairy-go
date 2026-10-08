package ticketfairy_test

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/theticketfairy/ticketfairy-go"
)

func ExampleEventsService_List() {
	client := ticketfairy.New()

	page, err := client.Events.List(context.Background(), ticketfairy.ListParams{Country: "GB", From: "2026-11-01", Size: 50})
	if err != nil {
		log.Fatal(err)
	}
	for _, event := range page.Events {
		fmt.Println(event.DisplayName, event.StartDate, event.URL)
	}
}

func ExampleEventsService_All() {
	client := ticketfairy.New()

	for event, err := range client.Events.All(context.Background(), ticketfairy.ListParams{Search: "jazz"}, 200) {
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(event.DisplayName)
	}
}

func ExampleEventsService_Create() {
	client := ticketfairy.New(ticketfairy.WithToken("...")) // or set TICKETFAIRY_TOKEN

	event, err := client.Events.Create(context.Background(), ticketfairy.CreateParams{
		BrandID: 1234,
		Attributes: map[string]any{
			"displayName": "Summer Festival 2027",
			"slug":        "summer-festival-2027",
			"flagDraft":   true,
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(event["id"])
}

func ExampleSetupCopyService_Wait() {
	client := ticketfairy.New()
	ctx := context.Background()
	var newEventID, lastYearEventID int64 = 2002, 1001

	run, err := client.SetupCopy.Start(ctx, newEventID, ticketfairy.StartParams{SourceEventID: lastYearEventID})
	if err != nil {
		log.Fatal(err)
	}
	run, err = client.SetupCopy.Wait(ctx, newEventID, run, ticketfairy.WaitOptions{Timeout: 10 * time.Minute})
	if errors.Is(err, ticketfairy.ErrSetupCopyTimeout) {
		fmt.Println("Still copying; call Wait again to keep following it.")
		return
	}
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(run.Status) // done, partly_done or failed
	for _, part := range run.Parts {
		fmt.Println(part.Label, part.Copied)
	}
}

func ExampleError() {
	client := ticketfairy.New()

	_, err := client.Events.Sales(context.Background(), 1234)
	var apiErr *ticketfairy.Error
	switch {
	case errors.Is(err, ticketfairy.ErrAuthentication):
		fmt.Println("Create a personal access token in your account settings.")
	case errors.Is(err, ticketfairy.ErrRateLimited) && errors.As(err, &apiErr):
		fmt.Println("Wait", apiErr.RetryAfter)
	case errors.As(err, &apiErr):
		fmt.Println(apiErr.Status, apiErr.Message, apiErr.Hint())
	}
}
