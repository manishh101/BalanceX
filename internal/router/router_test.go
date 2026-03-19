package router

import (
	"net/http"
	"testing"
)

func TestRouterManager(t *testing.T) {
	m := NewManager()

	err := m.AddRoute("default", `PathPrefix("/")`, 1, nil, "all-backends", nil)
	if err != nil {
		t.Fatal(err)
	}

	err = m.AddRoute("api", `PathPrefix("/api")`, 10, nil, "api-backends", nil)
	if err != nil {
		t.Fatal(err)
	}

	err = m.AddRoute("api-payment", `PathPrefix("/api/payment")`, 10, nil, "payment-backends", nil)
	if err != nil {
		t.Fatal(err)
	}

	// api and api-payment both have priority 10.
	// Because api-payment has a longer rule string, it should be evaluated before api.

	reqPayment, _ := http.NewRequest("GET", "/api/payment/process", nil)
	r1 := m.Route(reqPayment)
	if r1 == nil {
		t.Fatalf("Expected api-payment, got nil")
	}
	if r1.Name != "api-payment" {
		t.Errorf("Expected api-payment, got %s", r1.Name)
	}

	reqApi, _ := http.NewRequest("GET", "/api/users", nil)
	r2 := m.Route(reqApi)
	if r2 == nil {
		t.Fatalf("Expected api, got nil")
	}
	if r2.Name != "api" {
		t.Errorf("Expected api, got %s", r2.Name)
	}

	reqOther, _ := http.NewRequest("GET", "/home", nil)
	r3 := m.Route(reqOther)
	if r3 == nil {
		t.Fatalf("Expected default, got nil")
	}
	if r3.Name != "default" {
		t.Errorf("Expected default, got %s", r3.Name)
	}
}

func TestRouterManager_NoMatch(t *testing.T) {
	m := NewManager()
	m.AddRoute("only-api", `PathPrefix("/api")`, 1, nil, "svc", nil)

	req, _ := http.NewRequest("GET", "/static/css", nil)
	r := m.Route(req)
	if r != nil {
		t.Errorf("Expected no match, got %s", r.Name)
	}
}
