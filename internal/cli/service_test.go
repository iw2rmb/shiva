package cli

import (
	"context"
	"reflect"
	"testing"

	"github.com/iw2rmb/shiva/internal/cli/request"
)

func TestDraftServiceGetSpecNormalizesSelector(t *testing.T) {
	t.Parallel()

	client := &recordingQueryClient{
		specBody: []byte("openapi: 3.1.0\npaths: {}\n"),
	}

	service := &DraftService{client: client}
	body, err := service.GetSpec(context.Background(), request.Envelope{
		Repo:       " allure/allure-deployment ",
		API:        "service-catalog/allure-api.yaml",
		RevisionID: 146,
	}, SpecFormatYAML)
	if err != nil {
		t.Fatalf("get spec failed: %v", err)
	}
	if string(body) != "openapi: 3.1.0\npaths: {}\n" {
		t.Fatalf("unexpected spec body %q", string(body))
	}
	expected := request.Envelope{
		Kind:       request.KindSpec,
		Repo:       "allure/allure-deployment",
		API:        "service-catalog/allure-api.yaml",
		RevisionID: 146,
	}
	if !reflect.DeepEqual(client.lastSpecSelector, expected) {
		t.Fatalf("expected spec selector %+v, got %+v", expected, client.lastSpecSelector)
	}
	if client.lastSpecFormat != SpecFormatYAML {
		t.Fatalf("expected spec format %q, got %q", SpecFormatYAML, client.lastSpecFormat)
	}
}

func TestDraftServiceGetOperationNormalizesMethodPath(t *testing.T) {
	t.Parallel()

	client := &recordingQueryClient{
		operationBody: []byte(`{"operationId":"patchPet"}`),
	}

	service := &DraftService{client: client}
	_, err := service.GetOperation(context.Background(), request.Envelope{
		Repo:   "allure/allure-deployment",
		Method: "PATCH",
		Path:   "pets/:id",
	})
	if err != nil {
		t.Fatalf("get operation failed: %v", err)
	}

	expected := request.Envelope{
		Kind:   request.KindOperation,
		Repo:   "allure/allure-deployment",
		Method: "patch",
		Path:   "/pets/{id}",
	}
	if !reflect.DeepEqual(client.lastOperationSelector, expected) {
		t.Fatalf("expected operation selector %+v, got %+v", expected, client.lastOperationSelector)
	}
}

func TestDraftServicePlanCallNormalizesTarget(t *testing.T) {
	t.Parallel()

	client := &recordingQueryClient{
		callBody: []byte(`{"kind":"call"}`),
	}

	service := &DraftService{client: client}
	_, err := service.PlanCall(context.Background(), request.Envelope{
		Repo:        "allure/allure-deployment",
		OperationID: "getUsers",
		Target:      "shiva",
		DryRun:      true,
	})
	if err != nil {
		t.Fatalf("plan call failed: %v", err)
	}

	expected := request.Envelope{
		Kind:        request.KindCall,
		Repo:        "allure/allure-deployment",
		Target:      "shiva",
		OperationID: "getUsers",
		DryRun:      true,
	}
	if !reflect.DeepEqual(client.lastCallSelector, expected) {
		t.Fatalf("expected call selector %+v, got %+v", expected, client.lastCallSelector)
	}
}

func TestConvertJSONToYAML(t *testing.T) {
	t.Parallel()

	body, err := ConvertJSONToYAML([]byte(`{"operationId":"patchPet"}`))
	if err != nil {
		t.Fatalf("convert json to yaml failed: %v", err)
	}
	if string(body) != "operationId: patchPet\n" {
		t.Fatalf("unexpected yaml body %q", string(body))
	}
}

type recordingQueryClient struct {
	specBody              []byte
	operationBody         []byte
	callBody              []byte
	healthBody            []byte
	lastSpecSelector      request.Envelope
	lastOperationSelector request.Envelope
	lastCallSelector      request.Envelope
	lastSpecFormat        SpecFormat
}

func (c *recordingQueryClient) GetSpec(ctx context.Context, selector request.Envelope, format SpecFormat) ([]byte, error) {
	c.lastSpecSelector = selector
	c.lastSpecFormat = format
	return c.specBody, nil
}

func (c *recordingQueryClient) GetOperation(ctx context.Context, selector request.Envelope) ([]byte, error) {
	c.lastOperationSelector = selector
	return c.operationBody, nil
}

func (c *recordingQueryClient) PlanCall(ctx context.Context, selector request.Envelope) ([]byte, error) {
	c.lastCallSelector = selector
	return c.callBody, nil
}

func (c *recordingQueryClient) Health(ctx context.Context) ([]byte, error) {
	return c.healthBody, nil
}
