package azure

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/stretchr/testify/assert"
)

type fakeTokenCredential struct{}

func (f *fakeTokenCredential) GetToken(_ context.Context, _ policy.TokenRequestOptions) (azcore.AccessToken, error) {
	return azcore.AccessToken{Token: "fake-token", ExpiresOn: time.Now().Add(time.Hour)}, nil
}

func newTestClient(t *testing.T, handler http.Handler) *MsGraphClient {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return NewMSGraphClient(srv.URL, &fakeTokenCredential{})
}

type testItem struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Maybe  bool     `json:"maybe"`
	Lots   bool     `json:"lots"`
	More   int32    `json:"more"`
	Fields []string `json:"fields"`
}

func TestListTheoreticalKindWithFilter(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/v1.0/things", r.URL.Path)
		assert.Equal(t, "Bearer fake-token", r.Header.Get("Authorization"))
		assert.Equal(t, "name eq 'foo'", r.URL.Query().Get("$filter"))

		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(w).Encode(odataCollection[testItem]{
			Value: []testItem{
				{ID: "1", Name: "foo"},
				{ID: "2", Name: "foo-2"},
			},
		}))
	})

	client := newTestClient(t, handler)
	items, err := ListTheoreticalKindWithFilter[testItem](context.Background(), client, "things", "name eq 'foo'")
	if assert.NoError(t, err) {
		assert.Len(t, items, 2)
		assert.Equal(t, "1", items[0].ID)
		assert.Equal(t, "foo-2", items[1].Name)
	}
}

func TestListTheoreticalKindWithFilterEmpty(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(w).Encode(odataCollection[testItem]{Value: []testItem{}}))
	})

	client := newTestClient(t, handler)
	items, err := ListTheoreticalKindWithFilter[testItem](context.Background(), client, "things", "filter")
	if assert.NoError(t, err) {
		assert.Empty(t, items)
	}
}

func TestDeleteTheoreticalKind(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "/v1.0/things/obj-1", r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	})

	client := newTestClient(t, handler)
	err := DeleteTheoreticalKind(context.Background(), client, "things", "obj-1")
	assert.NoError(t, err)
}

func TestODataErrorParsing(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		assert.NoError(t, json.NewEncoder(w).Encode(odataErrorResponse{
			Error: odataErrorDetail{
				Code:    "Authorization_RequestDenied",
				Message: "Insufficient privileges to complete the operation.",
			},
		}))
	})

	client := newTestClient(t, handler)
	_, err := ListTheoreticalKindWithFilter[testItem](context.Background(), client, "things", "filter")
	if assert.Error(t, err) {
		assert.Contains(t, err.Error(), "Authorization_RequestDenied")
		assert.Contains(t, err.Error(), "Insufficient privileges")
	}
}

func TestNonODataErrorResponse(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, err := w.Write([]byte("internal server error"))
		assert.NoError(t, err)
	})

	client := newTestClient(t, handler)
	_, err := ListTheoreticalKindWithFilter[testItem](context.Background(), client, "things", "filter")
	if assert.Error(t, err) {
		assert.Contains(t, err.Error(), "unexpected status code 500")
	}
}

func TestDeleteNotFound(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		assert.NoError(t, json.NewEncoder(w).Encode(odataErrorResponse{
			Error: odataErrorDetail{
				Code:    "Request_ResourceNotFound",
				Message: "Resource 'obj-gone' does not exist.",
			},
		}))
	})

	client := newTestClient(t, handler)
	err := DeleteTheoreticalKind(context.Background(), client, "things", "obj-gone")
	if assert.Error(t, err) {
		assert.Contains(t, err.Error(), "Request_ResourceNotFound")
	}
}

func TestTrailingSlashNormalized(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.False(t, strings.Contains(r.URL.Path, "//"), "path should not contain double slashes: %s", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(w).Encode(odataCollection[testItem]{}))
	})

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	// Simulate endpoint with trailing slash like Azure environments provide
	client := NewMSGraphClient(srv.URL+"/", &fakeTokenCredential{})
	_, err := ListTheoreticalKindWithFilter[testItem](context.Background(), client, "things", "filter")
	assert.NoError(t, err)
}

func TestAuthorizationHeaderSent(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer fake-token", r.Header.Get("Authorization"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(w).Encode(odataCollection[testItem]{}))
	})

	client := newTestClient(t, handler)
	_, err := ListTheoreticalKindWithFilter[testItem](context.Background(), client, "things", "filter")
	assert.NoError(t, err)
}

func TestNewMSGraphClientNilForUnavailable(t *testing.T) {
	assert.Nil(t, NewMSGraphClient("", &fakeTokenCredential{}))
	assert.Nil(t, NewMSGraphClient("N/A", &fakeTokenCredential{}))
}
